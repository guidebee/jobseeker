# LinkedIn Profile Search

This document covers how to fetch and search LinkedIn public profiles using the two profile commands: `linkedin` and `findlinkedin`.

---

## Commands Overview

| Command | When to use | Input |
|---------|-------------|-------|
| `linkedin` | You already have the profile ID or URL | `guidebee` or `https://www.linkedin.com/in/guidebee/` |
| `findlinkedin` | You only know the person's name, employer, or location | `James Shen "Victorian Electoral Commission"` |

---

## Prerequisites

Both commands require the **Puppeteer stealth service** running locally. LinkedIn blocks headless browsers without it.

### 1. Install the service (first time only)

```bash
cd puppeteer-service
npm install
```

### 2. Configure the service (`puppeteer-service/.env`)

```bash
# Residential proxy (recommended — improves LinkedIn success rate)
SOLSCAN_PROXY_HOST=gw.dataimpulse.com
SOLSCAN_PROXY_PORT=823
SOLSCAN_PROXY_USER=your_proxy_username
SOLSCAN_PROXY_PASS=your_proxy_password

# Service settings
PUPPETEER_SERVICE_PORT=3001
PUPPETEER_POOL_SIZE=2
```

Proxy is optional but strongly recommended — LinkedIn blocks datacenter IPs frequently. See [Proxy Configuration](#proxy-configuration) for details.

### 3. Start the service (in a separate terminal)

```bash
cd puppeteer-service
npm start
```

Expected output:
```
[pool] 2 browser(s) ready
[puppeteer-service] listening on :3001  proxy=true
```

### 4. Point the CLI at the service (`.env` in project root)

```bash
PUPPETEER_SERVICE_URL=http://localhost:3001
CLAUDE_API_KEY=sk-ant-...   # optional — enables skill inference
```

---

## `jobseeker linkedin` — Fetch by Profile ID or URL

Use this when you already know the LinkedIn profile ID or full URL.

### Usage

```bash
jobseeker linkedin <user-id>
jobseeker linkedin <full-profile-url>
```

### Examples

```bash
# By vanity ID (the part after /in/)
jobseeker linkedin guidebee

# By full URL
jobseeker linkedin https://www.linkedin.com/in/guidebee/

# Quoted IDs with hyphens
jobseeker linkedin john-doe-123456
```

### How it works

```
CLI
 │
 ├─ POST /fetch-linkedin to Puppeteer service
 │
 └─ Puppeteer service:
       1. Clear browser cookies (fresh session)
       2. Set Google consent cookie (SOCS=CAI) — bypasses AU/EU consent gate
       3. Navigate to Google: "linkedin guidebee" → click LinkedIn result
          (Google referral makes LinkedIn treat the request as organic)
       4. Wait for Cloudflare challenge to resolve (up to 20s)
       5. Return rendered HTML
 │
 └─ CLI: ParseHTML() → optional Claude skill inference → print CV
```

The Google-referral step is key: LinkedIn trusts requests that come from a Google search result click (the `Referer: https://www.google.com/` header signals organic traffic).

### Retry behaviour

The CLI retries up to **5 times** with a 5-second pause between attempts. Each attempt gets a fresh browser session, which may land on a different residential proxy IP.

---

## `jobseeker findlinkedin` — Find by Keywords

Use this when you only know someone's name, employer, location, or role — not their profile ID.

### Usage

```bash
jobseeker findlinkedin <keywords...>
```

All positional arguments are joined into a single search query. Use shell quoting to keep phrases together.

### Examples

```bash
# Name + employer
jobseeker findlinkedin James Shen "Victorian Electoral Commission"

# Name + location
jobseeker findlinkedin James Shen Australia

# Name + role + location
jobseeker findlinkedin John Smith "Senior Engineer" Google Sydney

# Name only (broader, may return wrong person)
jobseeker findlinkedin "Sarah Johnson"
```

### How it works

The command runs in two phases:

```
Phase 1 — Discover the profile URL via Bing
──────────────────────────────────────────────────────────────────────
 CLI → POST /search-linkedin { keywords } → Puppeteer service

 Puppeteer service:
   1. Clear browser cookies
   2. Navigate to Bing: "James Shen Victorian Electoral Commission linkedin"
      (plain keyword query — no site: operator, which triggers bot CAPTCHAs)
   3. Read <cite> display URLs in Bing SERP
      (Bing result links are opaque redirects; the real URL is only in <cite> text)
   4. Extract first linkedin.com/in/ URL
   5. Click first Bing result title → navigate to LinkedIn
      (Bing referral: LinkedIn sees Referer: https://www.bing.com/)
   6. Return HTML + resolved profile URL

Phase 2 — Parse the profile HTML
──────────────────────────────────────────────────────────────────────
 CLI: ParseHTML(html, profileURL)
   → if ErrNonEnglishPage: re-fetch via FetchLinkedIn(profileURL) and retry
   → if success: optional Claude skill inference → print CV
```

### Why Bing instead of Google?

Google detects `site:linkedin.com/in` operator queries from headless browsers and shows a CAPTCHA. A plain keyword query like `James Shen linkedin` avoids the operator but Google still CAPTCHAs aggressively on search from datacenter/headless IPs.

Bing serves these results to headless browsers without a CAPTCHA, making it the reliable choice for automated profile discovery.

### Retry behaviour

| Phase | Max retries | Retry trigger |
|-------|-------------|---------------|
| Search (Bing → LinkedIn URL → HTML) | 5 | Any error (network, timeout, no result) |
| Parse (HTML → Profile struct) | 5 | `ErrNonEnglishPage` — re-fetches HTML each time |

Non-English pages happen when the residential proxy IP resolves to a non-English region. Each re-fetch picks a fresh session, which may land on a different IP.

---

## Sample Output

Both commands produce the same structured CV output:

```
Found profile: https://www.linkedin.com/in/guidebee/

Inferring skills via Claude AI...

=== LinkedIn Profile (used as CV) ===

NAME: James Shen
LINKEDIN: https://www.linkedin.com/in/guidebee/

EXPERIENCE:
- Victorian Electoral Commission
  ...

CERTIFICATIONS:
- AWS Certified Solutions Architect — Amazon Web Services
- TOGAF 9 Certified — The Open Group
- Professional Scrum Master I — Scrum.org
  ...

PROJECTS:
- Solana Blockchain DeFi NFT
- asx_gym — OpenAI Gym environment for ASX stock market
- Guidebee Map API
  ...

LANGUAGES:
- Chinese (Native or bilingual proficiency)
- English (Professional working proficiency)

SKILLS:
Go, React, AWS, Kubernetes, Docker, Solana, Blockchain, Python,
Machine Learning, GIS, Android, TOGAF, Enterprise Architecture, ...
```

Skills are inferred by Claude AI from certifications, projects, and experience — LinkedIn hides the skills section from unauthenticated users. Skill inference requires `CLAUDE_API_KEY` in `.env`.

---

## Proxy Configuration

A residential proxy significantly improves success rates because:
- LinkedIn blocks datacenter IP ranges (AWS, Azure, GCP) aggressively
- Residential IPs appear to come from real home internet connections
- Rotating IPs avoid per-IP rate limits and bans

### Supported: DataImpulse

The service is pre-configured for [DataImpulse](https://dataimpulse.com) residential proxies.

**Auth modes:**

| Mode | Setup |
|------|-------|
| **IP whitelist** ✅ Recommended | Leave `SOLSCAN_PROXY_USER` and `SOLSCAN_PROXY_PASS` blank. All connections from your whitelisted IP are accepted automatically. |
| Username + password | Set both vars. The service starts a local relay that injects `Proxy-Authorization` into every HTTPS CONNECT tunnel. |

> **Note: prefer IP whitelist mode over username/password with DataImpulse.**
>
> The username/password mode routes traffic through a local HTTPS CONNECT tunnel relay. This relay can cause intermittent connection failures — the tunnel handshake sometimes stalls or drops mid-session, leading to Chrome network errors that are hard to diagnose. IP whitelist mode bypasses the relay entirely: Chrome connects directly to the DataImpulse gateway without any credential negotiation, which is simpler and more stable. If you find profile fetches randomly failing with proxy or network errors, switching to IP whitelist mode is the first thing to try.
>
> To enable IP whitelist mode: log in to your DataImpulse dashboard, whitelist your current public IP, then leave `SOLSCAN_PROXY_USER` and `SOLSCAN_PROXY_PASS` unset (or remove them) in `puppeteer-service/.env`.

**Force a specific country** (username/password mode only) by appending a routing suffix to the username:
```
SOLSCAN_PROXY_USER=myuser-country-AU   # Australian exit IP
SOLSCAN_PROXY_USER=myuser-country-US   # US exit IP
```

In IP whitelist mode, country routing is configured in the DataImpulse dashboard instead.

### Without a proxy

The service still works without a proxy using the built-in go-rod browser pool, but LinkedIn blocks most datacenter IPs. You may need to:
- Use `SKIP_GOOGLE_SEARCH=true` in `puppeteer-service/.env` to navigate directly (skips the Google referral step)
- Retry multiple times until an unblocked session succeeds

---

## Puppeteer Service Configuration Reference

Set these in `puppeteer-service/.env`:

| Variable | Default | Purpose |
|----------|---------|---------|
| `SOLSCAN_PROXY_HOST` | — | Residential proxy host |
| `SOLSCAN_PROXY_PORT` | `823` | Proxy port |
| `SOLSCAN_PROXY_USER` | — | Proxy username — leave blank for IP whitelist mode (recommended) |
| `SOLSCAN_PROXY_PASS` | — | Proxy password — leave blank for IP whitelist mode (recommended) |
| `SKIP_GOOGLE_SEARCH` | `false` | `true` = navigate to LinkedIn directly; `false` = go via Google SERP click |
| `PUPPETEER_SERVICE_PORT` | `3001` | HTTP port the service listens on |
| `PUPPETEER_POOL_SIZE` | `2` | Number of persistent browser instances |
| `PUPPETEER_EXECUTABLE_PATH` | — | Path to a custom Chrome/Chromium binary |

### Service endpoints

| Endpoint | Method | Body | Purpose |
|----------|--------|------|---------|
| `/health` | GET | — | Health check |
| `/fetch-linkedin` | POST | `{"url": "..."}` | Fetch profile by URL (Google referral approach) |
| `/search-linkedin` | POST | `{"keywords": "..."}` | Search Bing → find profile URL → fetch HTML |
| `/fetch` | POST | `{"url": "...", "response_type": "html"}` | Generic stealth page fetch |

---

## Troubleshooting

### `Puppeteer service unreachable`
```
Puppeteer service unreachable: ...
Start it with: cd puppeteer-service && npm start
```
→ Start the service in a separate terminal  
→ Check `PUPPETEER_SERVICE_URL=http://localhost:3001` is set in `.env`

### `LinkedIn redirected to login wall`
```
LinkedIn redirected to login wall (title: "LinkedIn Login")
```
→ LinkedIn blocked the request — likely a flagged IP  
→ Enable a residential proxy in `puppeteer-service/.env`  
→ Try `SKIP_GOOGLE_SEARCH=false` (default) to use the Google referral approach  
→ Wait a few minutes and retry (the service retries up to 5 times automatically)

### `No LinkedIn profile found for keywords`
```
No LinkedIn profile found for keywords: "..."
```
→ Bing returned results but none contained a `linkedin.com/in/` URL  
→ Try more specific keywords (add employer, location, or role)  
→ Try fewer keywords if the query is too narrow  
→ Check Bing manually: `James Shen Victorian Electoral Commission linkedin`

### `Got non-English page`
```
Got non-English page — try again or use a different proxy region
```
→ All 5 retries returned a non-English page (proxy IP is in a non-English region)  
→ Force an English-speaking country in proxy username: `SOLSCAN_PROXY_USER=myuser-country-AU`  
→ Retry the command — a fresh session may pick a different IP

### `No JavaScript / empty profile`
→ LinkedIn returned a page that requires JavaScript to render  
→ The Puppeteer service already waits 3 seconds for JS rendering  
→ Increase `PUPPETEER_POOL_SIZE` to keep more warm browsers ready  
→ Check service logs for Cloudflare challenge detection messages

---

## Tips for Better Results

**For `findlinkedin`:**
- Include the **employer name** — it's the strongest disambiguator: `James Shen "Victorian Electoral Commission"`
- Use **quoted phrases** for multi-word employers or locations: `"New South Wales"`
- Add **role or title** for common names: `John Smith "Software Engineer" Sydney`
- Avoid overly generic searches — `John Smith` alone will likely return the wrong person

**For `linkedin`:**
- The profile ID is the slug after `/in/` in the LinkedIn URL
- IDs with numbers are stable even if the person changes their name: `john-doe-123456`
- Full URLs also work: paste directly from the browser address bar

**General:**
- Run with `CLAUDE_API_KEY` set to get AI-inferred skills (LinkedIn hides the skills section from unauthenticated users)
- The residential proxy is the single biggest factor in success rate — worth setting up
- If one attempt fails, just run the command again — fresh sessions often succeed where previous ones didn't
