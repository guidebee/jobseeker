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

const PROXY_HOST = process.env.SOLSCAN_PROXY_HOST ?? "";
const PROXY_PORT_NUM = Number(process.env.SOLSCAN_PROXY_PORT ?? 823);
const PROXY_USER = process.env.SOLSCAN_PROXY_USER ?? "";
const PROXY_PASS = process.env.SOLSCAN_PROXY_PASS ?? "";
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
  await page.setUserAgent(pickRandom(USER_AGENTS));
  // Proxy auth is handled by the local relay — no page.authenticate() needed.
  // Referer and Accept-Language make the request look like a real browser
  // navigating from a Google search result.
  await page.setExtraHTTPHeaders({
    "Referer": "https://www.google.com/search?q=linkedin+profile",
    "Accept-Language": "en-US,en;q=0.9",
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

// acceptGoogleConsent clicks "Accept all" on Google's cookie consent page if present.
// Google shows this page on first visit in many regions (AU, EU, etc.).
async function acceptGoogleConsent(page: Page): Promise<void> {
  const title = await page.title();
  // Consent page titles: "Before you continue to Google Search", "Avant de continuer", etc.
  // Also detect when document.title is the raw URL (another consent page symptom).
  const isConsentPage =
    title.includes("Before you continue") ||
    title.includes("Avant de continuer") ||
    title.startsWith("https://") ||
    title === "";

  if (!isConsentPage) return;

  console.log(`[linkedin] Google consent page detected (title: "${title}") — accepting`);

  // Try common "Accept all" button selectors across locales.
  for (const sel of [
    'button[aria-label*="Accept all"]',
    'button[aria-label*="Tout accepter"]',
    "button#L2AGLb",          // common ID for the accept button
    'form[action*="consent"] button',
    'div[role="none"] button', // fallback
  ]) {
    try {
      await page.waitForSelector(sel, { timeout: 3_000 });
      await Promise.all([
        page.waitForNavigation({ waitUntil: "domcontentloaded", timeout: 15_000 }),
        page.click(sel),
      ]);
      console.log(`[linkedin] consent accepted via "${sel}"`);
      return;
    } catch {
      // selector not present — try next
    }
  }
  console.log("[linkedin] could not find consent accept button — continuing anyway");
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
