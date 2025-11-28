package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/guidebee/jobseeker/internal/analyzer"
	"github.com/guidebee/jobseeker/internal/database"
	"github.com/spf13/cobra"
)

var (
	analyzeContractOnly bool
	analyzeJobType      string
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze jobs using Claude AI",
	Long:  `Uses Claude AI to analyze unanalyzed jobs and provide match scores and recommendations.`,
	Run:   runAnalyze,
}

func runAnalyze(cmd *cobra.Command, args []string) {
	// Initialize app
	prof, err := initApp()
	if err != nil {
		log.Fatalf("Initialization failed: %v", err)
	}

	// Get current user
	user, err := database.GetCurrentUser()
	if err != nil {
		log.Fatalf("Failed to get current user: %v\nRun 'jobseeker init' first", err)
	}
	fmt.Printf("Analyzing jobs for: %s (%s)\n", user.Name, user.Email)

	// Get Claude API key
	apiKey := os.Getenv("CLAUDE_API_KEY")
	if apiKey == "" {
		log.Fatal("CLAUDE_API_KEY not set in environment")
	}

	// Get match threshold
	threshold, _ := strconv.Atoi(getEnv("MATCH_THRESHOLD", "70"))

	// Create analyzer
	a := analyzer.NewAnalyzer(apiKey, prof)

	// Try to load resumes from resumes directory
	resumesDir := "./resumes"
	err = a.LoadResumes(resumesDir)
	if err != nil {
		fmt.Printf("No resumes found, using config.yaml profile (%v)\n", err)
	} else {
		fmt.Println("✓ Using resume(s) for analysis")
	}

	fmt.Println("Analyzing jobs with Claude AI...")

	// Get unanalyzed jobs from database
	db := database.GetDB()
	var jobs []database.Job

	// Build query with job type filter and user filter
	query := db.Where("user_id = ? AND is_analyzed = ?", user.ID, false)

	if analyzeContractOnly {
		query = query.Where("job_type = ?", "contract")
		fmt.Println("Filtering for contract roles only...")
	} else if analyzeJobType != "" {
		query = query.Where("job_type = ?", analyzeJobType)
		fmt.Printf("Filtering for %s roles only...\n", analyzeJobType)
	}

	result := query.Find(&jobs)
	if result.Error != nil {
		log.Fatalf("Failed to fetch jobs: %v", result.Error)
	}

	if len(jobs) == 0 {
		fmt.Println("No new jobs to analyze")
		return
	}

	fmt.Printf("Found %d jobs to analyze\n\n", len(jobs))

	recommended := 0
	for i, job := range jobs {
		fmt.Printf("[%d/%d] Analyzing: %s at %s (%s)\n", i+1, len(jobs), job.Title, job.Company, job.JobType)

		// Analyze with Claude
		analysis, err := a.AnalyzeJob(&job)
		if err != nil {
			log.Printf("  ✗ Error: %v", err)
			continue
		}

		// Update job with analysis results
		now := time.Now()
		job.MatchScore = analysis.MatchScore

		// Store full formatted analysis (for display)
		job.Analysis = fmt.Sprintf("Score: %d/100\n\nReasoning: %s\n\nPros:\n- %s\n\nCons:\n- %s",
			analysis.MatchScore,
			analysis.Reasoning,
			joinStrings(analysis.Pros, "\n- "),
			joinStrings(analysis.Cons, "\n- "),
		)

		// Store structured data (for querying)
		job.AnalysisReasoning = analysis.Reasoning
		prosJSON, _ := json.Marshal(analysis.Pros)
		job.AnalysisPros = string(prosJSON)
		consJSON, _ := json.Marshal(analysis.Cons)
		job.AnalysisCons = string(consJSON)

		// Record which resume was used (if any)
		if a.UseResumes() {
			job.ResumeUsed = a.GetResumeUsed(&job)
		}

		job.IsAnalyzed = true
		job.AnalyzedAt = &now

		// Set status based on threshold
		if analysis.MatchScore >= threshold {
			job.Status = "recommended"
			recommended++
			fmt.Printf("  ✓ Match: %d/100 - RECOMMENDED\n", analysis.MatchScore)
		} else {
			job.Status = "rejected"
			fmt.Printf("  ○ Match: %d/100 - Below threshold\n", analysis.MatchScore)
		}

		// Save to database
		db.Save(&job)
	}

	fmt.Printf("\n✓ Analysis complete!\n")
	fmt.Printf("  Recommended jobs: %d\n", recommended)
	fmt.Printf("  Below threshold: %d\n", len(jobs)-recommended)
	fmt.Println("\nRun 'jobseeker list --recommended' to see your matches")
}

// joinStrings joins a slice of strings with a separator
func joinStrings(items []string, sep string) string {
	if len(items) == 0 {
		return "None"
	}
	result := items[0]
	for i := 1; i < len(items); i++ {
		result += sep + items[i]
	}
	return result
}

func init() {
	// Add flags for filtering
	analyzeCmd.Flags().BoolVar(&analyzeContractOnly, "contract", false, "Analyze only contract roles")
	analyzeCmd.Flags().StringVarP(&analyzeJobType, "type", "t", "", "Analyze only specific job type (contract, permanent, unknown)")
}
