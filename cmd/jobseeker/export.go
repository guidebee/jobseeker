package main

import (
	"fmt"
	"log"
	"os"

	"github.com/guidebee/jobseeker/internal/database"
	"github.com/guidebee/jobseeker/internal/export"
	"github.com/spf13/cobra"
)

var (
	exportOutput          string
	exportRecommended     bool
	exportDiscovered      bool
	exportRejected        bool
	exportApplied         bool
	exportAllStatuses     bool
	exportMinScore        int
	exportMaxResults      int
	exportJobType         string
	exportSource          string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export jobs to Excel spreadsheet",
	Long: `Export scanned jobs with AI analysis to a professional Excel spreadsheet.

This command uses Claude's xlsx skill to create a formatted Excel file with:
- Jobs Summary sheet with all job details
- Detailed Analysis sheet with match scores, pros/cons
- Statistics dashboard with charts and summaries

The Excel file includes professional formatting, color-coding by match score,
clickable URLs, and auto-filtering on all columns.

Example: jobseeker export
Example: jobseeker export --recommended --output my_jobs.xlsx
Example: jobseeker export --min-score 70 --max-results 50
Example: jobseeker export --all-statuses --job-type contract`,
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output filename (default: jobs_export_YYYYMMDD_HHMMSS.xlsx)")
	exportCmd.Flags().BoolVar(&exportRecommended, "recommended", false, "Export only recommended jobs")
	exportCmd.Flags().BoolVar(&exportDiscovered, "discovered", false, "Include discovered (unanalyzed) jobs")
	exportCmd.Flags().BoolVar(&exportRejected, "rejected", false, "Include rejected jobs")
	exportCmd.Flags().BoolVar(&exportApplied, "applied", false, "Include applied jobs")
	exportCmd.Flags().BoolVar(&exportAllStatuses, "all-statuses", false, "Include all job statuses (overrides other status flags)")
	exportCmd.Flags().IntVar(&exportMinScore, "min-score", 0, "Minimum match score (0-100)")
	exportCmd.Flags().IntVar(&exportMaxResults, "max-results", 0, "Maximum number of jobs to export (0 = unlimited)")
	exportCmd.Flags().StringVar(&exportJobType, "job-type", "", "Filter by job type: contract, permanent")
	exportCmd.Flags().StringVar(&exportSource, "source", "", "Filter by source: seek, linkedin, indeed")
}

func runExport(cmd *cobra.Command, args []string) error {
	// Initialize app
	prof, err := initApp()
	if err != nil {
		return err
	}

	// Get Claude API key
	apiKey := os.Getenv("CLAUDE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("CLAUDE_API_KEY environment variable not set")
	}

	// Get user from database
	db := database.DB
	var user database.User
	err = db.Where("email = ?", prof.Profile.Email).First(&user).Error
	if err != nil {
		return fmt.Errorf("user not found. Please run 'jobseeker init' first")
	}

	// Build query for jobs
	query := db.Where("user_id = ?", user.ID)

	// Apply filters
	if exportJobType != "" {
		query = query.Where("job_type = ?", exportJobType)
	}

	if exportSource != "" {
		query = query.Where("source = ?", exportSource)
	}

	// Get jobs
	var jobs []database.Job
	err = query.Order("created_at DESC").Find(&jobs).Error
	if err != nil {
		return fmt.Errorf("failed to fetch jobs: %w", err)
	}

	if len(jobs) == 0 {
		log.Println("No jobs found in database")
		log.Println("Run 'jobseeker scan' to discover jobs first")
		return nil
	}

	log.Printf("Found %d jobs in database", len(jobs))

	// Configure export options
	options := export.ExportOptions{
		MinMatchScore: exportMinScore,
		MaxResults:    exportMaxResults,
	}

	// Set status filters
	if exportAllStatuses {
		options.IncludeDiscovered = true
		options.IncludeRecommended = true
		options.IncludeRejected = true
		options.IncludeApplied = true
	} else {
		// If no status flags specified, default to recommended
		if !exportRecommended && !exportDiscovered && !exportRejected && !exportApplied {
			exportRecommended = true
		}

		options.IncludeRecommended = exportRecommended
		options.IncludeDiscovered = exportDiscovered
		options.IncludeRejected = exportRejected
		options.IncludeApplied = exportApplied
	}

	// Generate output filename if not specified
	outputPath := exportOutput
	if outputPath == "" {
		outputPath = export.GenerateFilename("jobs_export")
	}

	// Create exporter
	exporter := export.NewJobExporter(apiKey)

	// Export to Excel
	fmt.Println("Generating Excel spreadsheet with Claude Skills...")
	fmt.Println("This may take 30-90 seconds depending on the number of jobs...")
	fmt.Println()

	err = exporter.ExportToExcel(jobs, outputPath, options)
	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	// Success!
	fmt.Println("✓ SUCCESS! Jobs exported to Excel")
	fmt.Printf("  File: %s\n", outputPath)

	// Show file info
	if info, err := os.Stat(outputPath); err == nil {
		fmt.Printf("  Size: %.1f KB\n", float64(info.Size())/1024)
	}

	fmt.Println()
	fmt.Println("The Excel file contains:")
	fmt.Println("  • Sheet 1: Jobs Summary - All job details")
	fmt.Println("  • Sheet 2: Detailed Analysis - Match scores, pros/cons")
	fmt.Println("  • Sheet 3: Statistics - Dashboard with charts")
	fmt.Println()
	fmt.Println("Tip: Open in Microsoft Excel or Google Sheets to explore the data!")

	return nil
}
