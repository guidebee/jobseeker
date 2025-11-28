package scraper

import (
	"fmt"
	"testing"

	"github.com/gocolly/colly/v2"
)

// TestSeekHTMLStructure inspects the actual HTML structure from SEEK
// Run with: go test -v ./internal/scraper -run TestSeekHTMLStructure
func TestSeekHTMLStructure(t *testing.T) {
	// Use a real SEEK search URL
	// Change this to your actual search URL from config.yaml
	searchURL := "https://www.seek.com.au/jobs?keywords=software+engineer&location=melbourne"

	fmt.Println("\n=== SEEK HTML Structure Inspector ===")
	fmt.Printf("Fetching: %s\n\n", searchURL)

	c := colly.NewCollector(
		colly.AllowedDomains("seek.com.au", "www.seek.com.au"),
	)

	// Track if we visited the page
	visited := false

	// Print the page title to confirm we loaded it
	c.OnHTML("title", func(e *colly.HTMLElement) {
		fmt.Printf("Page Title: %s\n\n", e.Text)
	})

	// Look for common job listing patterns
	c.OnHTML("article", func(e *colly.HTMLElement) {
		if !visited {
			fmt.Println("Found <article> elements:")
			visited = true
		}

		// Print article attributes
		fmt.Printf("\n--- Article ---\n")
		fmt.Printf("Tag: %s\n", e.Name)

		// Print all attributes
		for _, attr := range e.DOM.Nodes[0].Attr {
			fmt.Printf("  Attribute: %s=\"%s\"\n", attr.Key, attr.Val)
		}

		// Look for links
		e.ForEach("a", func(i int, link *colly.HTMLElement) {
			href := link.Attr("href")
			text := link.Text
			if href != "" {
				fmt.Printf("  Link: %s -> %s\n", text, href)
			}

			// Print link attributes
			for _, attr := range link.DOM.Nodes[0].Attr {
				if attr.Key == "data-testid" || attr.Key == "class" {
					fmt.Printf("    %s=\"%s\"\n", attr.Key, attr.Val)
				}
			}
		})

		// Look for spans
		e.ForEach("span", func(i int, span *colly.HTMLElement) {
			text := span.Text
			if text != "" && len(text) < 100 {
				for _, attr := range span.DOM.Nodes[0].Attr {
					if attr.Key == "data-testid" || attr.Key == "class" {
						fmt.Printf("  Span [%s=\"%s\"]: %s\n", attr.Key, attr.Val, text)
					}
				}
			}
		})
	})

	// Also check for divs with job-related classes
	c.OnHTML("div[data-testid], div[class*='job'], div[class*='Job']", func(e *colly.HTMLElement) {
		fmt.Printf("\n--- Div with job-related attributes ---\n")
		for _, attr := range e.DOM.Nodes[0].Attr {
			fmt.Printf("  %s=\"%s\"\n", attr.Key, attr.Val)
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		fmt.Printf("ERROR: %v\n", err)
		t.Fatalf("Failed to fetch page: %v", err)
	})

	c.OnRequest(func(r *colly.Request) {
		fmt.Printf("Visiting: %s\n", r.URL)
	})

	err := c.Visit(searchURL)
	if err != nil {
		t.Fatalf("Failed to visit URL: %v", err)
	}

	if !visited {
		fmt.Println("\nWARNING: No <article> elements found!")
		fmt.Println("SEEK might be using different HTML structure.")
		fmt.Println("Try inspecting the page source manually.")
	}
}

// TestSeekSelectorDebug tests specific CSS selectors
// Run with: go test -v ./internal/scraper -run TestSeekSelectorDebug
func TestSeekSelectorDebug(t *testing.T) {
	searchURL := "https://www.seek.com.au/jobs?keywords=software+engineer&location=melbourne"

	fmt.Println("\n=== Testing Different Selectors ===")

	c := colly.NewCollector(
		colly.AllowedDomains("seek.com.au", "www.seek.com.au"),
	)

	// Test various selector patterns
	selectors := []string{
		"article[data-testid='job-card']",
		"article[data-card-type='JobCard']",
		"article",
		"div[data-testid='job-card']",
		"div[class*='JobCard']",
		"a[data-testid='job-link']",
		"a[href*='/job/']",
	}

	for _, selector := range selectors {
		count := 0
		c.OnHTML(selector, func(e *colly.HTMLElement) {
			if count == 0 {
				fmt.Printf("\n✓ Selector '%s' found matches:\n", selector)
			}
			count++
			if count <= 2 { // Show first 2 matches
				fmt.Printf("  Match %d:\n", count)

				// Try to extract text content
				text := e.Text
				if len(text) > 100 {
					text = text[:100] + "..."
				}
				fmt.Printf("    Text: %s\n", text)

				// Show attributes
				for _, attr := range e.DOM.Nodes[0].Attr {
					fmt.Printf("    %s=\"%s\"\n", attr.Key, attr.Val)
				}
			}
		})

		err := c.Visit(searchURL)
		if err != nil {
			t.Fatalf("Failed to visit URL: %v", err)
		}

		if count == 0 {
			fmt.Printf("✗ Selector '%s' found 0 matches\n", selector)
		} else {
			fmt.Printf("  Total matches: %d\n", count)
		}

		// Reset collector for next selector
		c = colly.NewCollector(
			colly.AllowedDomains("seek.com.au", "www.seek.com.au"),
		)
	}
}

// TestSeekFullPageDump saves the full HTML to inspect
// Run with: go test -v ./internal/scraper -run TestSeekFullPageDump
func TestSeekFullPageDump(t *testing.T) {
	searchURL := "https://www.seek.com.au/jobs?keywords=software+engineer&location=melbourne"

	fmt.Println("\n=== Full Page HTML Dump ===")

	c := colly.NewCollector(
		colly.AllowedDomains("seek.com.au", "www.seek.com.au"),
	)

	c.OnResponse(func(r *colly.Response) {
		html := string(r.Body)
		fmt.Printf("Page size: %d bytes\n", len(html))

		// Save to file for inspection
		// Uncomment if you want to save the HTML
		// os.WriteFile("seek_page.html", r.Body, 0644)

		// Show first 2000 characters
		if len(html) > 2000 {
			fmt.Printf("\nFirst 2000 characters:\n%s\n", html[:2000])
		} else {
			fmt.Printf("\nFull HTML:\n%s\n", html)
		}
	})

	err := c.Visit(searchURL)
	if err != nil {
		t.Fatalf("Failed to visit URL: %v", err)
	}
}

// TestSeekScraperWithDebug tests the actual scraper with debug output
// Run with: go test -v ./internal/scraper -run TestSeekScraperWithDebug
func TestSeekScraperWithDebug(t *testing.T) {
	searchURL := "https://www.seek.com.au/jobs?keywords=software+engineer&location=melbourne"

	fmt.Println("\n=== Testing Current Scraper Implementation ===")

	s := NewScraper(1000) // 1 second delay

	// Add debug callbacks before scraping
	s.collector.OnHTML("article[data-testid='job-card']", func(e *colly.HTMLElement) {
		fmt.Println("\n--- Debug: Found job card ---")

		// Test each selector individually
		title := e.ChildText("a[data-testid='job-title']")
		fmt.Printf("Title selector result: '%s'\n", title)

		company := e.ChildText("a[data-testid='company-name']")
		fmt.Printf("Company selector result: '%s'\n", company)

		location := e.ChildText("span[data-testid='location']")
		fmt.Printf("Location selector result: '%s'\n", location)

		salary := e.ChildText("span[data-testid='salary']")
		fmt.Printf("Salary selector result: '%s'\n", salary)

		url := e.Request.AbsoluteURL(e.ChildAttr("a", "href"))
		fmt.Printf("URL: '%s'\n", url)

		// Show all links in this article
		fmt.Println("\nAll links in article:")
		e.ForEach("a", func(i int, link *colly.HTMLElement) {
			fmt.Printf("  Link %d: text='%s', href='%s'\n", i, link.Text, link.Attr("href"))
		})

		// Show all spans
		fmt.Println("\nAll spans in article:")
		e.ForEach("span", func(i int, span *colly.HTMLElement) {
			text := span.Text
			if text != "" && len(text) < 100 {
				fmt.Printf("  Span %d: '%s'\n", i, text)
			}
		})
	})

	jobs, err := s.ScrapeSeek(searchURL)
	if err != nil {
		t.Fatalf("Scraper failed: %v", err)
	}

	fmt.Printf("\n=== Results ===\n")
	fmt.Printf("Total jobs found: %d\n", len(jobs))

	for i, job := range jobs {
		fmt.Printf("\nJob %d:\n", i+1)
		fmt.Printf("  Title: %s\n", job.Title)
		fmt.Printf("  Company: %s\n", job.Company)
		fmt.Printf("  Location: %s\n", job.Location)
		fmt.Printf("  Salary: %s\n", job.Salary)
		fmt.Printf("  URL: %s\n", job.URL)
		fmt.Printf("  ExternalID: %s\n", job.ExternalID)
	}

	if len(jobs) == 0 {
		t.Log("WARNING: No jobs found. The CSS selectors might be incorrect.")
		t.Log("Run TestSeekHTMLStructure to inspect the actual HTML structure.")
	}
}
