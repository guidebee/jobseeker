// Package browser provides a Go port of the TypeScript puppeteer-service.
// It manages a pool of persistent headless Chrome browsers with the stealth
// plugin applied, and Cloudflare challenge detection — no separate Node.js
// process required.
//
// Env vars (all optional):
//
//	BROWSER_POOL_SIZE          number of persistent browsers (default 2)
//	PUPPETEER_EXECUTABLE_PATH  path to Chrome/Chromium binary
//	SOLSCAN_PROXY_HOST         proxy host (when residential proxy is needed)
//	SOLSCAN_PROXY_PORT         proxy port (default 823)
//	SOLSCAN_PROXY_USER         proxy username
//	SOLSCAN_PROXY_PASS         proxy password
package browser

import (
	"context"
	"fmt"
	"log"
	"math/rand"
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
}

// NewPool creates a Pool. Reads config from environment variables.
func NewPool() *Pool {
	size := 2
	if v := os.Getenv("BROWSER_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			size = n
		}
	}

	host := os.Getenv("SOLSCAN_PROXY_HOST")
	port := os.Getenv("SOLSCAN_PROXY_PORT")
	user := os.Getenv("SOLSCAN_PROXY_USER")
	pass := os.Getenv("SOLSCAN_PROXY_PASS")
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

// Init launches all browsers. Must be called once before FetchHTML.
func (p *Pool) Init() error {
	log.Printf("[browser] initialising pool of %d browser(s)", p.size)
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

// Close shuts down all idle browsers in the pool.
func (p *Pool) Close() {
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
		NoSandbox(true).
		Set("disable-setuid-sandbox").
		Set("disable-dev-shm-usage")

	if p.proxyURL != "" {
		l = l.Set("proxy-server", p.proxyURL)
	}

	if exe := os.Getenv("PUPPETEER_EXECUTABLE_PATH"); exe != "" {
		l = l.Bin(exe)
	}

	wsURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch Chrome: %w", err)
	}

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

// FetchLinkedInHTML fetches a LinkedIn profile page by first navigating to a
// Google search for the profile, then clicking the LinkedIn result so Chrome
// sends an authentic Referer header. Direct navigation to LinkedIn without a
// referrer triggers the signup-wall redirect.
func (p *Pool) FetchLinkedInHTML(profileURL string) (string, error) {
	b := p.acquire()
	defer p.release(b)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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

	// Step 1: visit Google homepage to plant a genuine Google session in Chrome
	// (cookies, TLS session, HTTP/2 connection).
	log.Println("[browser] step 1: visiting Google to seed session")
	navigateWithStop(page, "https://www.google.com/", 30*time.Second)
	logPageTitle(page, "Google")

	// Step 2: navigate to LinkedIn by evaluating window.location.href from
	// within the Google page context. This lets Chrome set the navigation
	// headers authentically:
	//   Referer: https://www.google.com/
	//   sec-fetch-site: cross-site
	//   sec-fetch-mode: navigate
	//   sec-fetch-dest: document
	// SetExtraHeaders injection can't reproduce these — Chrome would still
	// send sec-fetch-site: none, which LinkedIn detects as suspicious.
	log.Printf("[browser] step 2: navigating to LinkedIn from Google context")
	waitNav := page.Timeout(60 * time.Second).WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
	page.Eval(fmt.Sprintf(`() => { window.location.href = %q }`, profileURL)) //nolint:errcheck
	waitNav()
	proto.PageStopLoading{}.Call(page) //nolint:errcheck
	time.Sleep(500 * time.Millisecond)

	// Diagnostics.
	logPageTitle(page, "LinkedIn")
	if info, err := page.Info(); err == nil {
		log.Printf("[browser] current URL: %s", info.URL)
	}

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
	log.Printf("[browser] got %d bytes", len(html))
	return html, nil
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
