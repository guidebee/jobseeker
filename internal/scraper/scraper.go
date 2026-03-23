package scraper

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
	"github.com/guidebee/jobseeker/internal/database"
	"gorm.io/gorm"
)

// Scraper handles job board scraping
type Scraper struct {
	delay      time.Duration
	httpClient *http.Client
}

// NewScraper creates a new scraper instance
func NewScraper(delayMs int) *Scraper {
	return &Scraper{
		delay: time.Duration(delayMs) * time.Millisecond,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"

// newCollector creates a fresh Colly collector for the given allowed domains.
// Each Scrape* method creates its own collector so OnHTML handlers never
// accumulate across multiple calls.
func (s *Scraper) newCollector(domains ...string) *colly.Collector {
	c := colly.NewCollector(
		colly.AllowedDomains(domains...),
		colly.Async(true),
		colly.UserAgent(browserUA),
	)

	c.OnRequest(func(r *colly.Request) {
		log.Printf("Visiting: %s", r.URL)

		// Common modern-browser headers (Sec-Fetch-* are required by many sites)
		r.Headers.Set("Upgrade-Insecure-Requests", "1")
		r.Headers.Set("Sec-Fetch-Dest", "document")
		r.Headers.Set("Sec-Fetch-Mode", "navigate")
		r.Headers.Set("Sec-Fetch-User", "?1")
		r.Headers.Set("Sec-CH-UA", `"Chromium";v="134", "Google Chrome";v="134", "Not-A.Brand";v="99"`)
		r.Headers.Set("Sec-CH-UA-Mobile", "?0")
		r.Headers.Set("Sec-CH-UA-Platform", `"Windows"`)

		reqURL := r.URL.String()
		switch {
		case strings.Contains(reqURL, "indeed.com"):
			r.Headers.Set("Referer", "https://au.indeed.com/")
			r.Headers.Set("Origin", "https://au.indeed.com")
			r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
			r.Headers.Set("Accept-Language", "en-AU,en;q=0.9")
			r.Headers.Set("Sec-Fetch-Site", "same-origin")
		case strings.Contains(reqURL, "seek.com.au"):
			r.Headers.Set("Referer", "https://www.seek.com.au/")
			r.Headers.Set("Origin", "https://www.seek.com.au")
			r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
			r.Headers.Set("Accept-Language", "en-AU,en;q=0.9")
			r.Headers.Set("Sec-Fetch-Site", "same-origin")
		default:
			r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
			r.Headers.Set("Accept-Language", "en-AU,en;q=0.9")
			r.Headers.Set("Sec-Fetch-Site", "none")
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("Error scraping %s: %v (Status: %d)", r.Request.URL, err, r.StatusCode)
	})

	return c
}

// ScrapeSeek scrapes job listings from SEEK
func (s *Scraper) ScrapeSeek(searchURL string) ([]*database.Job, error) {
	var jobs []*database.Job
	seen := make(map[string]bool) // dedup within a single page (promoted vs standard cards)

	c := s.newCollector("seek.com.au", "www.seek.com.au")
	c.Limit(&colly.LimitRule{ //nolint:errcheck
		DomainGlob:  "*seek.com.au*",
		Delay:       s.delay,
		RandomDelay: 1 * time.Second,
		Parallelism: 2,
	})

	c.OnHTML("article[data-testid='job-card']", func(e *colly.HTMLElement) {
		title := ""
		e.ForEach("a[data-testid='job-card-title']", func(_ int, el *colly.HTMLElement) {
			if title == "" {
				title = strings.TrimSpace(el.Text)
			}
		})

		company := ""
		e.ForEach("a", func(_ int, link *colly.HTMLElement) {
			href := link.Attr("href")
			text := strings.TrimSpace(link.Text)
			if text != "" && (strings.HasSuffix(href, "-jobs") || strings.Contains(href, "advertiserid=")) {
				if company == "" {
					company = text
				}
			}
		})

		location := ""
		e.ForEach("a", func(_ int, link *colly.HTMLElement) {
			href := link.Attr("href")
			text := strings.TrimSpace(link.Text)
			if text != "" && strings.Contains(href, "/in-") && !strings.Contains(href, "All") {
				if location == "" {
					location = text
				}
			}
		})

		salary := ""
		e.ForEach("span", func(_ int, span *colly.HTMLElement) {
			text := strings.TrimSpace(span.Text)
			if strings.Contains(text, "$") && len(text) < 100 {
				if salary == "" {
					salary = text
				}
			}
		})

		jobURL := ""
		e.ForEach("a[data-testid='job-card-title']", func(_ int, el *colly.HTMLElement) {
			if jobURL == "" {
				jobURL = e.Request.AbsoluteURL(el.Attr("href"))
			}
		})

		job := &database.Job{
			Source:   "seek",
			URL:      jobURL,
			Title:    title,
			Company:  company,
			Location: location,
			Salary:   salary,
			Status:   "discovered",
		}
		job.ExternalID = extractJobID(job.URL)
		job.JobType = database.DetectJobType(job.Title, job.Salary, job.URL)

		if job.Title != "" && job.URL != "" && !seen[job.ExternalID] {
			seen[job.ExternalID] = true
			jobs = append(jobs, job)
			log.Printf("Found job: %s at %s", job.Title, job.Company)
		}
	})

	if err := c.Visit(searchURL); err != nil {
		return nil, fmt.Errorf("failed to visit URL: %w", err)
	}
	c.Wait()

	return jobs, nil
}

// ScrapeLinkedIn scrapes LinkedIn job listings using LinkedIn's public guest APIs.
//
// Two-step approach (no JavaScript rendering required):
//  1. GET seeMoreJobPostings → HTML with job cards, URLs and IDs
//  2. GET jobs-guest/jobs/api/jobPosting/{id} → HTML with full description
func (s *Scraper) ScrapeLinkedIn(searchURL string) ([]*database.Job, error) {
	apiURL := linkedInSearchToAPI(searchURL)
	log.Printf("LinkedIn: fetching job list from %s", apiURL)

	jobs, err := s.fetchLinkedInJobList(apiURL)
	if err != nil {
		return nil, err
	}
	log.Printf("LinkedIn: found %d jobs, fetching descriptions...", len(jobs))

	for _, job := range jobs {
		jobID := linkedInJobIDFromExternal(job.ExternalID)
		if jobID == "" {
			continue
		}
		desc, err := s.fetchLinkedInDescription(jobID)
		if err != nil {
			log.Printf("LinkedIn: could not fetch description for %s: %v", job.Title, err)
			continue
		}
		job.Description = desc
		// Throttle description fetches to be polite
		time.Sleep(s.delay)
	}

	return jobs, nil
}

// fetchLinkedInJobList scrapes the seeMoreJobPostings API for job summaries.
func (s *Scraper) fetchLinkedInJobList(apiURL string) ([]*database.Job, error) {
	var jobs []*database.Job

	c := s.newCollector("www.linkedin.com", "linkedin.com", "au.linkedin.com")
	c.Limit(&colly.LimitRule{ //nolint:errcheck
		DomainGlob:  "*linkedin.com*",
		Delay:       s.delay,
		RandomDelay: 2 * time.Second,
		Parallelism: 1,
	})

	// Add LinkedIn-specific headers (OnRequest from newCollector already logs the URL)
	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Referer", "https://www.linkedin.com/jobs/search/")
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "en-AU,en;q=0.9")
	})

	// Each job card in the seeMoreJobPostings response
	c.OnHTML("div.base-card", func(e *colly.HTMLElement) {
		// Job URL — the full-link anchor covers the whole card
		jobURL := ""
		e.ForEach("a.base-card__full-link", func(_ int, a *colly.HTMLElement) {
			if jobURL == "" {
				jobURL = strings.TrimSpace(a.Attr("href"))
				// Strip tracking query params, keep clean URL
				if u, err := url.Parse(jobURL); err == nil {
					u.RawQuery = ""
					jobURL = u.String()
				}
			}
		})

		// Job ID from data-entity-urn="urn:li:jobPosting:1234567890"
		entityURN := e.Attr("data-entity-urn")
		jobID := urnToJobID(entityURN)

		title := strings.TrimSpace(e.ChildText("h3.base-search-card__title"))
		company := strings.TrimSpace(e.ChildText("h4.base-search-card__subtitle"))
		if company == "" {
			company = strings.TrimSpace(e.ChildText("a.hidden-nested-link"))
		}
		location := strings.TrimSpace(e.ChildText("span.job-search-card__location"))

		if title == "" || jobID == "" {
			return
		}

		// Normalise job URL: prefer /jobs/view/{id}/ for consistency
		if jobURL == "" || !strings.Contains(jobURL, "/jobs/view/") {
			jobURL = fmt.Sprintf("https://www.linkedin.com/jobs/view/%s/", jobID)
		}

		job := &database.Job{
			Source:     "linkedin",
			URL:        jobURL,
			Title:      title,
			Company:    company,
			Location:   location,
			Status:     "discovered",
			ExternalID: "linkedin-" + jobID,
		}
		job.JobType = database.DetectJobType(job.Title, job.Salary, job.URL)

		jobs = append(jobs, job)
		log.Printf("Found LinkedIn job: %s at %s (id=%s)", job.Title, job.Company, jobID)
	})

	if err := c.Visit(apiURL); err != nil {
		return nil, fmt.Errorf("linkedin: visit failed: %w", err)
	}
	c.Wait()

	return jobs, nil
}

// fetchLinkedInDescription fetches the full job description from LinkedIn's
// guest jobPosting API (no JS required).
func (s *Scraper) fetchLinkedInDescription(jobID string) (string, error) {
	apiURL := fmt.Sprintf("https://www.linkedin.com/jobs-guest/jobs/api/jobPosting/%s", jobID)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-AU,en;q=0.9")
	req.Header.Set("Referer", "https://www.linkedin.com/jobs/search/")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		return "", fmt.Errorf("linkedin description API returned %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	// The description is inside div.show-more-less-html__markup
	descHTML, _ := doc.Find("div.show-more-less-html__markup").Html()
	desc := htmlToText(descHTML)

	return strings.TrimSpace(desc), nil
}

// htmlToText converts simple HTML to plain text by stripping tags and
// converting common block elements to newlines.
func htmlToText(h string) string {
	// Replace block-level tags with newlines
	for _, tag := range []string{"</p>", "<br>", "<br/>", "<br />", "</li>", "</h1>", "</h2>", "</h3>", "</h4>"} {
		h = strings.ReplaceAll(h, tag, tag+"\n")
	}
	// Strip remaining tags
	inTag := false
	var sb strings.Builder
	for _, ch := range h {
		switch {
		case ch == '<':
			inTag = true
		case ch == '>':
			inTag = false
		case !inTag:
			sb.WriteRune(ch)
		}
	}
	// Collapse blank lines
	lines := strings.Split(sb.String(), "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// linkedInSearchToAPI converts a LinkedIn search page URL to the seeMoreJobPostings
// guest API endpoint, which returns static HTML (no JS required).
//
// e.g. https://www.linkedin.com/jobs/search/?keywords=golang&location=Melbourne
//
//	→ https://www.linkedin.com/jobs-guest/jobs/api/seeMoreJobPostings/search?keywords=golang&location=Melbourne&start=0
func linkedInSearchToAPI(searchURL string) string {
	u, err := url.Parse(searchURL)
	if err != nil {
		return searchURL
	}
	if strings.Contains(u.Path, "jobs-guest") {
		return searchURL // already the API URL
	}

	q := u.Query()
	if q.Get("start") == "" {
		q.Set("start", "0")
	}
	return "https://www.linkedin.com/jobs-guest/jobs/api/seeMoreJobPostings/search?" + q.Encode()
}

// urnToJobID extracts the numeric job ID from a LinkedIn URN.
// e.g. "urn:li:jobPosting:4381775795" → "4381775795"
func urnToJobID(urn string) string {
	parts := strings.Split(urn, ":")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// linkedInJobIDFromExternal extracts the raw job ID from an ExternalID like "linkedin-4381775795".
func linkedInJobIDFromExternal(externalID string) string {
	return strings.TrimPrefix(externalID, "linkedin-")
}

// ScrapeIndeed scrapes job listings from Indeed
func (s *Scraper) ScrapeIndeed(searchURL string) ([]*database.Job, error) {
	var jobs []*database.Job

	c := s.newCollector("indeed.com", "www.indeed.com", "au.indeed.com")
	c.Limit(&colly.LimitRule{ //nolint:errcheck
		DomainGlob:  "*indeed.com*",
		Delay:       s.delay,
		RandomDelay: 2 * time.Second,
		Parallelism: 1,
	})

	c.OnHTML("div.job_seen_beacon, div.slider_container, td.resultContent", func(e *colly.HTMLElement) {
		var jobURL, title, company, location, salary, jobKey string

		e.ForEach("h2.jobTitle a, a.jcs-JobTitle", func(_ int, link *colly.HTMLElement) {
			if title == "" {
				title = strings.TrimSpace(link.Text)
				href := link.Attr("href")
				if href != "" {
					jobURL = link.Request.AbsoluteURL(href)
				}
				jobKey = link.Attr("data-jk")
				if jobKey == "" {
					jobKey = link.Attr("id")
				}
			}
		})

		if title == "" {
			e.ForEach("h2 a[data-jk]", func(_ int, link *colly.HTMLElement) {
				if title == "" {
					title = strings.TrimSpace(link.Text)
					href := link.Attr("href")
					if href != "" {
						jobURL = link.Request.AbsoluteURL(href)
					}
					jobKey = link.Attr("data-jk")
				}
			})
		}

		e.ForEach("span.companyName, span[data-testid='company-name']", func(_ int, span *colly.HTMLElement) {
			if company == "" {
				company = strings.TrimSpace(span.Text)
			}
		})

		e.ForEach("div.companyLocation, div[data-testid='text-location']", func(_ int, div *colly.HTMLElement) {
			if location == "" {
				location = strings.TrimSpace(div.Text)
			}
		})

		e.ForEach("div.salary-snippet, div.metadata.salary-snippet-container", func(_ int, div *colly.HTMLElement) {
			if salary == "" {
				salary = strings.TrimSpace(div.Text)
			}
		})

		if title != "" && jobURL != "" {
			job := &database.Job{
				Source:   "indeed",
				URL:      jobURL,
				Title:    title,
				Company:  company,
				Location: location,
				Salary:   salary,
				Status:   "discovered",
			}
			if jobKey != "" {
				job.ExternalID = "indeed-" + jobKey
			} else {
				job.ExternalID = extractJobID(job.URL)
			}
			job.JobType = database.DetectJobType(job.Title, job.Salary, job.URL)
			jobs = append(jobs, job)
			log.Printf("Found Indeed job: %s at %s", job.Title, job.Company)
		}
	})

	c.OnHTML("div.cardOutline", func(e *colly.HTMLElement) {
		var jobURL, title, company, location, salary string

		e.ForEach("a[data-jk]", func(_ int, link *colly.HTMLElement) {
			if jobURL == "" {
				jobURL = link.Request.AbsoluteURL(link.Attr("href"))
			}
		})
		e.ForEach("h2 span[title]", func(_ int, span *colly.HTMLElement) {
			if title == "" {
				title = strings.TrimSpace(span.Attr("title"))
			}
		})
		e.ForEach("span.companyName", func(_ int, span *colly.HTMLElement) {
			if company == "" {
				company = strings.TrimSpace(span.Text)
			}
		})
		e.ForEach("div.companyLocation", func(_ int, div *colly.HTMLElement) {
			if location == "" {
				location = strings.TrimSpace(div.Text)
			}
		})
		e.ForEach("div.salary-snippet", func(_ int, div *colly.HTMLElement) {
			if salary == "" {
				salary = strings.TrimSpace(div.Text)
			}
		})

		if title != "" && jobURL != "" {
			job := &database.Job{
				Source:   "indeed",
				URL:      jobURL,
				Title:    title,
				Company:  company,
				Location: location,
				Salary:   salary,
				Status:   "discovered",
			}
			job.ExternalID = extractJobID(job.URL)
			job.JobType = database.DetectJobType(job.Title, job.Salary, job.URL)
			jobs = append(jobs, job)
			log.Printf("Found Indeed job: %s at %s", job.Title, job.Company)
		}
	})

	if err := c.Visit(searchURL); err != nil {
		return nil, fmt.Errorf("failed to visit URL: %w", err)
	}
	c.Wait()

	return jobs, nil
}

// SaveJobs saves scraped jobs to the database, skipping existing ones.
func SaveJobs(jobs []*database.Job, userID uint) error {
	db := database.GetDB()

	for _, job := range jobs {
		job.UserID = userID

		var existing database.Job
		result := db.Where("external_id = ? AND user_id = ?", job.ExternalID, userID).First(&existing)
		if result.Error == nil {
			log.Printf("Job already exists: %s", job.Title)
			continue
		}
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			log.Printf("Database error checking job %s: %v", job.Title, result.Error)
			continue
		}

		if err := db.Create(job).Error; err != nil {
			log.Printf("Failed to save job %s: %v", job.Title, err)
			continue
		}
		log.Printf("Saved new job: %s", job.Title)
	}

	return nil
}

// extractJobID extracts a unique job ID from a job board URL.
func extractJobID(rawURL string) string {
	parts := strings.Split(rawURL, "/")

	if strings.Contains(rawURL, "indeed.com") {
		if idx := strings.Index(rawURL, "jk="); idx != -1 {
			jk := rawURL[idx+3:]
			if amp := strings.Index(jk, "&"); amp != -1 {
				jk = jk[:amp]
			}
			return "indeed-" + jk
		}
		return "indeed-" + rawURL
	}

	if strings.Contains(rawURL, "linkedin.com") {
		for i, part := range parts {
			if part == "view" && i+1 < len(parts) {
				id := strings.Split(parts[i+1], "?")[0]
				// Strip slug text, keep only the trailing numeric ID
				// e.g. "graduate-software-engineer-at-blackmagic-design-4381775795" → "4381775795"
				if dash := strings.LastIndex(id, "-"); dash != -1 {
					if numeric := id[dash+1:]; isNumeric(numeric) {
						return "linkedin-" + numeric
					}
				}
				return "linkedin-" + id
			}
		}
		return "linkedin-" + rawURL
	}

	if strings.Contains(rawURL, "seek.com.au") {
		for i, part := range parts {
			if part == "job" && i+1 < len(parts) {
				// Strip query parameters so the same job isn't stored
				// under two different IDs (e.g. type=promoted vs type=standard)
				jobID := strings.Split(parts[i+1], "?")[0]
				return "seek-" + jobID
			}
		}
		return "seek-" + rawURL
	}

	return rawURL
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
