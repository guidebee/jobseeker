package main

import (
	"fmt"
	"log"

	"github.com/guidebee/jobseeker/internal/database"
	"github.com/spf13/cobra"
)

var (
	statusFilter     string
	jobTypeFilter    string
	showRecommended  bool
	showContractOnly bool
	limit            int
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs from database",
	Long:  `Display jobs filtered by status, score, or other criteria.`,
	Run:   runList,
}

func runList(cmd *cobra.Command, args []string) {
	// Initialize app
	_, err := initApp()
	if err != nil {
		log.Fatalf("Initialization failed: %v", err)
	}

	// Get current user
	user, err := database.GetCurrentUser()
	if err != nil {
		log.Fatalf("Failed to get current user: %v\nRun 'jobseeker init' first", err)
	}

	db := database.GetDB()
	var jobs []database.Job

	// Build query with user filter
	query := db.Where("user_id = ?", user.ID).Order("created_at DESC")

	if showRecommended {
		query = query.Where("status = ?", "recommended")
	} else if statusFilter != "" {
		query = query.Where("status = ?", statusFilter)
	}

	// Filter by job type
	if showContractOnly {
		query = query.Where("job_type = ?", "contract")
	} else if jobTypeFilter != "" {
		query = query.Where("job_type = ?", jobTypeFilter)
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	// Execute query
	result := query.Find(&jobs)
	if result.Error != nil {
		log.Fatalf("Failed to fetch jobs: %v", result.Error)
	}

	// Display results
	if len(jobs) == 0 {
		fmt.Println("No jobs found")
		return
	}

	fmt.Printf("Found %d jobs:\n\n", len(jobs))

	for i, job := range jobs {
		fmt.Printf("%d. %s\n", i+1, job.Title)
		fmt.Printf("   Company: %s | Location: %s | Type: %s\n", job.Company, job.Location, job.JobType)
		if job.Salary != "" {
			fmt.Printf("   Rate/Salary: %s\n", job.Salary)
		}
		fmt.Printf("   Status: %s", job.Status)
		if job.IsAnalyzed {
			fmt.Printf(" | Match Score: %d/100", job.MatchScore)
		}
		fmt.Printf("\n   URL: %s\n", job.URL)

		if job.IsAnalyzed && job.Analysis != "" {
			fmt.Printf("   Analysis:\n")
			// Print first 200 chars of analysis
			analysis := job.Analysis
			if len(analysis) > 200 {
				analysis = analysis[:200] + "..."
			}
			fmt.Printf("   %s\n", analysis)
		}
		fmt.Println()
	}

	fmt.Printf("Total: %d jobs\n", len(jobs))
}

func init() {
	// Add flags for filtering
	listCmd.Flags().StringVarP(&statusFilter, "status", "s", "", "Filter by status (discovered, recommended, applied, rejected)")
	listCmd.Flags().StringVarP(&jobTypeFilter, "type", "t", "", "Filter by job type (contract, permanent, unknown)")
	listCmd.Flags().BoolVarP(&showRecommended, "recommended", "r", false, "Show only recommended jobs")
	listCmd.Flags().BoolVar(&showContractOnly, "contract", false, "Show only contract roles")
	listCmd.Flags().IntVarP(&limit, "limit", "l", 10, "Maximum number of jobs to show")
}
