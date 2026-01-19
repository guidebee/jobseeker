package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/guidebee/jobseeker/internal/cvtailor"
	"github.com/guidebee/jobseeker/internal/jd"
	"github.com/guidebee/jobseeker/internal/resume"
	"github.com/spf13/cobra"
)

var (
	tailorJDDir     string
	tailorOutputDir string
	batchMode       bool
)

var tailorcvCmd = &cobra.Command{
	Use:   "tailorcv",
	Short: "Generate tailored CVs based on job descriptions",
	Long: `Generate professionally tailored CVs (resumes) in Word format based on job descriptions.

This command uses Claude's document coauthoring skills to create customized CVs that:
- Emphasize relevant experience and skills for each job
- Incorporate job-specific keywords naturally
- Reorganize content to highlight the most relevant qualifications
- Maintain professional formatting in .docx format

The command analyzes job descriptions from the jobdescriptions/ directory, and for each one:
1. Analyzes the job requirements against your profile and resumes
2. Creates a tailored CV emphasizing relevant skills and experience
3. Saves the CV to the tailored_cvs/ directory with a descriptive name

Example: jobseeker tailorcv
Example: jobseeker tailorcv --jd-dir recruiters --output tailored --batch`,
	RunE: runTailorCV,
}

func init() {
	tailorcvCmd.Flags().StringVar(&tailorJDDir, "jd-dir", "jobdescriptions", "Directory containing job description .docx files")
	tailorcvCmd.Flags().StringVar(&tailorOutputDir, "output", "tailored_cvs", "Directory to save tailored CVs")
	tailorcvCmd.Flags().BoolVar(&batchMode, "batch", false, "Batch mode: process all JDs without prompts")
}

func runTailorCV(cmd *cobra.Command, args []string) error {
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

	// Load resumes
	resumesDir := "resumes"
	resumes, err := resume.LoadResumes(resumesDir)
	if err != nil {
		return fmt.Errorf("failed to load resumes: %w (ensure you have .docx resumes in %s directory)", err, resumesDir)
	}

	if len(resumes) == 0 {
		return fmt.Errorf("no resumes found in %s directory - at least one resume is required for CV tailoring", resumesDir)
	}

	log.Printf("Loaded %d resume(s)", len(resumes))

	// Load job descriptions
	jds, err := jd.LoadJobDescriptions(tailorJDDir)
	if err != nil {
		return fmt.Errorf("failed to load job descriptions: %w", err)
	}

	if len(jds) == 0 {
		log.Printf("No job descriptions found in %s", tailorJDDir)
		return nil
	}

	log.Printf("Found %d job description(s) to process\n", len(jds))

	// Create output directory
	if err := os.MkdirAll(tailorOutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create analyzer and tailor
	analyzer := jd.NewJDAnalyzer(apiKey, prof, resumes)
	tailor := cvtailor.NewCVTailor(apiKey, prof, resumes)

	// Process each job description
	successCount := 0
	for i, jobDesc := range jds {
		fmt.Printf("\n%s\n", strings.Repeat("=", 80))
		fmt.Printf("Processing JD %d/%d: %s\n", i+1, len(jds), jobDesc.Filename)
		fmt.Printf("%s\n\n", strings.Repeat("=", 80))

		// Analyze the job description first
		fmt.Println("Analyzing job description with Claude AI...")
		result, err := analyzer.AnalyzeJobDescription(jobDesc)
		if err != nil {
			log.Printf("Error analyzing %s: %v", jobDesc.Filename, err)
			continue
		}

		// Display analysis results
		displayAnalysisResult(result)

		// In non-batch mode, ask user if they want to proceed
		if !batchMode {
			if !askYesNo("\nWould you like to generate a tailored CV for this job?") {
				fmt.Println("Skipping CV generation for this job")
				continue
			}
		} else {
			// In batch mode, skip low-scoring matches
			if result.MatchScore < 60 {
				fmt.Printf("Skipping (match score %d/100 is below threshold)\n", result.MatchScore)
				continue
			}
		}

		// Generate tailored CV
		fmt.Println("\nGenerating tailored CV using Claude Skills...")
		fmt.Println("This may take 30-60 seconds as Claude creates a professionally formatted Word document...")

		outputPath, err := tailor.TailorCV(jobDesc, result, tailorOutputDir)
		if err != nil {
			log.Printf("Error generating tailored CV: %v", err)

			// Provide helpful error messages
			if strings.Contains(err.Error(), "file ID") {
				log.Println("Note: Claude Skills API may require additional permissions or setup.")
				log.Println("Ensure your API key has access to Skills API with docx skill enabled.")
			}
			continue
		}

		// Success!
		successCount++
		fmt.Printf("\n✓ SUCCESS! Tailored CV saved to: %s\n", outputPath)

		// Show file info
		if info, err := os.Stat(outputPath); err == nil {
			fmt.Printf("  File size: %.1f KB\n", float64(info.Size())/1024)
		}

		// In non-batch mode, ask if user wants to preview
		if !batchMode {
			fmt.Println("\nTip: Open the CV in Microsoft Word to review and make any final adjustments.")

			if askYesNo("Continue to next job description?") {
				continue
			} else {
				break
			}
		}
	}

	// Summary
	fmt.Printf("\n%s\n", strings.Repeat("=", 80))
	fmt.Printf("Processing complete! Generated %d tailored CV(s) out of %d job(s)\n", successCount, len(jds))
	fmt.Printf("Tailored CVs saved in: %s\n", tailorOutputDir)
	fmt.Printf("%s\n", strings.Repeat("=", 80))

	return nil
}
