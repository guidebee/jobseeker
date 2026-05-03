import "dotenv/config";
import http from "http";
import net from "net";
import puppeteer from "puppeteer-extra";
import StealthPlugin from "puppeteer-extra-plugin-stealth";
import type { Browser, Page } from "puppeteer";

puppeteer.use(StealthPlugin());

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const PORT = Number(process.env.PUPPETEER_SERVICE_PORT ?? 3001);
const POOL_SIZE = Number(process.env.PUPPETEER_POOL_SIZE ?? 2);
const CHROME_EXECUTABLE = process.env.PUPPETEER_EXECUTABLE_PATH;

const PROXY_HOST = process.env.SCAN_PROXY_HOST ?? "";
const PROXY_PORT_NUM = Number(process.env.SCAN_PROXY_PORT ?? 823);
const PROXY_USER = process.env.SCAN_PROXY_USER ?? "";
const PROXY_PASS = process.env.SCAN_PROXY_PASS ?? "";
const PROXY_ENABLED = PROXY_HOST !== "";

// When true, skip the Google-search step and navigate to LinkedIn directly.
// Useful when Google blocks the headless browser with a CAPTCHA.
// Set SKIP_GOOGLE_SEARCH=true in .env to enable.
const SKIP_GOOGLE_SEARCH = process.env.SKIP_GOOGLE_SEARCH === "true";

// ---------------------------------------------------------------------------
// Fingerprint entropy pools
// ---------------------------------------------------------------------------

const USER_AGENTS = [
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
  "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
];

const VIEWPORTS = [
  { width: 1920, height: 1080 },
  { width: 1440, height: 900 },
  { width: 1366, height: 768 },
  { width: 1280, height: 800 },
];

function pickRandom<T>(arr: T[]): T {
  return arr[Math.floor(Math.random() * arr.length)]!;
}

// ---------------------------------------------------------------------------
// Local proxy relay
//
// page.authenticate() is broken for HTTPS CONNECT tunnels in Puppeteer v22's
// new headless mode. The relay is a plain HTTP server on localhost that Chrome
// can use without auth; it injects Proxy-Authorization into the upstream
// CONNECT handshake transparently.
// ---------------------------------------------------------------------------

let relayPort: number | null = null;

function startLocalProxyRelay(
  upstreamHost: string,
  upstreamPort: number,
  user: string,
  pass: string,
): Promise<number> {
  const auth = Buffer.from(`${user}:${pass}`).toString("base64");
  console.log(`[relay] auth header will be: Basic ${auth.slice(0, 8)}… (user="${user.slice(0, 6)}…" len=${user.length}+${pass.length})`);

  const server = http.createServer((req, res) => {
    // Plain HTTP requests — forward with auth header.
    const targetUrl = new URL(req.url ?? "/", `http://${req.headers.host}`);
    const proxyReq = http.request(
      { host: upstreamHost, port: upstreamPort, path: req.url, method: req.method,
        headers: { ...req.headers, "Proxy-Authorization": `Basic ${auth}` } },
      (pr) => { res.writeHead(pr.statusCode!, pr.headers); pr.pipe(res); },
    );
    req.pipe(proxyReq);
    proxyReq.on("error", () => { res.writeHead(502); res.end(); });
    void targetUrl; // suppress unused warning
  });

  server.on("connect", (req, clientSocket) => {
    // HTTPS CONNECT — open a socket to the upstream proxy and authenticate there.
    const upstream = net.createConnection(upstreamPort, upstreamHost, () => {
      upstream.write(
        `CONNECT ${req.url} HTTP/1.1\r\n` +
        `Host: ${req.url}\r\n` +
        `Proxy-Authorization: Basic ${auth}\r\n` +
        `Proxy-Connection: Keep-Alive\r\n\r\n`,
      );
    });

    upstream.once("data", (chunk) => {
      const resp = chunk.toString("utf8", 0, 100);
      if (resp.includes("200")) {
        clientSocket.write("HTTP/1.1 200 Connection Established\r\n\r\n");
        upstream.pipe(clientSocket);
        clientSocket.pipe(upstream);
      } else {
        console.error(
          `[relay] upstream CONNECT failed: ${resp.slice(0, 80).replace(/\r\n/g, " | ")}\n` +
          `        verify with: curl -x http://${upstreamHost}:${upstreamPort} -U "${user}:***" https://api.ipify.org`
        );
        clientSocket.end("HTTP/1.1 502 Bad Gateway\r\n\r\n");
        upstream.destroy();
      }
    });

    upstream.on("error", (e) => {
      console.error(`[relay] upstream error: ${e.message}`);
      clientSocket.end("HTTP/1.1 502 Bad Gateway\r\n\r\n");
    });
    clientSocket.on("error", () => upstream.destroy());
  });

  return new Promise((resolve, reject) => {
    server.listen(0, "127.0.0.1", () => {
      const port = (server.address() as net.AddressInfo).port;
      console.log(`[relay] local proxy relay on 127.0.0.1:${port} → ${upstreamHost}:${upstreamPort}`);
      relayPort = port;
      resolve(port);
    });
    server.on("error", reject);
  });
}

// ---------------------------------------------------------------------------
// Browser pool
// ---------------------------------------------------------------------------

class BrowserPool {
  private available: Browser[] = [];
  private waitQueue: Array<(browser: Browser) => void> = [];

  constructor(private readonly size: number) {}

  async init(): Promise<void> {
    console.log(`[pool] initialising ${this.size} browser(s)  proxy=${PROXY_ENABLED}  skipGoogle=${SKIP_GOOGLE_SEARCH}`);
    const args = [
      "--no-sandbox", "--disable-setuid-sandbox", "--disable-dev-shm-usage",
      "--lang=en-US", "--accept-lang=en-US",
    ];
    if (PROXY_ENABLED) {
      if (PROXY_USER) {
        // Credentials present: start a local relay that injects Proxy-Authorization
        // into every CONNECT handshake (page.authenticate() is broken in Puppeteer v22).
        const port = await startLocalProxyRelay(PROXY_HOST, PROXY_PORT_NUM, PROXY_USER, PROXY_PASS);
        args.push(`--proxy-server=http://127.0.0.1:${port}`);
        console.log(`[pool] proxy via relay 127.0.0.1:${port} → ${PROXY_HOST}:${PROXY_PORT_NUM}  auth=yes`);
      } else {
        // No credentials: IP-whitelist mode — point Chrome directly at the proxy.
        args.push(`--proxy-server=http://${PROXY_HOST}:${PROXY_PORT_NUM}`);
        console.log(`[pool] proxy: ${PROXY_HOST}:${PROXY_PORT_NUM}  auth=no (IP whitelist)`);
      }
    }
    const launches = Array.from({ length: this.size }, () =>
      puppeteer.launch({
        headless: true,
        ...(CHROME_EXECUTABLE ? { executablePath: CHROME_EXECUTABLE } : {}),
        args,
      }) as Promise<Browser>
    );
    this.available.push(...(await Promise.all(launches)));
    console.log(`[pool] ${this.size} browser(s) ready`);
  }

  acquire(): Promise<Browser> {
    if (this.available.length > 0) {
      return Promise.resolve(this.available.pop()!);
    }
    return new Promise((resolve) => this.waitQueue.push(resolve));
  }

  async release(browser: Browser): Promise<void> {
    let live = browser;
    if (!browser.connected) {
      console.warn("[pool] browser crashed — replacing");
      try { await browser.close(); } catch { /* ignore */ }
      live = await puppeteer.launch({ headless: true }) as Browser;
    }
    if (this.waitQueue.length > 0) {
      this.waitQueue.shift()!(live);
    } else {
      this.available.push(live);
    }
  }

  async destroy(): Promise<void> {
    await Promise.all(this.available.map((b) => b.close().catch(() => {})));
    this.available = [];
  }
}

const pool = new BrowserPool(POOL_SIZE);

// ---------------------------------------------------------------------------
// Shared page setup
// ---------------------------------------------------------------------------

async function setupPage(browser: Browser): Promise<Page> {
  const page = await browser.newPage();
  await page.setViewport(pickRandom(VIEWPORTS));
  const ua = pickRandom(USER_AGENTS);
  await page.setUserAgent(ua);
  // Proxy auth is handled by the local relay — no page.authenticate() needed.
  // Do NOT set a global Referer here: setting Referer=google.com when navigating
  // TO Google is circular and a known bot signal. Each operation sets its own
  // Referer via natural link clicks or explicit header overrides.
  await page.setExtraHTTPHeaders({
    "Accept-Language": "en-US,en;q=0.9",
    // Chrome client-hint headers — headless Chrome omits these, making it detectable.
    "sec-ch-ua": '"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"',
    "sec-ch-ua-mobile": "?0",
    "sec-ch-ua-platform": '"Windows"',
  });
  return page;
}

// testProxyConnectivity navigates to a plain JSON endpoint to verify the proxy
// is working and logs the outbound IP being used.
async function testProxyConnectivity(browser: Browser): Promise<void> {
  const page = await browser.newPage();
  // Auth injected by local relay — no page.authenticate() needed.
  try {
    await page.goto("https://api.ipify.org?format=json", { waitUntil: "domcontentloaded", timeout: 15_000 });
    const body = await page.evaluate(() => document.body?.innerText ?? "");
    const url = page.url();
    if (url.includes("chrome-error:")) {
      console.error(`[proxy-test] FAILED — chrome-error page. Proxy tunnel broken. Check credentials.`);
    } else {
      console.log(`[proxy-test] OK — outbound IP: ${body}`);
    }
  } catch (e) {
    console.error(`[proxy-test] FAILED — ${e}`);
  } finally {
    await page.close().catch(() => {});
  }
}

// ---------------------------------------------------------------------------
// LinkedIn profile fetch  (Google search → click → return HTML)
//
// Replicates: open incognito Chrome → search "linkedin <id>" → click result.
// Using domcontentloaded (not networkidle0) because Google / LinkedIn have
// continuous background XHR that prevents networkidle0 from ever firing.
// ---------------------------------------------------------------------------

function extractProfileId(profileURL: string): string {
  const m = profileURL.match(/linkedin\.com\/in\/([^/?#]+)/);
  return m ? m[1]! : profileURL;
}

// setGoogleConsentCookie pre-sets Google's SOCS consent cookie so the consent
// page never appears. Must be called before any google.com navigation.
// CDP Network.setCookies works for any domain without prior navigation.
async function setGoogleConsentCookie(page: Page): Promise<void> {
  const cdp = await page.createCDPSession();
  try {
    await cdp.send("Network.setCookie", {
      name: "SOCS",
      value: "CAI",
      domain: ".google.com",
      path: "/",
      secure: false,
      httpOnly: false,
    });
    console.log("[google] consent cookie set (SOCS=CAI)");
  } finally {
    await cdp.detach();
  }
}

// acceptGoogleConsent clicks "Accept all" on Google's GDPR consent page if present.
// Returns true if a consent page was detected (regardless of whether the click worked).
// Does NOT handle /sorry/ CAPTCHA pages — those are handled separately.
async function acceptGoogleConsent(page: Page): Promise<boolean> {
  const currentURL = page.url();
  // /sorry/index is a bot-detection CAPTCHA page, not a consent page.
  // Bail out immediately so the caller can throw a meaningful CAPTCHA error.
  if (currentURL.includes("/sorry/")) {
    console.log(`[google] CAPTCHA page detected (${currentURL.slice(0, 80)}) — not a consent page`);
    return false;
  }

  const title = await page.title();
  // Consent page: title is "Before you continue to Google Search", a locale variant,
  // or the raw URL when document.title is unset (consent.google.com behaviour).
  // Exclude sorry-page URLs that also surface as raw-URL titles.
  const isConsentPage =
    title.includes("Before you continue") ||
    title.includes("Avant de continuer") ||
    ((title.startsWith("https://") || title === "") && !currentURL.includes("/sorry/"));

  if (!isConsentPage) return false;

  console.log(`[google] consent page detected (title: "${title}") — accepting`);

  // Try common "Accept all" button selectors across locales.
  for (const sel of [
    'button[aria-label*="Accept all"]',
    'button[aria-label*="Tout accepter"]',
    "button#L2AGLb",
    'form[action*="consent"] button',
    'div[role="none"] button',
  ]) {
    try {
      await page.waitForSelector(sel, { timeout: 3_000 });
      await Promise.all([
        page.waitForNavigation({ waitUntil: "domcontentloaded", timeout: 15_000 }),
        page.click(sel),
      ]);
      console.log(`[google] consent accepted via "${sel}"`);
      return true;
    } catch {
      // selector not present — try next
    }
  }
  console.log("[google] could not find consent accept button — continuing anyway");
  return true;
}

async function fetchLinkedInHTML(profileURL: string): Promise<string> {
  const maxAttempts = 3;
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      return SKIP_GOOGLE_SEARCH
        ? await fetchLinkedInDirect(profileURL)
        : await fetchLinkedInViaGoogle(profileURL);
    } catch (e: any) {
      // Non-English content means the proxy IP landed on a non-English region.
      // Retry so the pool picks a different residential IP.
      const nonEnglish = String(e).includes("non-English content");
      if (nonEnglish && attempt < maxAttempts) {
        console.log(`[linkedin] attempt ${attempt}/${maxAttempts}: non-English proxy IP — retrying`);
        continue;
      }
      throw e;
    }
  }
  throw new Error("unreachable");
}

// Direct navigation — skips Google SERP, relies on residential proxy + stealth
// to pass LinkedIn's bot check without a Google Referer header.
async function fetchLinkedInDirect(profileURL: string): Promise<string> {
  // Force English locale so the page is parseable regardless of the proxy's GeoIP.
  const targetURL = profileURL.includes("?") ? profileURL + "&locale=en_US" : profileURL + "?locale=en_US";
  console.log(`[linkedin] direct navigation to ${targetURL}`);
  const browser = await pool.acquire();
  let page: Page | undefined;
  try {
    page = await setupPage(browser);

    const cdp = await page.createCDPSession();
    await cdp.send("Network.clearBrowserCookies");
    await cdp.detach();

    try {
      await page.goto(targetURL, { waitUntil: "domcontentloaded", timeout: 30_000 });
    } catch (e: any) {
      if (!String(e).includes("timeout") && !String(e).includes("Timeout")) throw e;
    }

    const title = await page.title();
    const url = page.url();
    console.log(`[linkedin] title: "${title}"  url: ${url}`);

    // Non-ASCII title = Arabic, Chinese, etc. from a GeoIP-mismatched proxy IP.
    // Throw a retryable error so fetchLinkedInHTML can pick a different IP.
    if (/[^\x00-\x7F]/.test(title)) {
      throw new Error(`non-English content detected (title: "${title}") — proxy IP is from a non-English region`);
    }

    if (url.includes("chrome-error:")) {
      // Extract Chrome's error code from the page for diagnosis.
      const errorDetail = await page.evaluate(() => {
        const code = document.querySelector("#error-code")?.textContent ?? "";
        const msg  = document.querySelector("#main-message h1")?.textContent ?? "";
        return `${code} ${msg}`.trim() || "unknown Chrome error";
      }).catch(() => "unknown Chrome error");
      throw new Error(
        `Chrome proxy/network error navigating to ${profileURL}: ${errorDetail}\n` +
        `Run: curl -x http://${PROXY_HOST}:${PROXY_PORT_NUM} -U "${PROXY_USER}:***" https://api.ipify.org\n` +
        `to verify proxy credentials work independently of Puppeteer.`
      );
    }

    if (
      title.toLowerCase().includes("linkedin login") ||
      title.toLowerCase().includes("sign in") ||
      url.includes("/login") ||
      url.includes("/authwall")
    ) {
      throw new Error(`LinkedIn redirected to login wall (title: "${title}"). Residential proxy IP may be flagged.`);
    }

    if (
      title.toLowerCase().includes("page not found") ||
      title.toLowerCase().includes("profile not found") ||
      title.includes("404")
    ) {
      throw new Error(`LinkedIn profile not found (title: "${title}", url: ${url}). Check the profile ID.`);
    }

    await new Promise((r) => setTimeout(r, 3_000));
    const html = await page.content();
    console.log(`[linkedin] got ${html.length} bytes`);
    return html;
  } finally {
    if (page) await page.close().catch(() => {});
    await pool.release(browser);
  }
}

async function fetchLinkedInViaGoogle(profileURL: string): Promise<string> {
  const profileId = extractProfileId(profileURL);
  const searchURL = `https://www.google.com/search?q=${encodeURIComponent("linkedin " + profileId)}&hl=en`;

  const browser = await pool.acquire();
  let page: Page | undefined;
  try {
    page = await setupPage(browser);

    // Clear cookies so LinkedIn can't correlate with a prior flagged session.
    const cdp = await page.createCDPSession();
    await cdp.send("Network.clearBrowserCookies");
    await cdp.detach();

    // Pre-set Google consent cookie so the GDPR/AU consent page never appears.
    await setGoogleConsentCookie(page);

    // Step 1: navigate to Google search (domcontentloaded is enough to get links).
    console.log(`[linkedin] step 1: Google search — ${searchURL}`);
    try {
      await page.goto(searchURL, { waitUntil: "domcontentloaded", timeout: 30_000 });
    } catch (e: any) {
      if (!String(e).includes("timeout") && !String(e).includes("Timeout")) throw e;
      console.log("[linkedin] Google search navigation timed out (partial DOM is OK)");
    }
    console.log(`[linkedin] Google title: "${await page.title()}"  url: ${page.url()}`);

    // Handle Google cookie consent page (common on first visit in AU/EU).
    await acceptGoogleConsent(page);
    if (page.url().includes("/sorry/")) {
      throw new Error(
        `Google blocked the request with a CAPTCHA (bot detection). ` +
        `Try again later or configure a residential proxy.`
      );
    }
    console.log(`[linkedin] post-consent title: "${await page.title()}"`);

    // Log page body snippet to confirm we have search results.
    const bodySnippet = await page.evaluate(() =>
      document.body?.innerText?.slice(0, 200) ?? "(no body)"
    );
    console.log(`[linkedin] body snippet: ${bodySnippet.replace(/\n/g, " ")}`);

    // Step 2: find the LinkedIn result and click it so Chrome sends an authentic
    // Referer + sec-fetch-site: cross-site header to LinkedIn.
    console.log("[linkedin] step 2: looking for LinkedIn link to click");
    let clicked = false;
    for (const sel of [
      `a[href*="linkedin.com/in/${profileId}"]`,
      `a[href*="linkedin.com/in/"]`,
    ]) {
      const el = await page.$(sel);
      if (!el) {
        console.log(`[linkedin] selector not found: ${sel}`);
        continue;
      }
      const href = await el.evaluate((a) => a.getAttribute("href"));
      console.log(`[linkedin] clicking: ${href}`);
      try {
        await Promise.all([
          page.waitForNavigation({ waitUntil: "domcontentloaded", timeout: 30_000 }),
          el.click(),
        ]);
        clicked = true;
        break;
      } catch (e: any) {
        console.log(`[linkedin] click/nav error: ${e.message}`);
      }
    }

    if (!clicked) {
      console.log(`[linkedin] no SERP result found — navigating directly to ${profileURL}`);
      try {
        await page.goto(profileURL, { waitUntil: "domcontentloaded", timeout: 30_000 });
      } catch (e: any) {
        if (!String(e).includes("timeout") && !String(e).includes("Timeout")) throw e;
      }
    }

    const liTitle = await page.title();
    const liURL = page.url();
    console.log(`[linkedin] title: "${liTitle}"  url: ${liURL}`);

    // Detect LinkedIn login/auth wall — profile is not accessible without login.
    if (
      liTitle.toLowerCase().includes("linkedin login") ||
      liTitle.toLowerCase().includes("sign in") ||
      liURL.includes("/login") ||
      liURL.includes("/authwall")
    ) {
      throw new Error(
        `LinkedIn redirected to login wall (title: "${liTitle}", url: ${liURL}). ` +
        `Try again in a few minutes or configure a residential proxy.`
      );
    }

    // Give LinkedIn's JS a moment to render dynamic content.
    await new Promise((r) => setTimeout(r, 3_000));

    const html = await page.content();
    console.log(`[linkedin] got ${html.length} bytes`);
    return html;

  } finally {
    if (page) await page.close().catch(() => {});
    await pool.release(browser);
  }
}

// ---------------------------------------------------------------------------
// LinkedIn keyword search  (Google site:linkedin.com/in + keywords → HTML)
// ---------------------------------------------------------------------------

async function searchLinkedInProfile(keywords: string, engine: string = "bing"): Promise<{ profileURL: string; html: string }> {
  // Plain keyword search — no site: operator, which both Google and Bing treat
  // as a bot signal and CAPTCHA. Appending "linkedin" steers results toward
  // LinkedIn profile pages without triggering operator-based bot detection.
  const query = `${keywords} linkedin`;
  const useGoogle = engine.toLowerCase() === "google";
  const searchURL = useGoogle
    ? `https://www.google.com/search?q=${encodeURIComponent(query)}&hl=en`
    : `https://www.bing.com/search?q=${encodeURIComponent(query)}&setlang=en`;

  const browser = await pool.acquire();
  let page: Page | undefined;
  try {
    page = await setupPage(browser);

    const cdp = await page.createCDPSession();
    await cdp.send("Network.clearBrowserCookies");
    await cdp.detach();

    if (useGoogle) {
      await setGoogleConsentCookie(page);

      // Warm up: visit google.com home before the search URL.
      // A real user session starts at the home page; jumping directly to a search
      // URL from a cold headless browser is a detectable pattern.
      console.log("[search] warm-up: visiting google.com");
      try {
        await page.goto("https://www.google.com/", { waitUntil: "domcontentloaded", timeout: 15_000 });
      } catch (e: any) {
        if (!String(e).includes("timeout") && !String(e).includes("Timeout")) throw e;
      }
      // Small random pause — mimics the time a human spends before typing a query.
      await new Promise((r) => setTimeout(r, 500 + Math.random() * 1000));
    }

    console.log(`[search] step 1: ${useGoogle ? "Google" : "Bing"} search — ${searchURL}`);
    try {
      await page.goto(searchURL, { waitUntil: "domcontentloaded", timeout: 30_000 });
    } catch (e: any) {
      if (!String(e).includes("timeout") && !String(e).includes("Timeout")) throw e;
    }

    if (useGoogle) {
      await acceptGoogleConsent(page);
      // Throw immediately if Google is showing a CAPTCHA — no point extracting URLs.
      if (page.url().includes("/sorry/")) {
        throw new Error(
          `Google blocked the search with a CAPTCHA (bot detection). ` +
          `Use --search-engine=bing instead, or configure a residential proxy.`
        );
      }
    }

    console.log(`[search] title: "${await page.title()}"  url: ${page.url()}`);

    const bodySnippet = await page.evaluate(() =>
      document.body?.innerText?.slice(0, 200) ?? "(no body)"
    );
    console.log(`[search] body snippet: ${bodySnippet.replace(/\n/g, " ")}`);

    // Extract the first LinkedIn profile URL from the SERP.
    // Bing result links use opaque redirect hrefs (bing.com/ck/a?...) so the actual
    // URL must be read from <cite> text elements.
    // Google result links have the destination URL directly in href.
    const foundURL: string = useGoogle
      ? await page.evaluate(() => {
          const links = Array.from(document.querySelectorAll("a[href]")) as HTMLAnchorElement[];
          for (const a of links) {
            const href = a.href ?? "";
            const m = href.match(/linkedin\.com\/in\/([^/?#&]+)/);
            if (m && m[1]) return `https://www.linkedin.com/in/${m[1]}/`;
          }
          return "";
        })
      : await page.evaluate(() => {
          // Primary: cite text (the display URL Bing shows under each result title)
          const cites = Array.from(document.querySelectorAll("cite"));
          for (const cite of cites) {
            const text = (cite.textContent ?? "").replace(/\s*[›>]\s*/g, "/").trim();
            const m = text.match(/(?:https?:\/\/)?(?:[a-z]+\.)?linkedin\.com\/in\/([^/?#&\s]+)/i);
            if (m && m[1]) return `https://www.linkedin.com/in/${m[1]}/`;
          }
          // Fallback: direct href if Bing happens to include one
          const links = Array.from(document.querySelectorAll("a[href]")) as HTMLAnchorElement[];
          for (const a of links) {
            const href = a.href ?? "";
            const m = href.match(/linkedin\.com\/in\/([^/?#&]+)/);
            if (m && m[1]) return `https://www.linkedin.com/in/${m[1]}/`;
          }
          return "";
        });

    if (!foundURL) {
      throw new Error(`No LinkedIn profile found for keywords: ${keywords}`);
    }
    console.log(`[search] found profile URL: ${foundURL}`);

    // Click the first matching result link so LinkedIn sees an authentic Referer.
    const clickSelectors = useGoogle
      ? ["a[href*='linkedin.com/in/']", "div#search h3 a"]
      : ["li.b_algo h2 a", "a[href*='linkedin.com/in/']"];

    let clicked = false;
    for (const sel of clickSelectors) {
      const el = await page.$(sel);
      if (!el) continue;
      const href = await el.evaluate((a) => a.getAttribute("href"));
      console.log(`[search] clicking: ${(href ?? "").slice(0, 80)}`);
      try {
        await Promise.all([
          page.waitForNavigation({ waitUntil: "domcontentloaded", timeout: 30_000 }),
          el.click(),
        ]);
        clicked = true;
        break;
      } catch (e: any) {
        console.log(`[search] click/nav error: ${e.message}`);
      }
    }

    if (!clicked) {
      console.log(`[search] no link clicked — navigating directly to ${foundURL}`);
      try {
        await page.goto(foundURL, { waitUntil: "domcontentloaded", timeout: 30_000 });
      } catch (e: any) {
        if (!String(e).includes("timeout") && !String(e).includes("Timeout")) throw e;
      }
    }

    const liTitle = await page.title();
    const liURL = page.url();
    console.log(`[search] LinkedIn title: "${liTitle}"  url: ${liURL}`);

    if (
      liTitle.toLowerCase().includes("linkedin login") ||
      liTitle.toLowerCase().includes("sign in") ||
      liURL.includes("/login") ||
      liURL.includes("/authwall")
    ) {
      throw new Error(
        `LinkedIn redirected to login wall (title: "${liTitle}"). ` +
        `Try again in a few minutes or configure a residential proxy.`
      );
    }

    await new Promise((r) => setTimeout(r, 3_000));
    const html = await page.content();
    console.log(`[search] got ${html.length} bytes from ${liURL}`);
    return { profileURL: liURL, html };

  } finally {
    if (page) await page.close().catch(() => {});
    await pool.release(browser);
  }
}

// ---------------------------------------------------------------------------
// Generic HTML/JSON fetch  (POST /fetch)
// ---------------------------------------------------------------------------

interface FetchRequest {
  url: string;
  method?: "GET" | "POST";
  headers?: Record<string, string>;
  body?: unknown;
  response_type?: "html" | "json";  // default: json
}

async function fetchGeneric(req: FetchRequest): Promise<unknown> {
  const browser = await pool.acquire();
  let page: Page | undefined;
  try {
    page = await setupPage(browser);
    if (req.headers && Object.keys(req.headers).length > 0) {
      await page.setExtraHTTPHeaders(req.headers);
    }

    await page.goto(req.url, { waitUntil: "domcontentloaded", timeout: 60_000 });

    if (req.response_type === "html") {
      await new Promise((r) => setTimeout(r, 2_000));
      return { html: await page.content() };
    }

    const bodyText = await page.evaluate(() => document.body.innerText);
    return JSON.parse(bodyText);
  } finally {
    if (page) await page.close().catch(() => {});
    await pool.release(browser);
  }
}

// ---------------------------------------------------------------------------
// HTTP server
// ---------------------------------------------------------------------------

function readBody(req: http.IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url ?? "/", `http://localhost:${PORT}`);

  // GET /health
  if (url.pathname === "/health") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status: "ok", service: "puppeteer-service" }));
    return;
  }

  // POST /fetch-linkedin  — Google search → click → HTML
  if (url.pathname === "/fetch-linkedin" && req.method === "POST") {
    let body: { url?: string };
    try {
      body = JSON.parse(await readBody(req));
    } catch {
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "invalid JSON body" }));
      return;
    }
    if (!body.url) {
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "missing field: url" }));
      return;
    }
    console.log(`[api] /fetch-linkedin ${body.url}`);
    try {
      const html = await fetchLinkedInHTML(body.url);
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ html }));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error(`[api] /fetch-linkedin error: ${msg}`);
      res.writeHead(500, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: msg }));
    }
    return;
  }

  // POST /search-linkedin  — keyword search → first LinkedIn profile → HTML
  if (url.pathname === "/search-linkedin" && req.method === "POST") {
    let body: { keywords?: string; engine?: string };
    try {
      body = JSON.parse(await readBody(req));
    } catch {
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "invalid JSON body" }));
      return;
    }
    if (!body.keywords) {
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "missing field: keywords" }));
      return;
    }
    const engine = body.engine ?? "bing";
    console.log(`[api] /search-linkedin keywords="${body.keywords}" engine="${engine}"`);
    try {
      const result = await searchLinkedInProfile(body.keywords, engine);
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ html: result.html, profileURL: result.profileURL }));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error(`[api] /search-linkedin error: ${msg}`);
      res.writeHead(500, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: msg }));
    }
    return;
  }

  // POST /fetch  — generic fetch (existing API)
  if (url.pathname === "/fetch" && req.method === "POST") {
    let fetchReq: FetchRequest;
    try {
      fetchReq = JSON.parse(await readBody(req));
    } catch {
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "invalid JSON body" }));
      return;
    }
    if (!fetchReq.url) {
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "missing field: url" }));
      return;
    }
    console.log(`[api] /fetch ${fetchReq.method ?? "GET"} ${fetchReq.url}`);
    try {
      const data = await fetchGeneric(fetchReq);
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify(data));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error(`[api] /fetch error: ${msg}`);
      res.writeHead(500, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: msg }));
    }
    return;
  }

  res.writeHead(404, { "Content-Type": "application/json" });
  res.end(JSON.stringify({ error: "not found" }));
});

// ---------------------------------------------------------------------------
// Start
// ---------------------------------------------------------------------------

await pool.init();

if (PROXY_ENABLED) {
  const testBrowser = await pool.acquire();
  await testProxyConnectivity(testBrowser);
  await pool.release(testBrowser);
}

server.listen(PORT, "0.0.0.0", () => {
  console.log(`[puppeteer-service] listening on :${PORT}  proxy=${PROXY_ENABLED}`);
});

process.on("SIGTERM", async () => {
  console.log("[puppeteer-service] SIGTERM — shutting down");
  await pool.destroy();
  server.close();
  process.exit(0);
});
