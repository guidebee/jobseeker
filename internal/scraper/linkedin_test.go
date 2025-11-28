package scraper

import (
	"testing"
)

// TestScrapeLinkedIn tests the LinkedIn scraper
// This is a live test that actually scrapes LinkedIn
// Run with: go test ./internal/scraper -run TestScrapeLinkedIn -v
func TestScrapeLinkedIn(t *testing.T) {
	// Create scraper with 3 second delay
	s := NewScraper(3000)

	// Test URL - LinkedIn job search for software engineers in Melbourne
	testURL := "https://www.linkedin.com/jobs/search/?keywords=software+engineer&location=Melbourne"

	t.Logf("Testing LinkedIn scraper with URL: %s", testURL)

	// Scrape the page
	jobs, err := s.ScrapeLinkedIn(testURL)
	if err != nil {
		t.Fatalf("ScrapeLinkedIn failed: %v", err)
	}

	// Log results
	t.Logf("Found %d jobs", len(jobs))

	if len(jobs) == 0 {
		t.Log("Warning: No jobs found. This could mean:")
		t.Log("  1. LinkedIn's HTML structure has changed")
		t.Log("  2. LinkedIn is blocking the scraper")
		t.Log("  3. No jobs match the search criteria")
		t.Log("  4. JavaScript rendering required (Colly only gets initial HTML)")
	}

	// Print details of first few jobs
	for i, job := range jobs {
		if i >= 5 {
			break // Only show first 5
		}
		t.Logf("\nJob %d:", i+1)
		t.Logf("  Title: %s", job.Title)
		t.Logf("  Company: %s", job.Company)
		t.Logf("  Location: %s", job.Location)
		t.Logf("  Salary: %s", job.Salary)
		t.Logf("  URL: %s", job.URL)
		t.Logf("  ExternalID: %s", job.ExternalID)
		t.Logf("  JobType: %s", job.JobType)
		t.Logf("  Source: %s", job.Source)
	}

	// Basic validation on first job if any found
	if len(jobs) > 0 {
		firstJob := jobs[0]

		if firstJob.Title == "" {
			t.Error("First job has empty title")
		}

		if firstJob.URL == "" {
			t.Error("First job has empty URL")
		}

		if firstJob.Source != "linkedin" {
			t.Errorf("Expected source 'linkedin', got '%s'", firstJob.Source)
		}

		if firstJob.ExternalID == "" {
			t.Error("First job has empty ExternalID")
		}

		t.Logf("\n✓ Validation passed for first job")
	}
}
