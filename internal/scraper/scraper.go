package scraper

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/guidebee/jobseeker/internal/database"
	"gorm.io/gorm"
)

// Scraper handles job board scraping
type Scraper struct {
	collector *colly.Collector
	delay     time.Duration
}

// NewScraper creates a new scraper instance
func NewScraper(delayMs int) *Scraper {
	// Create a new collector with politeness settings
	c := colly.NewCollector(
		colly.AllowedDomains("seek.com.au", "www.seek.com.au", "linkedin.com", "www.linkedin.com", "au.linkedin.com", "indeed.com", "www.indeed.com", "au.indeed.com"),
		colly.Async(true), // Enable async mode for concurrent scraping
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	// Set rate limiting to be polite to the website
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*seek.com.au*",
		Delay:       time.Duration(delayMs) * time.Millisecond,
		RandomDelay: 1 * time.Second,
		Parallelism: 2,
	})

	// Add rate limiting for LinkedIn
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*linkedin.com*",
		Delay:       time.Duration(delayMs) * time.Millisecond,
		RandomDelay: 2 * time.Second, // LinkedIn is more strict, use longer delay
		Parallelism: 1,                // Reduce parallelism for LinkedIn
	})

	// Add rate limiting for Indeed
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*indeed.com*",
		Delay:       time.Duration(delayMs) * time.Millisecond,
		RandomDelay: 2 * time.Second, // Indeed is strict about scraping
		Parallelism: 1,
	})

	// Set proper headers for each request to appear more like a real browser
	c.OnRequest(func(r *colly.Request) {
		// Log the visit
		log.Printf("Visiting: %s", r.URL)

		// Set Referer and Origin based on the domain
		url := r.URL.String()

		if strings.Contains(url, "indeed.com") {
			r.Headers.Set("Referer", "https://au.indeed.com/")
			r.Headers.Set("Origin", "https://au.indeed.com")
			r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
			r.Headers.Set("Accept-Language", "en-US,en;q=0.9")
		} else if strings.Contains(url, "linkedin.com") {
			r.Headers.Set("Referer", "https://www.linkedin.com/")
			r.Headers.Set("Origin", "https://www.linkedin.com")
			r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
			r.Headers.Set("Accept-Language", "en-US,en;q=0.9")
		} else if strings.Contains(url, "seek.com.au") {
			r.Headers.Set("Referer", "https://www.seek.com.au/")
			r.Headers.Set("Origin", "https://www.seek.com.au")
			r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
			r.Headers.Set("Accept-Language", "en-AU,en;q=0.9")
		}
	})

	// Handle errors globally
	c.OnError(func(r *colly.Response, err error) {
		log.Printf("Error scraping %s: %v (Status: %d)", r.Request.URL, err, r.StatusCode)
	})

	return &Scraper{
		collector: c,
		delay:     time.Duration(delayMs) * time.Millisecond,
	}
}

// ScrapeSeek scrapes job listings from SEEK
// This is a simplified version - you'll need to inspect SEEK's HTML structure
func (s *Scraper) ScrapeSeek(searchURL string) ([]*database.Job, error) {
	var jobs []*database.Job

	// Set up HTML callbacks
	// This will be called for each job card found
	s.collector.OnHTML("article[data-testid='job-card']", func(e *colly.HTMLElement) {
		// Extract job details using correct selectors
		// Title: from link with data-testid
		title := ""
		e.ForEach("a[data-testid='job-card-title']", func(_ int, el *colly.HTMLElement) {
			if title == "" {
				title = strings.TrimSpace(el.Text)
			}
		})

		// Company: Look for links ending with -jobs or advertiser IDs
		company := ""
		e.ForEach("a", func(_ int, link *colly.HTMLElement) {
			href := link.Attr("href")
			text := strings.TrimSpace(link.Text)
			// Company links end with -jobs or have advertiserid parameter
			if text != "" && (strings.HasSuffix(href, "-jobs") || strings.Contains(href, "advertiserid=")) {
				if company == "" {
					company = text
				}
			}
		})

		// Location: Look for location links (contain /jobs/in-)
		location := ""
		e.ForEach("a", func(_ int, link *colly.HTMLElement) {
			href := link.Attr("href")
			text := strings.TrimSpace(link.Text)
			// Location links contain /jobs/in- or /software-engineer-jobs/in-
			if text != "" && strings.Contains(href, "/in-") && !strings.Contains(href, "All") {
				if location == "" {
					location = text
				}
			}
		})

		// Salary: Look for dollar amounts in spans
		salary := ""
		e.ForEach("span", func(_ int, span *colly.HTMLElement) {
			text := strings.TrimSpace(span.Text)
			// Salary text contains dollar signs
			if strings.Contains(text, "$") && len(text) < 100 {
				if salary == "" {
					salary = text
				}
			}
		})

		// Get job URL from the title link
		jobURL := ""
		e.ForEach("a[data-testid='job-card-title']", func(_ int, el *colly.HTMLElement) {
			if jobURL == "" {
				jobURL = e.Request.AbsoluteURL(el.Attr("href"))
			}
		})

		job := &database.Job{
			Source:    "seek",
			URL:       jobURL,
			Title:     title,
			Company:   company,
			Location:  location,
			Salary:    salary,
			Status:    "discovered",
		}

		// Generate a unique external ID from the URL
		job.ExternalID = extractJobID(job.URL)

		// Detect if contract or permanent role
		job.JobType = database.DetectJobType(job.Title, job.Salary, job.URL)

		// Only add if we have at least a title and URL
		if job.Title != "" && job.URL != "" {
			jobs = append(jobs, job)
			log.Printf("Found job: %s at %s", job.Title, job.Company)
		}
	})

	// Start scraping
	err := s.collector.Visit(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to visit URL: %w", err)
	}

	// Wait for async requests to complete
	s.collector.Wait()

	return jobs, nil
}

// ScrapeLinkedIn scrapes job listings from LinkedIn
// Note: LinkedIn heavily uses JavaScript, so this scraper has limitations
// It works best with LinkedIn's public job search pages
func (s *Scraper) ScrapeLinkedIn(searchURL string) ([]*database.Job, error) {
	var jobs []*database.Job

	// Try multiple selector strategies for LinkedIn's various layouts
	// Strategy 1: Job card divs with base-card class
	s.collector.OnHTML("div.base-card", func(e *colly.HTMLElement) {
		var jobURL string
		var title string
		var company string
		var location string

		// Find job link
		e.ForEach("a.base-card__full-link", func(_ int, link *colly.HTMLElement) {
			if jobURL == "" {
				href := link.Attr("href")
				if strings.Contains(href, "/jobs/view/") || strings.Contains(href, "/jobs-guest/jobs/api/") {
					jobURL = link.Request.AbsoluteURL(href)
				}
			}
		})

		// Extract title
		e.ForEach("h3.base-search-card__title", func(_ int, h3 *colly.HTMLElement) {
			if title == "" {
				title = strings.TrimSpace(h3.Text)
			}
		})

		// Extract company
		e.ForEach("h4.base-search-card__subtitle, a.hidden-nested-link", func(_ int, el *colly.HTMLElement) {
			if company == "" {
				company = strings.TrimSpace(el.Text)
			}
		})

		// Extract location
		e.ForEach("span.job-search-card__location", func(_ int, span *colly.HTMLElement) {
			if location == "" {
				location = strings.TrimSpace(span.Text)
			}
		})

		if title != "" && jobURL != "" {
			job := &database.Job{
				Source:    "linkedin",
				URL:       jobURL,
				Title:     title,
				Company:   company,
				Location:  location,
				Status:    "discovered",
			}
			job.ExternalID = extractJobID(job.URL)
			job.JobType = database.DetectJobType(job.Title, job.Salary, job.URL)
			jobs = append(jobs, job)
			log.Printf("Found LinkedIn job: %s at %s", job.Title, job.Company)
		}
	})

	// Strategy 2: List items (backup for different LinkedIn layouts)
	s.collector.OnHTML("li.jobs-search__results-list", func(e *colly.HTMLElement) {
		// This is a container - process child elements
		e.ForEach("div.job-search-card", func(_ int, card *colly.HTMLElement) {
			var jobURL string
			var title string
			var company string
			var location string

			// Find the job link
			card.ForEach("a[href*='/jobs/view/'], a[href*='/jobs-guest/']", func(_ int, link *colly.HTMLElement) {
				if jobURL == "" {
					jobURL = link.Request.AbsoluteURL(link.Attr("href"))
				}
			})

			// Extract title from h3
			card.ForEach("h3", func(_ int, h3 *colly.HTMLElement) {
				if title == "" {
					title = strings.TrimSpace(h3.Text)
				}
			})

			// Extract company from h4
			card.ForEach("h4", func(_ int, h4 *colly.HTMLElement) {
				if company == "" {
					company = strings.TrimSpace(h4.Text)
				}
			})

			// Extract location
			card.ForEach("span.job-search-card__location", func(_ int, span *colly.HTMLElement) {
				if location == "" {
					location = strings.TrimSpace(span.Text)
				}
			})

			if title != "" && jobURL != "" {
				job := &database.Job{
					Source:    "linkedin",
					URL:       jobURL,
					Title:     title,
					Company:   company,
					Location:  location,
					Status:    "discovered",
				}
				job.ExternalID = extractJobID(job.URL)
				job.JobType = database.DetectJobType(job.Title, job.Salary, job.URL)
				jobs = append(jobs, job)
				log.Printf("Found LinkedIn job: %s at %s", job.Title, job.Company)
			}
		})
	})

	// Start scraping
	err := s.collector.Visit(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to visit URL: %w", err)
	}

	// Wait for async requests to complete
	s.collector.Wait()

	return jobs, nil
}

// ScrapeIndeed scrapes job listings from Indeed
// Indeed uses specific class names for job cards and details
func (s *Scraper) ScrapeIndeed(searchURL string) ([]*database.Job, error) {
	var jobs []*database.Job

	// Indeed uses multiple possible selectors depending on the page layout
	// Strategy 1: Main job card container with mosaic provider
	s.collector.OnHTML("div.job_seen_beacon, div.slider_container, td.resultContent", func(e *colly.HTMLElement) {
		var jobURL string
		var title string
		var company string
		var location string
		var salary string
		var jobKey string

		// Extract job title and URL
		// Indeed uses <h2> with class "jobTitle" containing an <a> tag
		e.ForEach("h2.jobTitle a, a.jcs-JobTitle", func(_ int, link *colly.HTMLElement) {
			if title == "" {
				title = strings.TrimSpace(link.Text)
				href := link.Attr("href")
				if href != "" {
					jobURL = link.Request.AbsoluteURL(href)
				}
				// Extract job key from data attribute
				jobKey = link.Attr("data-jk")
				if jobKey == "" {
					jobKey = link.Attr("id")
				}
			}
		})

		// Fallback: Try different title selectors
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

		// Extract company name
		e.ForEach("span.companyName, span[data-testid='company-name']", func(_ int, span *colly.HTMLElement) {
			if company == "" {
				company = strings.TrimSpace(span.Text)
			}
		})

		// Extract location
		e.ForEach("div.companyLocation, div[data-testid='text-location']", func(_ int, div *colly.HTMLElement) {
			if location == "" {
				location = strings.TrimSpace(div.Text)
			}
		})

		// Extract salary if available
		e.ForEach("div.salary-snippet, div.metadata.salary-snippet-container", func(_ int, div *colly.HTMLElement) {
			if salary == "" {
				salary = strings.TrimSpace(div.Text)
			}
		})

		// Create job if we have essential fields
		if title != "" && jobURL != "" {
			job := &database.Job{
				Source:    "indeed",
				URL:       jobURL,
				Title:     title,
				Company:   company,
				Location:  location,
				Salary:    salary,
				Status:    "discovered",
			}

			// Generate external ID from job key or URL
			if jobKey != "" {
				job.ExternalID = "indeed-" + jobKey
			} else {
				job.ExternalID = extractJobID(job.URL)
			}

			// Detect job type
			job.JobType = database.DetectJobType(job.Title, job.Salary, job.URL)

			jobs = append(jobs, job)
			log.Printf("Found Indeed job: %s at %s", job.Title, job.Company)
		}
	})

	// Strategy 2: Alternative Indeed layout (cardOutline)
	s.collector.OnHTML("div.cardOutline", func(e *colly.HTMLElement) {
		var jobURL string
		var title string
		var company string
		var location string
		var salary string

		// Extract title and URL
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

		// Extract company
		e.ForEach("span.companyName", func(_ int, span *colly.HTMLElement) {
			if company == "" {
				company = strings.TrimSpace(span.Text)
			}
		})

		// Extract location
		e.ForEach("div.companyLocation", func(_ int, div *colly.HTMLElement) {
			if location == "" {
				location = strings.TrimSpace(div.Text)
			}
		})

		// Extract salary
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

	// Start scraping
	err := s.collector.Visit(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to visit URL: %w", err)
	}

	// Wait for async requests to complete
	s.collector.Wait()

	return jobs, nil
}

// SaveJobs saves scraped jobs to the database
// It skips jobs that already exist (based on ExternalID + UserID)
func SaveJobs(jobs []*database.Job, userID uint) error {
	db := database.GetDB()

	for _, job := range jobs {
		// Set user ID on job
		job.UserID = userID

		// Check if job already exists for this user
		var existing database.Job
		result := db.Where("external_id = ? AND user_id = ?", job.ExternalID, userID).First(&existing)

		if result.Error == nil {
			// Job exists, skip
			log.Printf("Job already exists: %s", job.Title)
			continue
		}

		// Check if it's a real error or just "not found"
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			// Real database error
			log.Printf("Database error checking job %s: %v", job.Title, result.Error)
			continue
		}

		// Job doesn't exist, save it
		err := db.Create(job).Error
		if err != nil {
			log.Printf("Failed to save job %s: %v", job.Title, err)
			continue
		}

		log.Printf("Saved new job: %s", job.Title)
	}

	return nil
}

// extractJobID extracts the job ID from a job board URL
// Example: https://www.seek.com.au/job/12345678 -> seek-12345678
// Example: https://www.linkedin.com/jobs/view/12345678 -> linkedin-12345678
// Example: https://au.indeed.com/viewjob?jk=abc123 -> indeed-abc123
func extractJobID(url string) string {
	parts := strings.Split(url, "/")

	// Indeed pattern: /viewjob?jk={id} or /rc/clk?jk={id}
	if strings.Contains(url, "indeed.com") {
		// Try to extract from query parameter jk=
		if strings.Contains(url, "jk=") {
			jkIndex := strings.Index(url, "jk=")
			if jkIndex != -1 {
				jkValue := url[jkIndex+3:]
				// Remove any additional query parameters
				if ampIndex := strings.Index(jkValue, "&"); ampIndex != -1 {
					jkValue = jkValue[:ampIndex]
				}
				return "indeed-" + jkValue
			}
		}
		// Fallback for Indeed
		return "indeed-" + url
	}

	// LinkedIn pattern: /jobs/view/{id}
	if strings.Contains(url, "linkedin.com") {
		for i, part := range parts {
			if part == "view" && i+1 < len(parts) {
				// Extract just the numeric ID, removing query parameters
				id := strings.Split(parts[i+1], "?")[0]
				return "linkedin-" + id
			}
		}
		return "linkedin-" + url
	}

	// SEEK pattern: /job/{id}
	if strings.Contains(url, "seek.com.au") {
		for i, part := range parts {
			if part == "job" && i+1 < len(parts) {
				return "seek-" + parts[i+1]
			}
		}
		return "seek-" + url
	}

	// Fallback: use the full URL as ID
	return url
}
