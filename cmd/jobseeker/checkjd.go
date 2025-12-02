package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/guidebee/jobseeker/internal/jd"
	"github.com/guidebee/jobseeker/internal/resume"
	"github.com/spf13/cobra"
)

var (
	jdDir      string
	archiveDir string
	coverDir   string
)

var checkjdCmd = &cobra.Command{
	Use:   "checkjd",
	Short: "Analyze job descriptions from recruiters",
	Long: `Analyze job descriptions (in .docx format) from the jobdescriptions/ directory.
For each job description:
- Analyzes skill match and fit using your profile and resumes
- Optionally generates a tailored cover letter
- Moves processed files to archive

Example: jobseeker checkjd
Example: jobseeker checkjd --jd-dir custom_jds`,
	RunE: runCheckJD,
}

func init() {
	checkjdCmd.Flags().StringVar(&jdDir, "jd-dir", "jobdescriptions", "Directory containing job description .docx files")
	checkjdCmd.Flags().StringVar(&archiveDir, "archive-dir", "jobdescriptions/archive", "Directory to move processed JDs")
	checkjdCmd.Flags().StringVar(&coverDir, "cover-dir", "coverletters", "Directory to save generated cover letters")
}

func runCheckJD(cmd *cobra.Command, args []string) error {
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
		log.Printf("Warning: Could not load resumes: %v", err)
		log.Println("Will use config.yaml profile for analysis")
		resumes = []*resume.Resume{}
	} else {
		log.Printf("Loaded %d resume(s)", len(resumes))
	}

	// Load job descriptions
	jds, err := jd.LoadJobDescriptions(jdDir)
	if err != nil {
		return fmt.Errorf("failed to load job descriptions: %w", err)
	}

	if len(jds) == 0 {
		log.Printf("No job descriptions found in %s", jdDir)
		return nil
	}

	log.Printf("Found %d job description(s) to process\n", len(jds))

	// Create analyzer
	analyzer := jd.NewJDAnalyzer(apiKey, prof, resumes)

	// Create cover letters directory if needed
	if err := os.MkdirAll(coverDir, 0755); err != nil {
		return fmt.Errorf("failed to create cover letters directory: %w", err)
	}

	// Process each job description
	for i, jobDesc := range jds {
		fmt.Printf("\n%s\n", strings.Repeat("=", 80))
		fmt.Printf("Processing JD %d/%d: %s\n", i+1, len(jds), jobDesc.Filename)
		fmt.Printf("%s\n\n", strings.Repeat("=", 80))

		// Analyze the job description
		fmt.Println("Analyzing job description with Claude AI...")
		result, err := analyzer.AnalyzeJobDescription(jobDesc)
		if err != nil {
			log.Printf("Error analyzing %s: %v", jobDesc.Filename, err)
			continue
		}

		// Display analysis results
		displayAnalysisResult(result)

		// Ask if user wants to generate cover letter
		if !askYesNo("\nWould you like to generate a cover letter for this job?") {
			// Ask if user wants to archive without cover letter
			if askYesNo("Skip cover letter and archive this JD?") {
				if err := jobDesc.MoveToArchive(archiveDir); err != nil {
					log.Printf("Warning: failed to archive %s: %v", jobDesc.Filename, err)
				} else {
					fmt.Printf("✓ Archived %s\n", jobDesc.Filename)
				}
			} else {
				fmt.Println("Skipping this JD (will remain in directory)")
			}
			continue
		}

		// Ask for additional input for cover letter
		fmt.Println("\nYou can provide additional context for the cover letter (optional).")
		fmt.Println("For example: specific projects, achievements, or why you're interested.")
		fmt.Println("You can paste multiple lines of text.")

		userInput := readMultiLineInput("\nYour context (or press Enter to skip): ")

		// Create a reader for single-line inputs later
		reader := bufio.NewReader(os.Stdin)

		// Generate cover letter with iterative refinement
		var coverLetter string
		satisfied := false

		for !satisfied {
			// Generate or regenerate cover letter
			if coverLetter == "" {
				// Initial generation
				fmt.Println("\nGenerating cover letter...")
				var err error
				coverLetter, err = analyzer.GenerateCoverLetter(jobDesc, userInput)
				if err != nil {
					log.Printf("Error generating cover letter: %v", err)
					break
				}
			}

			// Display cover letter
			fmt.Printf("\n%s\n", strings.Repeat("-", 80))
			fmt.Println("GENERATED COVER LETTER:")
			fmt.Printf("%s\n", strings.Repeat("-", 80))
			fmt.Println(coverLetter)
			fmt.Printf("%s\n\n", strings.Repeat("-", 80))

			// Ask if user likes it
			fmt.Println("\nOptions:")
			fmt.Println("  1. Save this cover letter")
			fmt.Println("  2. Refine it (provide feedback for changes)")
			fmt.Println("  3. Discard and skip")
			fmt.Print("\nEnter your choice (1/2/3): ")

			choice, _ := reader.ReadString('\n')
			choice = strings.TrimSpace(choice)

			switch choice {
			case "1":
				// User is satisfied, save it
				satisfied = true

			case "2":
				// User wants to refine
				fmt.Println("\nWhat would you like to change?")
				fmt.Println("Examples: 'Make it more concise', 'Add more technical details',")
				fmt.Println("          'Emphasize leadership experience', 'More enthusiastic tone'")
				fmt.Println("You can paste multiple lines of detailed feedback.")

				feedback := readMultiLineInput("\nYour feedback: ")

				if feedback == "" {
					fmt.Println("No feedback provided, keeping current version.")
					continue
				}

				// Refine the cover letter
				fmt.Println("\nRefining cover letter based on your feedback...")
				refinedLetter, err := analyzer.RefineCoverLetter(jobDesc, coverLetter, feedback)
				if err != nil {
					log.Printf("Error refining cover letter: %v", err)
					fmt.Println("Keeping previous version.")
					continue
				}
				coverLetter = refinedLetter

			case "3":
				// User wants to discard
				fmt.Println("Cover letter discarded")
				coverLetter = "" // Mark as discarded
				satisfied = true // Exit loop

			default:
				fmt.Println("Invalid choice. Please enter 1, 2, or 3.")
			}
		}

		// If cover letter was discarded, skip to next JD
		if coverLetter == "" {
			continue
		}

		// Save cover letter
		coverFilename := strings.TrimSuffix(jobDesc.Filename, filepath.Ext(jobDesc.Filename)) + "_coverletter.txt"
		coverPath := filepath.Join(coverDir, coverFilename)

		if err := os.WriteFile(coverPath, []byte(coverLetter), 0644); err != nil {
			log.Printf("Error saving cover letter: %v", err)
			continue
		}

		fmt.Printf("✓ Cover letter saved to: %s\n", coverPath)

		// Archive the job description
		if err := jobDesc.MoveToArchive(archiveDir); err != nil {
			log.Printf("Warning: failed to archive %s: %v", jobDesc.Filename, err)
		} else {
			fmt.Printf("✓ Archived %s\n", jobDesc.Filename)
		}
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 80))
	fmt.Println("Processing complete!")
	fmt.Printf("%s\n", strings.Repeat("=", 80))

	return nil
}

// displayAnalysisResult prints the analysis results in a formatted way
func displayAnalysisResult(result *jd.AnalysisResult) {
	fmt.Printf("MATCH SCORE: %d/100\n\n", result.MatchScore)

	if result.ResumeUsed != "" {
		fmt.Printf("Resume Used: %s\n\n", result.ResumeUsed)
	}

	fmt.Printf("OVERALL ASSESSMENT:\n%s\n\n", result.Reasoning)

	if len(result.Pros) > 0 {
		fmt.Println("PROS:")
		for _, pro := range result.Pros {
			fmt.Printf("  ✓ %s\n", pro)
		}
		fmt.Println()
	}

	if len(result.Cons) > 0 {
		fmt.Println("CONS:")
		for _, con := range result.Cons {
			fmt.Printf("  ✗ %s\n", con)
		}
		fmt.Println()
	}

	if len(result.KeySkillsMatched) > 0 {
		fmt.Println("KEY SKILLS MATCHED:")
		for _, skill := range result.KeySkillsMatched {
			fmt.Printf("  • %s\n", skill)
		}
		fmt.Println()
	}

	if len(result.MissingSkills) > 0 {
		fmt.Println("SKILLS TO DEVELOP:")
		for _, skill := range result.MissingSkills {
			fmt.Printf("  • %s\n", skill)
		}
		fmt.Println()
	}
}

// readMultiLineInput reads multi-line input from stdin
// User can paste multiple lines and press Ctrl+D (Unix) or Ctrl+Z (Windows) to finish
// Or type END on a line by itself
func readMultiLineInput(prompt string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(prompt)
	fmt.Println("(Type END on a new line when done, or paste text and press Ctrl+D/Ctrl+Z)")
	fmt.Print("> ")

	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF reached (Ctrl+D or Ctrl+Z)
			if err.Error() == "EOF" {
				break
			}
			// Other error, just break
			break
		}

		line = strings.TrimRight(line, "\r\n")

		// Check for END terminator
		if strings.TrimSpace(line) == "END" {
			break
		}

		lines = append(lines, line)
	}

	result := strings.Join(lines, "\n")
	return strings.TrimSpace(result)
}

// askYesNo prompts user for yes/no response
func askYesNo(question string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s (y/n): ", question)
		response, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("Error reading input: %v", err)
			return false
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response == "y" || response == "yes" {
			return true
		}
		if response == "n" || response == "no" {
			return false
		}

		fmt.Println("Please enter 'y' or 'n'")
	}
}
