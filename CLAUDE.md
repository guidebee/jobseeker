# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Jobseeker is an AI-powered job application assistant built in Go that:
- Scrapes job boards (SEEK, LinkedIn, and Indeed)
- Uses Claude Sonnet 4.5 to analyze job matches based on user profiles and resumes
- Tracks applications in SQLite with multi-user support
- Features profile caching, resume integration, and smart filtering

**Important**: This is designed as a SaaS-ready application with multi-user support, user isolation, and subscription limits built in from the start.

## Development Commands

### Build & Run
```bash
# Build the application
go build -o jobseeker.exe ./cmd/jobseeker

# Or use Makefile
make build

# Run without building (development)
go run ./cmd/jobseeker [command]
make run
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Test specific package
go test ./internal/scraper -v

# Run with coverage
go test -cover ./...

# Run scraper-specific tests
go test ./internal/scraper/scraper_test.go -v
```

### Dependencies
```bash
# Download and tidy dependencies
go mod download
go mod tidy
make deps
```

### Application Commands
```bash
# Initialize user profile (must run first)
jobseeker init
jobseeker init --github username --linkedin url
jobseeker init --force  # Force refresh

# Scan job boards
jobseeker scan

# Analyze jobs with Claude AI
jobseeker analyze
jobseeker analyze --contract
jobseeker analyze --type permanent

# List jobs
jobseeker list --recommended
jobseeker list --contract --recommended
jobseeker list --limit 20
```

## Architecture

### High-Level Flow
```
User Profile Setup (init)
    ↓
Job Scanning (scraper) → Database
    ↓
AI Analysis (analyzer) → Claude API
    ↓
User Review (list)
    ↓
[Future] Application Executor
```

### Multi-User Architecture

The application is built for multiple users with complete data isolation:

- **User Identification**: Users identified by email address (from config.yaml)
- **Environment Variable**: `USER_EMAIL` in .env must match email in config.yaml
- **Data Scoping**: All jobs, analyses, and applications are scoped to `user_id` via database foreign keys
- **Profile Caching**: The `ProfileData` table stores cached resumes, GitHub repos, and keywords per user
- **Subscription System**: Built-in `PlanType` field on users for future SaaS deployment (free/premium/enterprise)

**Key Files**:
- `internal/database/models.go`: User, Job, Application, ProfileData models with `UserID` foreign keys
- `internal/database/user.go`: `GetOrCreateUser()`, `GetUserByEmail()` functions
- `cmd/jobseeker/init.go`: User profile initialization with caching

### Core Components

#### 1. Database Layer (`internal/database/`)

**Models** (`models.go`):
- `User`: Multi-user support with subscription plans, usage limits
- `Job`: Job postings with `UserID` foreign key for isolation. Unique constraint: `(ExternalID, UserID)`
- `Application`: Tracks submissions per user
- `ProfileData`: Caches resumes, GitHub repos, LinkedIn data per user

**Helpers** (`helpers.go`, `user.go`):
- `DetectJobType()`: Identifies contract vs permanent roles from title/salary/URL
- `GetOrCreateUser()`: User management
- `GetUserByEmail()`: Retrieve user by email
- `GetUserStats()`: Usage statistics per user

**Key Patterns**:
- Uses GORM ORM for database operations
- SQLite backend via glebarez/sqlite driver
- Automatic migrations in `InitDB()`
- All queries filter by `user_id` to ensure data isolation

#### 2. Scraper (`internal/scraper/scraper.go`)

**Technology**: Colly web scraping framework with async/concurrent support

**Architecture**:
- HTML selector-based extraction using CSS selectors and data attributes
- Rate limiting: 2s delay + 1-2s random delay, parallelism=1-2 depending on site
- Politeness: Only scrapes allowed domains (seek.com.au, linkedin.com, indeed.com)
- Anti-blocking measures: Sets User-Agent, Referer, and Origin headers per domain

**SEEK Scraper** (`ScrapeSeek()`):
- Job card: `article[data-testid='job-card']`
- Title: `a[data-testid='job-card-title']`
- Company: Links ending with `-jobs` or containing `advertiserid=`
- Location: Links containing `/in-`
- Salary: Spans containing `$`
- External ID format: `seek-{job_id}`

**LinkedIn Scraper** (`ScrapeLinkedIn()`):
- Job card: `<li>` elements containing job links
- Job link pattern: `a[href*='/jobs/view/']`
- Title: `<h3>` tags within job card
- Company: `<h4>` tags within job card
- Location: `span.job-search-card__location` or spans with comma-separated text
- Salary: `span.job-search-card__salary-info` (rarely present)
- External ID format: `linkedin-{job_id}`
- **Note**: LinkedIn uses more JavaScript rendering, so scraping may be less reliable than SEEK

**Indeed Scraper** (`ScrapeIndeed()`):
- Job card: `div.job_seen_beacon`, `div.slider_container`, or `td.resultContent`
- Job title: `h2.jobTitle a` or `a.jcs-JobTitle`
- Company: `span.companyName` or `span[data-testid='company-name']`
- Location: `div.companyLocation` or `div[data-testid='text-location']`
- Salary: `div.salary-snippet` or `div.metadata.salary-snippet-container`
- Job key: Extracted from `data-jk` attribute
- External ID format: `indeed-{job_key}` from jk= query parameter
- **Note**: Indeed actively blocks scrapers, proper headers (Referer, Origin) are critical

**Rate Limiting Strategy**:
- SEEK: 2s base delay, 1s random, parallelism=2
- LinkedIn: 2s base delay, 2s random, parallelism=1 (more conservative due to stricter anti-scraping)
- Indeed: 2s base delay, 2s random, parallelism=1 (strict anti-scraping measures)

**Header Strategy** (set in `OnRequest` callback):
- SEEK: Referer=https://www.seek.com.au/, Origin=https://www.seek.com.au
- LinkedIn: Referer=https://www.linkedin.com/, Origin=https://www.linkedin.com
- Indeed: Referer=https://au.indeed.com/, Origin=https://au.indeed.com
- All sites: User-Agent set to Chrome 120, Accept and Accept-Language headers

**Saving Logic** (`SaveJobs()`):
- Checks for duplicates using `(external_id, user_id)` composite key
- Skips existing jobs per user
- Generates unique `ExternalID` from URL via `extractJobID()`

#### 3. Analyzer (`internal/analyzer/analyzer.go`)

**Core Functionality**:
- Sends job descriptions + user profile/resume to Claude API
- Returns structured JSON with match score (0-100), reasoning, pros/cons
- Smart resume selection based on job type and keywords

**Resume Integration**:
- Loads `.docx` files from `./resumes/` directory
- `SelectBestResume()`: Matches resume to job type (contract/permanent) or keywords
- Falls back to config.yaml if no resumes found

**Prompt Strategies**:
- `buildResumeBasedPrompt()`: Uses actual resume content (preferred, limited to 2000 chars)
- `buildConfigBasedPrompt()`: Uses config.yaml skills/experience (fallback)
- Includes salary/rate preferences based on detected job type

**Response Parsing**:
- Handles Claude's JSON responses (strips markdown code blocks)
- Extracts `AnalysisResult` struct with match_score, reasoning, pros, cons

#### 4. Profile Management (`internal/profile/`)

**Config Loading** (`profile.go`):
- Parses `configs/config.yaml` using YAML unmarshal
- Stores name, email, skills, experience, preferences, job board URLs
- Separate salary preferences for permanent vs contract roles

**Resume Loading** (`internal/resume/loader.go`):
- Parses `.docx` files using `nguyenthenguyen/docx` library
- Extracts text content from Word documents
- `SelectBestResume()`: Keyword matching logic (contract/permanent, senior/lead/backend/etc.)

**Keyword Extraction** (`internal/resume/keywords.go`):
- Uses Claude API to extract primary skills and roles from resume
- Cached in `ProfileData.SearchKeywords` during init
- Used for better job matching

#### 5. Claude API Client (`pkg/claude/client.go`)

**Implementation**:
- HTTP client with 60s timeout
- API endpoint: `https://api.anthropic.com/v1/messages`
- Default model: `claude-sonnet-4-5-20250929`
- Max tokens: 4096

**Headers Required**:
- `Content-Type: application/json`
- `x-api-key: {CLAUDE_API_KEY}`
- `anthropic-version: 2023-06-01`

**Usage Pattern**:
```go
client := claude.NewClient(apiKey)
response, err := client.SendMessage(prompt)
```

#### 6. GitHub Integration (`pkg/github/github.go`)

**Purpose**: Fetch user's public repositories to enrich profile
**Endpoint**: `https://api.github.com/users/{username}/repos`
**Storage**: Cached as JSON in `ProfileData.GitHubRepos` during init

### Command Structure (`cmd/jobseeker/`)

**CLI Framework**: Cobra (spf13/cobra)

**Commands**:
- `main.go`: Root command setup, global flags (`--config`, `--database`)
- `init.go`: Profile initialization command
- `scan.go`: Job scraping command
- `analyze.go`: AI analysis command
- `list.go`: Display jobs with filters

**Global Initialization** (`initApp()`):
- Called by most commands
- Initializes database connection
- Loads profile from config.yaml
- Returns `*profile.Profile` for use by command

### Configuration Files

**`.env`**:
- `CLAUDE_API_KEY`: Required for AI analysis
- `USER_EMAIL`: Must match email in config.yaml (multi-user support)
- `CLAUDE_MODEL`: Optional, defaults to sonnet-4.5
- `DB_PATH`: Database location
- `SCRAPER_DELAY_MS`: Delay between requests
- `MATCH_THRESHOLD`: Minimum score for recommendations
- `GITHUB_USERNAME`, `LINKEDIN_URL`: Optional for init command

**`configs/config.yaml`**:
- User profile (name, email, skills, experience)
- Separate salary preferences for permanent (`salary_min`) and contract (`contract.hourly_rate_min`, `contract.daily_rate_min`)
- Job board search URLs (multiple URLs per board)

## Important Patterns & Conventions

### Error Handling
- Always use `fmt.Errorf()` with `%w` to wrap errors with context
- Return errors up the call stack, don't panic
- Log warnings with `log.Printf()` for non-fatal issues

### Job Type Detection
The `DetectJobType()` function determines contract vs permanent:
- Checks title for keywords: "contract", "c2c", "contractor", "hourly"
- Checks salary for: "hour", "day" (not "per year")
- Checks URL for: "contract-jobs"
- Defaults to "unknown" if detection fails

**Why this matters**: Different salary preferences apply to different job types, and resume selection uses this.

### Resume Selection Logic
Located in `internal/resume/loader.go:SelectBestResume()`:
1. First priority: Match job type (contract → "contract" in filename)
2. Second priority: Match keywords in title to filename (senior, backend, etc.)
3. Fallback: Return first resume

### User Isolation Pattern
Every database query must filter by `user_id`:
```go
db.Where("user_id = ?", userID).Find(&jobs)
db.Where("external_id = ? AND user_id = ?", externalID, userID)
```

The unique constraint on jobs is `(external_id, user_id)` - same job can exist for multiple users.

### Async Scraping
The scraper uses Colly's async mode:
- `c.Async(true)` enables concurrent requests
- Must call `c.Wait()` after `Visit()` to ensure completion
- Rate limiting via `LimitRule` prevents overwhelming servers

## Common Development Tasks

### Adding a New Job Board

1. Create scraper method in `internal/scraper/scraper.go`:
```go
func (s *Scraper) ScrapeLinkedIn(searchURL string) ([]*database.Job, error) {
    // Implement HTML parsing logic
}
```

2. Add domain to collector:
```go
colly.AllowedDomains("linkedin.com", "www.linkedin.com")
```

3. Update `cmd/jobseeker/scan.go` to call new method

4. Add config section to `configs/config.yaml`

### Updating HTML Selectors (when sites change structure)

Both SEEK and LinkedIn scrapers use CSS selectors that may break when sites update their HTML.

**SEEK Scraper** (`internal/scraper/scraper.go:51-157`):
- Job card: `article[data-testid='job-card']`
- Title: `a[data-testid='job-card-title']`
- Company: Links ending with `-jobs` or containing `advertiserid=`
- Location: Links containing `/in-`
- Salary: Spans containing `$`

**LinkedIn Scraper** (`internal/scraper/scraper.go:189-326`):
- Job card: `<li>` elements with `a[href*='/jobs/view/']`
- Title: `<h3>` tags
- Company: `<h4>` tags
- Location: `span.job-search-card__location` with fallback to comma-containing spans
- Salary: `span.job-search-card__salary-info`

**Indeed Scraper** (`internal/scraper/scraper.go:328-501`):
- Job card: `div.job_seen_beacon`, `div.slider_container`, `td.resultContent`
- Title: `h2.jobTitle a`, `a.jcs-JobTitle`
- Company: `span.companyName`, `span[data-testid='company-name']`
- Location: `div.companyLocation`, `div[data-testid='text-location']`
- Salary: `div.salary-snippet`

**To debug selector issues**:
1. Visit the job board search URL in browser
2. Open DevTools → Inspect job card element
3. Find `data-testid` attributes, class names, or unique patterns
4. Update selectors in the appropriate `OnHTML()` callback
5. Test with `go test ./internal/scraper/scraper_test.go -v`

**LinkedIn-specific challenges**:
- Heavy JavaScript rendering (scraper only gets initial HTML)
- More aggressive anti-bot measures
- Location text sometimes mixed with posting dates ("2 weeks ago", "Actively Hiring")
- Salary rarely shown in search results
- May require longer delays or user-agent headers to avoid blocks

**Important LinkedIn Limitations**:
LinkedIn's job search pages are heavily JavaScript-driven, meaning most content is loaded dynamically after the initial page load. The current scraper has the following limitations:

1. **Limited Job Discovery**: May find 0 jobs even when jobs exist, because content is loaded via JavaScript after page load
2. **Workarounds**:
   - Use LinkedIn's public job pages (no login required) which have better static HTML
   - Consider using a headless browser solution (e.g., chromedp, selenium) for JavaScript rendering
   - Focus primarily on SEEK for reliable scraping, use LinkedIn as secondary source
3. **User-Agent Required**: The scraper sets a realistic browser User-Agent to avoid immediate blocking
4. **Multiple Selector Strategies**: Uses 2 different selector strategies to handle LinkedIn's various page layouts

**Recommendation**: For production use, consider using LinkedIn's official API (requires partnership) or focus on SEEK which has more reliable HTML structure.

### Running Single Tests

Use table-driven tests pattern:
```bash
# Run specific test
go test -run TestScrapeSeek ./internal/scraper -v

# Run with race detector
go test -race ./...
```

## Debugging Tips

### Enable Verbose Logging
Add to code:
```go
log.Printf("Debug: job = %+v\n", job)  // %+v prints struct field names
```

### Check Database Contents
```bash
# Install SQLite CLI
# Query users
sqlite3 jobseeker.db "SELECT * FROM users;"

# Check job counts per user
sqlite3 jobseeker.db "SELECT user_id, COUNT(*) FROM jobs GROUP BY user_id;"

# View recommended jobs
sqlite3 jobseeker.db "SELECT title, company, match_score FROM jobs WHERE status='recommended';"
```

### Test Claude API Separately
```go
client := claude.NewClient(apiKey)
response, err := client.SendMessage("Hello, Claude!")
fmt.Println(response)
```

### Resume Parsing Issues
If `.docx` parsing fails:
- Ensure file is not corrupted
- Check for Word temporary files (`~$*.docx`)
- Verify `nguyenthenguyen/docx` library supports format

## Future Enhancements (Planned)

Based on README roadmap:
- Web dashboard (likely using Gin or Fiber framework)
- Cover letter generation (already has `GenerateCoverLetter()` in analyzer)
- Automated application submission
- Email monitoring for recruiter responses
- Resume tailoring per job
- Interview scheduling

## Known Limitations

1. **Web Scraping Fragility**: Selectors break when job boards update HTML structure
2. **LinkedIn Rendering**: Heavy JavaScript use means scraper only sees initial HTML, may miss dynamically loaded content
3. **Anti-Scraping Measures**:
   - LinkedIn actively blocks scrapers; may need user-agent rotation or longer delays
   - Indeed blocks requests without proper headers (403 errors) - now mitigated with Referer/Origin headers
   - Both may block based on IP address with high request volumes
4. **Resume Format**: Only supports `.docx`, not PDF or plain text
5. **Rate Limiting**: No built-in retry logic for API failures
6. **Error Recovery**: Partial scrape failures don't rollback transactions
7. **Resume Selection**: Simple keyword matching, could use semantic similarity

## Project Structure Summary

```
cmd/jobseeker/          # CLI commands (Cobra)
├── main.go            # Entry point, global flags
├── init.go            # Profile initialization
├── scan.go            # Job scraping
├── analyze.go         # AI analysis
└── list.go            # Display jobs

internal/               # Private application code
├── database/          # GORM models, user management
│   ├── models.go     # User, Job, Application, ProfileData
│   ├── db.go         # Database initialization
│   ├── user.go       # User CRUD operations
│   └── helpers.go    # DetectJobType(), stats
├── scraper/          # Web scraping (Colly)
│   ├── scraper.go    # SEEK scraper implementation
│   └── scraper_test.go
├── analyzer/         # Claude AI integration
│   └── analyzer.go   # Job matching, prompt building
├── profile/          # Config loading
│   └── profile.go    # YAML parsing
└── resume/           # Resume handling
    ├── loader.go     # .docx parsing, selection
    └── keywords.go   # Claude-based keyword extraction

pkg/                   # Reusable libraries
├── claude/           # Claude API client
│   └── client.go
└── github/           # GitHub API integration
    └── github.go

configs/
└── config.yaml       # User profile, preferences, search URLs

resumes/              # User resumes (.docx)
└── *.docx

.env                  # API keys, environment config
jobseeker.db          # SQLite database
```

## Testing Strategy

Current test coverage is minimal. Priority areas for adding tests:

1. **Unit Tests**:
   - `internal/database/helpers.go`: Test `DetectJobType()` with various inputs
   - `internal/resume/loader.go`: Test `SelectBestResume()` logic
   - `internal/analyzer/analyzer.go`: Test prompt building, JSON parsing

2. **Integration Tests**:
   - End-to-end flow: scan → analyze → list
   - Multi-user data isolation

3. **Scraper Tests** (existing):
   - `internal/scraper/scraper_test.go`: Live SEEK scraping test
   - Run with: `go test ./internal/scraper/scraper_test.go -v`