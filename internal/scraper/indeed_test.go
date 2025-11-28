package scraper

import (
	"testing"
)

// TestScrapeIndeed tests the Indeed scraper
// This is a live test that actually scrapes Indeed
// Run with: go test ./internal/scraper -run TestScrapeIndeed -v
func TestScrapeIndeed(t *testing.T) {
	// Create scraper with 3 second delay
	s := NewScraper(3000)

	// Test URL - Indeed job search for software engineers in Melbourne
	testURL := "https://au.indeed.com/jobs?q=software+engineer&l=Melbourne"

	t.Logf("Testing Indeed scraper with URL: %s", testURL)

	// Scrape the page
	jobs, err := s.ScrapeIndeed(testURL)
	if err != nil {
		t.Fatalf("ScrapeIndeed failed: %v", err)
	}

	// Log results
	t.Logf("Found %d jobs", len(jobs))

	if len(jobs) == 0 {
		t.Log("Warning: No jobs found. This could mean:")
		t.Log("  1. Indeed's HTML structure has changed")
		t.Log("  2. Indeed is blocking the scraper (403 error)")
		t.Log("  3. No jobs match the search criteria")
		t.Log("  4. The headers (Referer/Origin) need adjustment")
		t.Log("\nCheck the scraper logs above for error details (especially status codes)")
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

		if firstJob.Source != "indeed" {
			t.Errorf("Expected source 'indeed', got '%s'", firstJob.Source)
		}

		if firstJob.ExternalID == "" {
			t.Error("First job has empty ExternalID")
		}

		// Check that external ID follows the pattern
		if len(firstJob.ExternalID) > 7 && firstJob.ExternalID[:7] != "indeed-" {
			t.Errorf("External ID should start with 'indeed-', got: %s", firstJob.ExternalID)
		}

		t.Logf("\n✓ Validation passed for first job")
	}
}
