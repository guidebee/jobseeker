// Package browser provides a Go port of the TypeScript puppeteer-service.
// It manages a pool of persistent headless Chrome browsers with the stealth
// plugin applied, and Cloudflare challenge detection — no separate Node.js
// process required.
//
// Env vars (all optional):
//
//	BROWSER_POOL_SIZE          number of persistent browsers (default 2)
//	PUPPETEER_EXECUTABLE_PATH  path to Chrome/Chromium binary
//	SCAN_PROXY_HOST         proxy host (when residential proxy is needed)
//	SCAN_PROXY_PORT         proxy port (default 823)
//	SCAN_PROXY_USER         proxy username
//	SCAN_PROXY_PASS         proxy password
package browser

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
)

// ---------------------------------------------------------------------------
// Fingerprint entropy pools — mirrors the TS service
// ---------------------------------------------------------------------------

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
}

type viewport struct{ w, h int }

var viewports = []viewport{
	{1920, 1080},
	{1440, 900},
	{1366, 768},
	{1280, 800},
}

// ---------------------------------------------------------------------------
// Pool — Go port of the TypeScript BrowserPool class
// ---------------------------------------------------------------------------

// Pool manages a fixed set of persistent headless Chrome browsers.
// Each browser handles one request at a time; pages are opened and closed
// per request while the browser process stays alive.
type Pool struct {
	ch        chan *rod.Browser
	size      int
	proxyURL  string
	proxyUser string
	proxyPass string
	remote    bool // true when connected to an existing Chrome (not launched by us)
}

// NewPool creates a Pool. Reads config from environment variables.
func NewPool() *Pool {
	size := 2
	if v := os.Getenv("BROWSER_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			size = n
		}
	}

	host := os.Getenv("SCAN_PROXY_HOST")
	port := os.Getenv("SCAN_PROXY_PORT")
	user := os.Getenv("SCAN_PROXY_USER")
	pass := os.Getenv("SCAN_PROXY_PASS")
	if port == "" {
		port = "823"
	}

	// Embed credentials directly in the proxy URL — this is the only method
	// that Chrome honours for proxy (407) auth in legacy headless mode.
	proxyURL := ""
	if host != "" {
		if user != "" {
			proxyURL = fmt.Sprintf("http://%s:%s@%s:%s", user, pass, host, port)
		} else {
			proxyURL = fmt.Sprintf("http://%s:%s", host, port)
		}
	}

	return &Pool{
		ch:        make(chan *rod.Browser, size),
		size:      size,
		proxyURL:  proxyURL,
		proxyUser: user,
		proxyPass: pass,
	}
}

// Init connects to or launches browsers.
//
// If CHROME_REMOTE_DEBUG_URL is set (e.g. http://localhost:9222), the pool
// connects to that existing Chrome process instead of launching a new headless
// one. This is useful on Windows where spawned headless Chrome processes are
// sometimes blocked from making network requests by Windows Defender / Firewall.
//
// To enable: start Chrome with --remote-debugging-port=9222, then set
// CHROME_REMOTE_DEBUG_URL=http://localhost:9222 in .env.
func (p *Pool) Init() error {
	if remoteURL := os.Getenv("CHROME_REMOTE_DEBUG_URL"); remoteURL != "" {
		return p.initRemote(remoteURL)
	}
	return p.initHeadless()
}

func (p *Pool) initHeadless() error {
	log.Printf("[browser] initialising pool of %d headless browser(s)", p.size)
	for i := 0; i < p.size; i++ {
		b, err := p.launchBrowser()
		if err != nil {
			return fmt.Errorf("browser %d: %w", i, err)
		}
		p.ch <- b
	}
	log.Printf("[browser] pool ready")
	return nil
}

func (p *Pool) initRemote(debugURL string) error {
	log.Printf("[browser] connecting to existing Chrome at %s", debugURL)
	wsURL, err := launcher.ResolveURL(debugURL)
	if err != nil {
		return fmt.Errorf("cannot resolve Chrome debug URL %s: %w", debugURL, err)
	}
	b := rod.New().ControlURL(wsURL)
	if err := b.Connect(); err != nil {
		return fmt.Errorf("failed to connect to Chrome at %s: %w", debugURL, err)
	}
	log.Printf("[browser] connected to existing Chrome")
	p.remote = true
	// All pool slots share the same browser connection.
	for i := 0; i < p.size; i++ {
		p.ch <- b
	}
	return nil
}

// Close shuts down idle browsers. No-op for remote connections (we don't own the process).
func (p *Pool) Close() {
	if p.remote {
		return
	}
	for {
		select {
		case b := <-p.ch:
			b.Close() //nolint:errcheck
		default:
			return
		}
	}
}

// acquire blocks until a browser is free.
func (p *Pool) acquire() *rod.Browser {
	return <-p.ch
}

// release returns a browser to the pool, replacing it if it has crashed.
// Uses Version() as a lightweight liveness probe.
func (p *Pool) release(b *rod.Browser) {
	if p.remote {
		p.ch <- b
		return
	}
	if _, err := b.Version(); err != nil {
		log.Println("[browser] browser disconnected — replacing")
		b.Close() //nolint:errcheck
		fresh, err := p.launchBrowser()
		if err != nil {
			log.Printf("[browser] WARNING: could not replace browser: %v", err)
			return
		}
		b = fresh
	}
	p.ch <- b
}

func (p *Pool) launchBrowser() (*rod.Browser, error) {
	l := launcher.New().
		Headless(true).
		Leakless(false) // leakless.exe is blocked by Windows Defender on Windows
	// Note: NoSandbox / disable-setuid-sandbox / disable-dev-shm-usage are
	// Linux/CI flags. On Windows they are either no-ops or can interfere with
	// Chrome's network stack — leave them out.

	if p.proxyURL != "" {
		l = l.Set("proxy-server", p.proxyURL)
	}

	if exe := os.Getenv("PUPPETEER_EXECUTABLE_PATH"); exe != "" {
		l = l.Bin(exe)
	}

	log.Printf("[browser] launching Chrome (headless)")

	wsURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch Chrome: %w", err)
	}
	log.Printf("[browser] Chrome DevTools URL: %s", wsURL)

	b := rod.New().ControlURL(wsURL)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to Chrome: %w", err)
	}

	return b, nil
}

// ---------------------------------------------------------------------------
// FetchHTML — Go port of fetchViaProxy (GET path)
// ---------------------------------------------------------------------------

// FetchHTML navigates to url through a stealth browser and returns the fully
// rendered outer HTML after any Cloudflare challenge resolves.
func (p *Pool) FetchHTML(targetURL string) (string, error) {
	b := p.acquire()
	defer p.release(b)

	// Overall 3-minute timeout. Individual operations have their own shorter
	// caps so they can fail fast without burning the whole budget.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Set up proxy credentials. HandleAuth intercepts the CDP auth challenge
	// Chrome raises when the proxy demands credentials.
	var stopAuth func() error
	if p.proxyUser != "" {
		stopAuth = b.HandleAuth(p.proxyUser, p.proxyPass)
	}
	defer func() {
		if stopAuth != nil {
			stopAuth() //nolint:errcheck
		}
	}()

	// Open a stealth page — patches navigator.webdriver, Canvas, WebGL, etc.
	log.Printf("[browser] opening stealth page for %s", targetURL)
	page, err := stealth.Page(b)
	if err != nil {
		return "", fmt.Errorf("failed to create stealth page: %w", err)
	}
	defer page.Close()

	// Bind the timeout context so Navigate/WaitLoad/Eval all respect it.
	page = page.Context(ctx)

	// Randomise viewport and user-agent per request to reduce session uniformity.
	vp := viewports[rand.Intn(len(viewports))]
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             vp.w,
		Height:            vp.h,
		DeviceScaleFactor: 1,
	}); err != nil {
		return "", fmt.Errorf("failed to set viewport: %w", err)
	}

	ua := userAgents[rand.Intn(len(userAgents))]
	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: ua}); err != nil {
		return "", fmt.Errorf("failed to set user agent: %w", err)
	}

	// Navigate with a 60s cap. rod's Navigate waits for networkAlmostIdle which
	// never fires through a proxy — timeout is expected and non-fatal.
	log.Printf("[browser] navigating to %s", targetURL)
	if err := page.Timeout(60 * time.Second).Navigate(targetURL); err != nil {
		if !isTimeoutErr(err) {
			return "", fmt.Errorf("navigation failed: %w", err)
		}
		log.Println("[browser] navigate timed out — stopping pending loads via CDP")
		// Use CDP Page.stopLoading — works at protocol level even when Chrome has
		// no window/document yet (e.g. still mid-TCP-handshake through the proxy).
		stopErr := proto.PageStopLoading{}.Call(page)
		if stopErr != nil {
			log.Printf("[browser] CDP stop error: %v", stopErr)
		}
		// Belt-and-suspenders JS stop once the CDP stop has taken effect.
		time.Sleep(500 * time.Millisecond)
		page.Timeout(3 * time.Second).Eval(`() => window.stop()`) //nolint:errcheck
	}

	// Log the page title — always log so we can diagnose blank/error pages.
	if r, err := page.Timeout(8 * time.Second).Eval(`() => document.title`); err != nil {
		log.Printf("[browser] title eval failed: %v", err)
	} else {
		log.Printf("[browser] page title: %q", r.Value.String())
	}

	// Log current URL to confirm we're on the right page.
	if info, err := page.Info(); err != nil {
		log.Printf("[browser] page.Info error: %v", err)
	} else {
		log.Printf("[browser] current URL: %s", info.URL)
	}

	// Wait for any Cloudflare challenge to resolve before reading HTML.
	log.Println("[browser] checking for Cloudflare challenge...")
	if err := waitForChallengeResolution(page); err != nil {
		return "", err
	}

	// Give JS a moment to finish rendering dynamic content.
	time.Sleep(3 * time.Second)

	// Use Eval with an explicit timeout instead of page.HTML() which can block
	// indefinitely on a page that is still processing network responses.
	log.Println("[browser] reading page HTML...")
	result, err := page.Timeout(40 * time.Second).Eval(`() => document.documentElement.outerHTML`)
	if err != nil {
		return "", fmt.Errorf("failed to get page HTML: %w", err)
	}

	html := result.Value.String()
	log.Printf("[browser] got %d bytes", len(html))
	return html, nil
}

// ---------------------------------------------------------------------------
// FetchLinkedInHTML — Google-referral approach for LinkedIn bot avoidance
// ---------------------------------------------------------------------------

// FetchLinkedInHTML replicates the manual flow: open a fresh browser, search
// Google for the LinkedIn profile, click the result. Cookies are cleared first
// to simulate an incognito session without using Chrome's incognito context
// (which is unreliable with go-rod on Windows).
func (p *Pool) FetchLinkedInHTML(profileURL string) (string, error) {
	b := p.acquire()
	defer p.release(b)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	var stopAuth func() error
	if p.proxyUser != "" {
		stopAuth = b.HandleAuth(p.proxyUser, p.proxyPass)
	}
	defer func() {
		if stopAuth != nil {
			stopAuth() //nolint:errcheck
		}
	}()

	page, err := stealth.Page(b)
	if err != nil {
		return "", fmt.Errorf("failed to create stealth page: %w", err)
	}
	defer page.Close()
	page = page.Context(ctx)

	vp := viewports[rand.Intn(len(viewports))]
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width: vp.w, Height: vp.h, DeviceScaleFactor: 1,
	}); err != nil {
		return "", fmt.Errorf("failed to set viewport: %w", err)
	}
	ua := userAgents[rand.Intn(len(userAgents))]
	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: ua}); err != nil {
		return "", fmt.Errorf("failed to set user agent: %w", err)
	}

	// Clear all cookies for headless mode to simulate a fresh incognito session.
	// Skipped for remote Chrome — clearing would wipe real browser sessions.
	if !p.remote {
		if err := (proto.NetworkClearBrowserCookies{}).Call(page); err != nil {
			log.Printf("[browser] warning: could not clear cookies: %v", err)
		} else {
			log.Println("[browser] cookies cleared (fresh session)")
		}
	}

	// Connectivity test: navigate to a data URL (no network needed).
	// If this fails, Chrome's CDP connection is broken. If it succeeds but
	// http://example.com fails, there is a network/proxy/firewall block.
	log.Println("[browser] connectivity test 1: data URL (no network)")
	if err := page.Timeout(5 * time.Second).Navigate("data:text/html,<h1>ok</h1>"); err != nil {
		log.Printf("[browser] FATAL: even data URL navigation failed — CDP broken: %v", err)
	} else {
		log.Println("[browser] connectivity test 1 passed")
	}

	log.Println("[browser] connectivity test 2: http://example.com")
	navigateWithStop(page, "http://example.com/", 15*time.Second)
	if info, err := page.Info(); err != nil {
		log.Printf("[browser] connectivity test 2 page.Info error: %v", err)
	} else {
		log.Printf("[browser] connectivity test 2 URL: %s", info.URL)
	}
	logPageTitle(page, "example.com")

	// Pre-set Google consent cookie so the AU/EU consent page never appears.
	setGoogleConsentCookie(page)

	// Step 1: navigate to a Google search for the LinkedIn profile.
	profileID := linkedInID(profileURL)
	searchURL := "https://www.google.com/search?q=" +
		url.QueryEscape("linkedin "+profileID) + "&hl=en"
	log.Printf("[browser] step 1: navigating to Google search — %s", searchURL)

	navigateWithStop(page, searchURL, 60*time.Second)

	if info, err := page.Info(); err != nil {
		log.Printf("[browser] page.Info error after Google nav: %v", err)
	} else {
		log.Printf("[browser] URL after Google nav: %s", info.URL)
	}
	logPageTitle(page, "Google search")

	// Log a snippet of the page body to confirm we actually got search results.
	if r, err := page.Timeout(5 * time.Second).Eval(`() => document.body ? document.body.innerText.slice(0, 300) : "(no body)"`); err != nil {
		log.Printf("[browser] body preview error: %v", err)
	} else {
		log.Printf("[browser] body preview: %s", r.Value.String())
	}

	time.Sleep(2 * time.Second)

	// Step 2: find and click the LinkedIn result so Chrome generates an authentic
	// link-click navigation (Referer + sec-fetch-site: cross-site).
	log.Println("[browser] step 2: looking for LinkedIn result to click")
	clicked := false
	for _, sel := range []string{
		fmt.Sprintf(`a[href*="linkedin.com/in/%s"]`, profileID),
		`a[href*="linkedin.com/in/"]`,
	} {
		el, elErr := page.Timeout(8 * time.Second).Element(sel)
		if elErr != nil {
			log.Printf("[browser] selector %q not found: %v", sel, elErr)
			continue
		}
		href, _ := el.Attribute("href")
		log.Printf("[browser] found LinkedIn link: %v — clicking", href)
		waitNav := page.Timeout(60 * time.Second).WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
		if clickErr := el.Click(proto.InputMouseButtonLeft, 1); clickErr != nil {
			log.Printf("[browser] click error: %v", clickErr)
			continue
		}
		waitNav()
		proto.PageStopLoading{}.Call(page) //nolint:errcheck
		clicked = true
		break
	}

	if !clicked {
		log.Println("[browser] no LinkedIn link found in SERP — JS redirect from Google context")
		waitNav := page.Timeout(60 * time.Second).WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
		page.Eval(fmt.Sprintf(`() => { window.location.href = %q }`, profileURL)) //nolint:errcheck
		waitNav()
		proto.PageStopLoading{}.Call(page) //nolint:errcheck
	}

	time.Sleep(500 * time.Millisecond)

	if info, err := page.Info(); err != nil {
		log.Printf("[browser] page.Info error after LinkedIn nav: %v", err)
	} else {
		log.Printf("[browser] URL after LinkedIn nav: %s", info.URL)
	}
	logPageTitle(page, "LinkedIn")

	log.Println("[browser] checking for Cloudflare challenge...")
	if err := waitForChallengeResolution(page); err != nil {
		return "", err
	}

	time.Sleep(3 * time.Second)

	log.Println("[browser] reading page HTML...")
	result, err := page.Timeout(40 * time.Second).Eval(`() => document.documentElement.outerHTML`)
	if err != nil {
		return "", fmt.Errorf("failed to get page HTML: %w", err)
	}
	html := result.Value.String()
	log.Printf("[browser] got %d bytes of HTML", len(html))
	return html, nil
}

// SearchAndFetchLinkedIn does a Google keyword search (site:linkedin.com/in KEYWORDS),
// finds the first matching LinkedIn profile URL in the SERP, clicks it to generate
// an authentic Referer, and returns the profile URL and rendered HTML.
func (p *Pool) SearchAndFetchLinkedIn(keywords string) (profileURL, html string, err error) {
	b := p.acquire()
	defer p.release(b)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	var stopAuth func() error
	if p.proxyUser != "" {
		stopAuth = b.HandleAuth(p.proxyUser, p.proxyPass)
	}
	defer func() {
		if stopAuth != nil {
			stopAuth() //nolint:errcheck
		}
	}()

	page, err := stealth.Page(b)
	if err != nil {
		return "", "", fmt.Errorf("failed to create stealth page: %w", err)
	}
	defer page.Close()
	page = page.Context(ctx)

	vp := viewports[rand.Intn(len(viewports))]
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width: vp.w, Height: vp.h, DeviceScaleFactor: 1,
	}); err != nil {
		return "", "", fmt.Errorf("failed to set viewport: %w", err)
	}
	ua := userAgents[rand.Intn(len(userAgents))]
	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: ua}); err != nil {
		return "", "", fmt.Errorf("failed to set user agent: %w", err)
	}

	if !p.remote {
		if err := (proto.NetworkClearBrowserCookies{}).Call(page); err != nil {
			log.Printf("[browser] warning: could not clear cookies: %v", err)
		} else {
			log.Println("[browser] cookies cleared (fresh session)")
		}
	}

	// Step 1: Bing keyword search. No site: operator — both Google and Bing
	// CAPTCHA headless browsers on operator queries. Appending "linkedin" steers
	// results toward profile pages without triggering operator-based bot detection.
	query := keywords + " linkedin"
	searchURL := "https://www.bing.com/search?q=" + url.QueryEscape(query) + "&setlang=en"
	log.Printf("[browser] step 1: LinkedIn keyword search (Bing) — %s", searchURL)

	navigateWithStop(page, searchURL, 60*time.Second)
	time.Sleep(2 * time.Second)
	logPageTitle(page, "Google search")

	if r, err := page.Timeout(5 * time.Second).Eval(`() => document.body ? document.body.innerText.slice(0, 300) : "(no body)"`); err != nil {
		log.Printf("[browser] body preview error: %v", err)
	} else {
		log.Printf("[browser] body preview: %s", r.Value.String())
	}

	// Step 2: extract the first LinkedIn profile URL from Bing SERP.
	// Bing result links use opaque redirect hrefs (bing.com/ck/a?...) — the actual
	// URL is only in <cite> text like "au.linkedin.com › in › guidebee".
	r, evalErr := page.Timeout(10 * time.Second).Eval(`() => {
		const cites = Array.from(document.querySelectorAll('cite'));
		for (const cite of cites) {
			const text = (cite.textContent || '').replace(/\s*[›>]\s*/g, '/').trim();
			const m = text.match(/(?:https?:\/\/)?(?:[a-z]+\.)?linkedin\.com\/in\/([^/?#&\s]+)/i);
			if (m && m[1]) return 'https://www.linkedin.com/in/' + m[1] + '/';
		}
		const links = Array.from(document.querySelectorAll('a[href]'));
		for (const a of links) {
			const href = a.href || '';
			const m = href.match(/linkedin\.com\/in\/([^/?#&]+)/);
			if (m && m[1]) return 'https://www.linkedin.com/in/' + m[1] + '/';
		}
		return '';
	}`)
	if evalErr != nil {
		return "", "", fmt.Errorf("failed to extract LinkedIn URL from SERP: %w", evalErr)
	}
	foundURL := r.Value.String()
	if foundURL == "" {
		return "", "", fmt.Errorf("no LinkedIn profile found for keywords: %q", keywords)
	}
	log.Printf("[browser] step 2: found profile URL: %s", foundURL)

	// Step 3: click the first Bing result title link — it redirects through Bing
	// to LinkedIn, so LinkedIn sees Referer: https://www.bing.com/ (trusted).
	clicked := false
	for _, sel := range []string{
		`li.b_algo h2 a`,
		`a[href*="linkedin.com/in/"]`,
	} {
		el, elErr := page.Timeout(8 * time.Second).Element(sel)
		if elErr != nil {
			log.Printf("[browser] selector %q not found: %v", sel, elErr)
			continue
		}
		href, _ := el.Attribute("href")
		log.Printf("[browser] step 3: clicking LinkedIn link: %v", derefStr(href))
		waitNav := page.Timeout(60 * time.Second).WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
		if clickErr := el.Click(proto.InputMouseButtonLeft, 1); clickErr != nil {
			log.Printf("[browser] click error: %v", clickErr)
			continue
		}
		waitNav()
		proto.PageStopLoading{}.Call(page) //nolint:errcheck
		clicked = true
		break
	}

	if !clicked {
		log.Printf("[browser] no link to click — JS redirect to %s", foundURL)
		waitNav := page.Timeout(60 * time.Second).WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
		page.Eval(fmt.Sprintf(`() => { window.location.href = %q }`, foundURL)) //nolint:errcheck
		waitNav()
		proto.PageStopLoading{}.Call(page) //nolint:errcheck
	}

	time.Sleep(500 * time.Millisecond)

	if info, infoErr := page.Info(); infoErr == nil {
		log.Printf("[browser] URL after LinkedIn nav: %s", info.URL)
		profileURL = info.URL
	} else {
		profileURL = foundURL
	}
	logPageTitle(page, "LinkedIn")

	log.Println("[browser] checking for Cloudflare challenge...")
	if err := waitForChallengeResolution(page); err != nil {
		return "", "", err
	}

	time.Sleep(3 * time.Second)

	log.Println("[browser] reading page HTML...")
	result, err := page.Timeout(40 * time.Second).Eval(`() => document.documentElement.outerHTML`)
	if err != nil {
		return "", "", fmt.Errorf("failed to get page HTML: %w", err)
	}
	htmlContent := result.Value.String()
	log.Printf("[browser] got %d bytes of HTML", len(htmlContent))
	return profileURL, htmlContent, nil
}

// navigateWithStop navigates to url, tolerating timeouts by issuing a CDP
// Page.stopLoading so subsequent JS evals are not blocked.
func navigateWithStop(page *rod.Page, url string, timeout time.Duration) {
	if err := page.Timeout(timeout).Navigate(url); err != nil {
		if isTimeoutErr(err) {
			log.Printf("[browser] navigate timed out (%s) — stopping via CDP", url)
		} else {
			log.Printf("[browser] navigate error (%s): %v", url, err)
		}
		proto.PageStopLoading{}.Call(page) //nolint:errcheck
		time.Sleep(500 * time.Millisecond)
	}
}

// logPageTitle evals document.title and logs it, including any error.
func logPageTitle(page *rod.Page, label string) {
	if r, err := page.Timeout(8 * time.Second).Eval(`() => document.title`); err != nil {
		log.Printf("[browser] %s title eval failed: %v", label, err)
	} else {
		log.Printf("[browser] %s title: %q", label, r.Value.String())
	}
}

// linkedInID extracts the vanity ID from a LinkedIn profile URL.
// "https://www.linkedin.com/in/guidebee/" → "guidebee"
func linkedInID(profileURL string) string {
	parts := strings.Split(strings.TrimRight(profileURL, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return profileURL
}

// derefStr safely dereferences a *string.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------------------------------------------------------------------------
// Cloudflare challenge detection — port of waitForChallengeResolution
// ---------------------------------------------------------------------------

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "deadline") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "context canceled")
}

func waitForChallengeResolution(page *rod.Page) error {
	const total = 20 * time.Second
	const evalTimeout = 3 * time.Second
	const poll = 500 * time.Millisecond

	deadline := time.Now().Add(total)
	challengeDetected := false

	for time.Now().Before(deadline) {
		// Each Eval call gets its own 3s cap so a blocked page can't stall the loop.
		result, err := page.Timeout(evalTimeout).Eval(`() => {
			const t = document.title;
			if (t.includes("Just a moment") || t.includes("Attention Required")) return true;
			return !!(
				document.querySelector("#cf-challenge-running") ||
				document.querySelector('iframe[src*="challenges.cloudflare.com"]') ||
				document.querySelector("#turnstile-wrapper") ||
				document.querySelector(".cf-challenge-container")
			);
		}`)
		if err != nil {
			// Eval error (page navigating, context cancelled) — treat as resolved.
			return nil
		}

		var isChallenge bool
		if err := result.Value.Unmarshal(&isChallenge); err != nil {
			return nil
		}

		if isChallenge {
			if !challengeDetected {
				challengeDetected = true
				log.Println("[cf] Cloudflare challenge detected — waiting for resolution")
			}
		} else {
			if challengeDetected {
				log.Println("[cf] Cloudflare challenge resolved")
			}
			return nil
		}
		time.Sleep(poll)
	}

	return fmt.Errorf("Cloudflare challenge did not resolve within %s", total)
}

// setGoogleConsentCookie pre-sets Google's SOCS consent cookie via CDP so the
// AU/EU consent page never appears. Must be called before any google.com navigation.
// CDP Network.setCookie works for any domain without requiring prior navigation.
func setGoogleConsentCookie(page *rod.Page) {
	_, err := proto.NetworkSetCookie{
		Name:   "SOCS",
		Value:  "CAI",
		Domain: ".google.com",
		Path:   "/",
	}.Call(page)
	if err != nil {
		log.Printf("[browser] warning: could not set Google consent cookie: %v", err)
	} else {
		log.Println("[browser] Google consent cookie set (SOCS=CAI)")
	}
}
