package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/guidebee/jobseeker/internal/database"
	"github.com/guidebee/jobseeker/internal/profile"
	"github.com/guidebee/jobseeker/internal/resume"
	"github.com/guidebee/jobseeker/internal/scraper"
	"github.com/guidebee/jobseeker/pkg/claude"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan job boards for new opportunities",
	Long:  `Scrapes configured job boards (SEEK, LinkedIn, etc.) and saves jobs to the database.`,
	Run:   runScan,
}

func runScan(cmd *cobra.Command, args []string) {
	// Initialize app (database and profile)
	prof, err := initApp()
	if err != nil {
		log.Fatalf("Initialization failed: %v", err)
	}

	// Get current user
	user, err := database.GetCurrentUser()
	if err != nil {
		log.Fatalf("Failed to get current user: %v\nRun 'jobseeker init' first", err)
	}
	fmt.Printf("Scanning for user: %s (%s)\n", user.Name, user.Email)

	// Get scraper settings from environment
	delayMs, _ := strconv.Atoi(getEnv("SCRAPER_DELAY_MS", "2000"))

	// Create scraper
	s := scraper.NewScraper(delayMs)

	fmt.Println("Starting job scan...")

	// Scan each enabled job board
	totalJobs := 0

	// SEEK
	if prof.JobBoards["seek"].Enabled {
		fmt.Println("\nScanning SEEK...")

		// Get static URLs from config
		staticURLs := prof.JobBoards["seek"].SearchURLs

		// Try to generate dynamic URLs from resume
		dynamicURLs := generateDynamicURLs(prof)

		// Merge URLs
		allURLs := resume.MergeSearchURLs(dynamicURLs, staticURLs)

		if len(allURLs) == 0 {
			log.Println("No search URLs configured or generated")
		} else {
			fmt.Printf("Total search URLs: %d", len(allURLs))
			if len(staticURLs) > 0 {
				fmt.Printf(" (%d from config", len(staticURLs))
			}
			if len(dynamicURLs) > 0 {
				fmt.Printf(" + %d from resume", len(dynamicURLs))
			}
			fmt.Println(")")

			for i, seekURL := range allURLs {
				fmt.Printf("\n[%d/%d] Scanning: %s\n", i+1, len(allURLs), seekURL)
				jobs, err := s.ScrapeSeek(seekURL)
				if err != nil {
					log.Printf("Error scraping SEEK: %v", err)
					continue
				}

				fmt.Printf("  Found %d jobs\n", len(jobs))
				err = scraper.SaveJobs(jobs, user.ID)
				if err != nil {
					log.Printf("  Error saving jobs: %v", err)
				}
				totalJobs += len(jobs)
			}
		}
	}

	// LinkedIn
	if prof.JobBoards["linkedin"].Enabled {
		fmt.Println("\nScanning LinkedIn...")

		linkedinURLs := prof.JobBoards["linkedin"].SearchURLs

		if len(linkedinURLs) == 0 {
			log.Println("No LinkedIn search URLs configured")
		} else {
			fmt.Printf("Configured %d search URLs\n", len(linkedinURLs))

			for i, linkedinURL := range linkedinURLs {
				fmt.Printf("\n[%d/%d] Scanning: %s\n", i+1, len(linkedinURLs), linkedinURL)
				jobs, err := s.ScrapeLinkedIn(linkedinURL)
				if err != nil {
					log.Printf("Error scraping LinkedIn: %v", err)
					continue
				}

				fmt.Printf("  Found %d jobs\n", len(jobs))
				err = scraper.SaveJobs(jobs, user.ID)
				if err != nil {
					log.Printf("  Error saving jobs: %v", err)
				}
				totalJobs += len(jobs)
			}
		}
	}

	// Indeed
	if prof.JobBoards["indeed"].Enabled {
		fmt.Println("\nScanning Indeed...")

		indeedURLs := prof.JobBoards["indeed"].SearchURLs

		if len(indeedURLs) == 0 {
			log.Println("No Indeed search URLs configured")
		} else {
			fmt.Printf("Configured %d search URLs\n", len(indeedURLs))

			for i, indeedURL := range indeedURLs {
				fmt.Printf("\n[%d/%d] Scanning: %s\n", i+1, len(indeedURLs), indeedURL)
				jobs, err := s.ScrapeIndeed(indeedURL)
				if err != nil {
					log.Printf("Error scraping Indeed: %v", err)
					continue
				}

				fmt.Printf("  Found %d jobs\n", len(jobs))
				err = scraper.SaveJobs(jobs, user.ID)
				if err != nil {
					log.Printf("  Error saving jobs: %v", err)
				}
				totalJobs += len(jobs)
			}
		}
	}

	fmt.Printf("\n✓ Scan complete! Found %d total jobs\n", totalJobs)
	fmt.Println("Run 'jobseeker analyze' to evaluate new jobs with AI")
}

// generateDynamicURLs creates search URLs from resume content
func generateDynamicURLs(prof *profile.Profile) []string {
	// Try to load resumes
	resumesDir := "./resumes"
	resumes, err := resume.LoadResumes(resumesDir)
	if err != nil || len(resumes) == 0 {
		// No resumes found, return empty
		return []string{}
	}

	fmt.Printf("✓ Found %d resume(s), generating search keywords...\n", len(resumes))

	// Get Claude API key
	apiKey := os.Getenv("CLAUDE_API_KEY")
	if apiKey == "" {
		log.Println("Warning: CLAUDE_API_KEY not set, skipping dynamic URL generation")
		return []string{}
	}

	// Create Claude client
	claudeClient := claude.NewClient(apiKey)

	// Use first resume for keyword extraction
	// TODO: Could merge keywords from all resumes
	selectedResume := resumes[0]
	fmt.Printf("  Analyzing: %s\n", selectedResume.Filename)

	keywords, err := resume.ExtractKeywords(selectedResume, claudeClient)
	if err != nil {
		log.Printf("Warning: Failed to extract keywords: %v", err)
		return []string{}
	}

	fmt.Printf("  Extracted keywords:\n")
	fmt.Printf("    Primary skills: %v\n", keywords.PrimarySkills)
	fmt.Printf("    Roles: %v\n", keywords.Roles)
	fmt.Printf("    Search terms: %d generated\n", len(keywords.SearchKeywords))

	// Get preferred location from config
	location := "melbourne" // Default
	if len(prof.Profile.Preferences.Locations) > 0 {
		location = prof.Profile.Preferences.Locations[0]
	}

	// Generate URLs
	urls := resume.GenerateSearchURLs(keywords, location)

	return urls
}

func init() {
	// Add scan-specific flags if needed
	// scanCmd.Flags().StringP("board", "b", "all", "Job board to scan (seek, linkedin, all)")
}
