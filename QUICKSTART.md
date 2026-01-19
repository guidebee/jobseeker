# Quick Start Guide

New to Go? Follow these steps to get your job seeker running in 10 minutes.

## Step-by-Step Setup

### 1. Install Go (5 minutes)

**Windows:**
1. Download installer: https://go.dev/dl/
2. Run the `.msi` file
3. Open Command Prompt and verify:
   ```cmd
   go version
   ```
   You should see: `go version go1.21.x windows/amd64`

**Mac:**
```bash
brew install go
go version
```

**Linux:**
```bash
wget https://go.dev/dl/go1.21.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
go version
```

### 2. Get Claude API Key (2 minutes)

1. Go to https://console.anthropic.com/
2. Sign up or log in
3. Navigate to "API Keys"
4. Click "Create Key"
5. Copy your key (starts with `sk-ant-`)

### 3. Configure the Project (2 minutes)

1. **Set your API key:**
   ```cmd
   # Windows
   copy .env.example .env
   notepad .env

   # Mac/Linux
   cp .env.example .env
   nano .env
   ```

   Paste your Claude API key:
   ```
   CLAUDE_API_KEY=sk-ant-api03-your-key-here
   ```

2. **Update your profile:**
   ```cmd
   notepad configs\config.yaml
   ```

   Change these fields:
   - `name`: Your full name
   - `email`: Your email
   - `skills`: Your actual tech skills
   - `experience`: Your years of experience
   - `salary_min`: Your minimum salary
   - `locations`: Where you want to work

### 4. Install Dependencies (1 minute)

```cmd
cd C:\workspace\jobseeker
go mod download
```

This downloads all required libraries (colly, cobra, gorm, etc.)

### 5. Build and Run (1 minute)

**Build the executable:**
```cmd
go build -o jobseeker.exe ./cmd/jobseeker
```

**Test it works:**
```cmd
jobseeker.exe --help
```

You should see the help menu!

## Your First Job Search

### Scan for Jobs
```cmd
jobseeker.exe scan
```
This will:
- Visit SEEK's job listings
- Extract job details (title, company, salary)
- Save to database (`jobseeker.db`)

**Expected output:**
```
Starting job scan...
Scanning SEEK...
Visiting: https://www.seek.com.au/...
Found job: Senior Go Developer at Tech Co
Found job: Backend Engineer at Startup Inc
...
✓ Scan complete! Found 15 total jobs
```

### Analyze with AI
```cmd
jobseeker.exe analyze
```
This will:
- Send each job to Claude AI
- Get match scores (0-100)
- Save analysis to database

**Expected output:**
```
Analyzing jobs with Claude AI...
Found 15 jobs to analyze

[1/15] Analyzing: Senior Go Developer at Tech Co
  ✓ Match: 85/100 - RECOMMENDED
[2/15] Analyzing: Frontend Developer at Agency
  ○ Match: 45/100 - Below threshold
...
✓ Analysis complete!
  Recommended jobs: 8
  Below threshold: 7
```

### View Your Matches
```cmd
jobseeker.exe list --recommended
```

**Expected output:**
```
Found 8 jobs:

1. Senior Go Developer
   Company: Tech Co | Location: Melbourne
   Salary: $120k-150k
   Status: recommended | Match Score: 85/100
   Analysis: Strong match - you have 4/5 required skills...
   URL: https://seek.com.au/job/12345

2. Backend Engineer
   ...
```

## Understanding the Output

### Match Scores
- **85-100**: Excellent match - apply immediately
- **70-84**: Good match - worth considering
- **50-69**: Partial match - missing some requirements
- **0-49**: Poor match - not recommended

### Job Status
- `discovered`: Newly scraped, not yet analyzed
- `recommended`: AI says it's a good match
- `rejected`: Below your match threshold
- `applied`: You've submitted an application

## Customizing Your Search

### Change Match Threshold
Edit `.env`:
```
MATCH_THRESHOLD=80  # Only show jobs scoring 80+
```

### Update SEEK Search
Edit `configs/config.yaml`:
```yaml
job_boards:
  seek:
    enabled: true
    search_url: "https://www.seek.com.au/jobs?keywords=golang&location=sydney"
```

Find your search URL:
1. Go to seek.com.au
2. Search for your ideal job
3. Copy the URL from your browser
4. Paste into config.yaml

### Adjust Scraping Speed
Edit `.env`:
```
SCRAPER_DELAY_MS=3000  # Wait 3 seconds between requests (be polite!)
```

## Troubleshooting

### "go: command not found"
Go isn't installed or not in PATH.
- Restart your terminal after installing Go
- Windows: Check "Environment Variables" has Go in PATH

### "CLAUDE_API_KEY not set"
You forgot to create `.env` file or add your key.
```cmd
copy .env.example .env
notepad .env
```

### "No jobs found"
SEEK's HTML structure changed (happens often).
- Check the search URL in config.yaml is valid
- Open an issue on GitHub for help updating selectors

### "Failed to parse JSON"
Claude returned unexpected format.
- This is rare - the prompt might need tweaking
- Check your API key is valid
- Ensure you have API credits

### Build errors
```cmd
go mod tidy
go mod download
go build -o jobseeker.exe ./cmd/jobseeker
```

## Advanced Features

### Generate Tailored CVs (NEW!)

Create job-specific CVs in Word format using Claude's document skills:

**1. Place your resume(s) in the `resumes/` directory:**
```cmd
mkdir resumes
# Copy your resume.docx into resumes/
```

**2. Add recruiter job descriptions to `jobdescriptions/`:**
```cmd
mkdir jobdescriptions
# Save JD from recruiter email as .docx
```

**3. Generate tailored CVs:**
```cmd
jobseeker.exe tailorcv
```

**What happens:**
- Analyzes each JD against your profile
- Shows match score and skill analysis
- Asks if you want to generate a tailored CV
- Creates professionally formatted Word CV (30-60 seconds)
- Saves to `tailored_cvs/` directory

**Batch mode (auto-process all):**
```cmd
jobseeker.exe tailorcv --batch
```
Only generates CVs for jobs with ≥60% match score.

**Expected output:**
```
Loaded 2 resume(s)
Found 1 job description(s) to process

Analyzing job description with Claude AI...

MATCH SCORE: 85/100

Resume Used: Backend_Senior_Engineer.docx

KEY SKILLS MATCHED:
  • Go programming
  • Microservices architecture
  • Docker & Kubernetes

Would you like to generate a tailored CV for this job? (y/n): y

Generating tailored CV using Claude Skills...
✓ SUCCESS! Tailored CV saved to: tailored_cvs/CV_Senior_Backend_Developer_TechCorp_20260119.docx
```

**What gets tailored:**
- Professional summary rewritten for the specific role
- Skills reordered (job requirements first)
- Work experience emphasizes relevant projects
- Keywords from JD incorporated naturally
- Professional Word formatting

**Important:** Never fabricates experience - only reorganizes existing content!

### Generate Cover Letters

Analyze recruiter JDs and create cover letters:

```cmd
jobseeker.exe checkjd
```

**Interactive workflow:**
1. Analyzes job description
2. Shows match analysis
3. Generates cover letter
4. Lets you refine iteratively
5. Saves final version

## Next Steps

Once you're comfortable with basics:

1. **Complete application packages**
   - Use `checkjd` for cover letters
   - Use `tailorcv` for customized CVs
   - Submit with confidence!

2. **Improve matching**
   - Customize Claude's analysis prompt
   - Add more criteria (company culture, tech stack)

3. **Automate everything**
   - Schedule scans with Windows Task Scheduler or cron
   - Batch process recruiter JDs
   - Submit applications automatically

4. **Build a dashboard**
   - Learn Go web frameworks (gin, fiber)
   - Create a web UI to review jobs

## Learning Go

### Recommended Order
1. **Go Tour**: https://go.dev/tour/
   - Interactive tutorial in your browser
   - Takes 1-2 hours

2. **Read this codebase**
   - Start with `cmd/jobseeker/main.go`
   - Follow imports to understand flow
   - All files have detailed comments

3. **Make small changes**
   - Add a new field to Job struct
   - Customize Claude's prompt
   - Add a new CLI command

4. **Go by Example**: https://gobyexample.com/
   - Practical examples of Go features
   - Copy and experiment

### Key Files to Read
- `cmd/jobseeker/main.go` - CLI setup (cobra)
- `pkg/claude/client.go` - HTTP requests & JSON
- `internal/database/models.go` - Structs & ORM
- `internal/scraper/scraper.go` - Web scraping & concurrency

## Getting Help

**Go Questions:**
- Official docs: https://go.dev/doc/
- Go Forum: https://forum.golangbridge.org/
- Reddit: r/golang

**Project Issues:**
- GitHub Issues: https://github.com/guidebee/jobseeker/issues
- Read README.md for detailed docs

**Claude API:**
- Docs: https://docs.anthropic.com/
- Discord: https://discord.gg/anthropic

Happy job hunting! 🚀
