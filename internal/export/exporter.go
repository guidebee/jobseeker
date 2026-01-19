package export

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/guidebee/jobseeker/internal/database"
	"github.com/guidebee/jobseeker/pkg/claude"
)

// JobExporter handles exporting job data to Excel
type JobExporter struct {
	skillsClient *claude.SkillsClient
}

// NewJobExporter creates a new job exporter
func NewJobExporter(apiKey string) *JobExporter {
	return &JobExporter{
		skillsClient: claude.NewSkillsClient(apiKey),
	}
}

// ExportOptions configures what data to export
type ExportOptions struct {
	IncludeDiscovered  bool
	IncludeRecommended bool
	IncludeRejected    bool
	IncludeApplied     bool
	MinMatchScore      int
	MaxResults         int
}

// ExportToExcel exports jobs to an Excel file using Claude's xlsx skill
func (e *JobExporter) ExportToExcel(jobs []database.Job, outputPath string, options ExportOptions) error {
	// Filter jobs based on options
	filteredJobs := e.filterJobs(jobs, options)

	if len(filteredJobs) == 0 {
		return fmt.Errorf("no jobs match the export criteria")
	}

	// Build the prompt for Excel generation
	prompt := e.buildExcelPrompt(filteredJobs)

	// Send request to Claude with xlsx skill
	response, err := e.skillsClient.SendMessageWithXlsx(prompt)
	if err != nil {
		return fmt.Errorf("failed to generate Excel file: %w", err)
	}

	// Debug: Print response structure
	fmt.Printf("\n[DEBUG] Response received, content blocks: %d\n", len(response.Content))
	for i, content := range response.Content {
		fmt.Printf("[DEBUG] Block %d - Type: %s\n", i, content.Type)
		if content.Name != "" {
			fmt.Printf("[DEBUG] Block %d - Name: %s\n", i, content.Name)
		}
		if content.Text != "" {
			fmt.Printf("[DEBUG] Block %d - Text (first 200 chars): %s\n", i, truncateText(content.Text, 200))
		}
	}

	// Extract file ID from response
	fileID, err := e.extractFileIDFromResponse(response)
	if err != nil {
		return fmt.Errorf("failed to extract file ID from response: %w", err)
	}

	// Download the generated Excel file
	err = e.skillsClient.DownloadFile(fileID, outputPath)
	if err != nil {
		return fmt.Errorf("failed to download Excel file: %w", err)
	}

	return nil
}

// filterJobs filters jobs based on export options
func (e *JobExporter) filterJobs(jobs []database.Job, options ExportOptions) []database.Job {
	var filtered []database.Job

	for _, job := range jobs {
		// Filter by status
		statusMatch := false
		switch job.Status {
		case "discovered":
			statusMatch = options.IncludeDiscovered
		case "recommended":
			statusMatch = options.IncludeRecommended
		case "rejected":
			statusMatch = options.IncludeRejected
		case "applied", "approved":
			statusMatch = options.IncludeApplied
		default:
			statusMatch = true // Include unknown statuses
		}

		if !statusMatch {
			continue
		}

		// Filter by match score
		if job.MatchScore < options.MinMatchScore {
			continue
		}

		filtered = append(filtered, job)

		// Limit results if specified
		if options.MaxResults > 0 && len(filtered) >= options.MaxResults {
			break
		}
	}

	return filtered
}

// buildExcelPrompt creates the prompt for Claude to generate an Excel file
func (e *JobExporter) buildExcelPrompt(jobs []database.Job) string {
	var prompt strings.Builder

	prompt.WriteString("Create a professional Excel (.xlsx) file with 3 sheets:\n\n")

	prompt.WriteString("SHEET 1 - Jobs Summary (columns: ID, Title, Company, Location, Salary, Type, Source, Score, Status, Date, URL)\n")
	prompt.WriteString("SHEET 2 - Analysis (columns: ID, Title, Company, Score, Reasoning, Pros, Cons, Resume, Date)\n")
	prompt.WriteString("SHEET 3 - Statistics (Total jobs, by status, by source, by type, avg score, top companies, top locations)\n\n")

	prompt.WriteString("Formatting: Bold headers, auto-fit columns, filters, freeze top row, color-code scores (Red<50, Yellow 50-69, Green≥70), clickable URLs\n\n")

	prompt.WriteString(fmt.Sprintf("DATA (%d jobs):\n", len(jobs)))

	// Limit the number of jobs to avoid timeout - batch processing if needed
	maxJobs := len(jobs)
	if maxJobs > 100 {
		maxJobs = 100 // Limit to 100 jobs per request
	}

	// Use compact CSV-like format
	for i := 0; i < maxJobs; i++ {
		job := jobs[i]

		// Parse pros/cons
		var pros, cons []string
		json.Unmarshal([]byte(job.AnalysisPros), &pros)
		json.Unmarshal([]byte(job.AnalysisCons), &cons)

		prosStr := strings.Join(pros, "; ")
		consStr := strings.Join(cons, "; ")

		analyzedDate := ""
		if job.AnalyzedAt != nil {
			analyzedDate = job.AnalyzedAt.Format("02/01/2006")
		}

		// Compact format
		prompt.WriteString(fmt.Sprintf("%d|%s|%s|%s|%s|%s|%s|%d|%s|%s|%s|%s|%s|%s|%s|%s\n",
			job.ID,
			job.Title,
			job.Company,
			job.Location,
			job.Salary,
			job.JobType,
			job.Source,
			job.MatchScore,
			job.Status,
			job.CreatedAt.Format("02/01/2006"),
			job.URL,
			truncateText(job.AnalysisReasoning, 200),
			prosStr,
			consStr,
			job.ResumeUsed,
			analyzedDate,
		))
	}

	if len(jobs) > maxJobs {
		prompt.WriteString(fmt.Sprintf("\n[Note: Showing first %d of %d jobs due to size limits]\n", maxJobs, len(jobs)))
	}

	prompt.WriteString("\nCreate the Excel file now with proper formatting.\n")

	return prompt.String()
}

// truncateText truncates text to specified length with ellipsis
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// extractFileIDFromResponse extracts the file ID from Claude's response
func (e *JobExporter) extractFileIDFromResponse(response *claude.SkillsResponse) (string, error) {
	// Look through content blocks for file references
	for i, content := range response.Content {
		fmt.Printf("[DEBUG] Block %d - Checking type: %s\n", i, content.Type)

		// Check text_editor_code_execution_tool_result (Skills API specific)
		if content.Type == "text_editor_code_execution_tool_result" {
			fmt.Printf("[DEBUG] Found text_editor_code_execution_tool_result\n")

			// Check output field for file references
			if content.Output != nil {
				fmt.Printf("[DEBUG] Output field exists\n")
				// Look for files array in output
				if files, ok := content.Output["files"].([]interface{}); ok {
					fmt.Printf("[DEBUG] Found files array with %d items\n", len(files))
					if len(files) > 0 {
						if fileMap, ok := files[0].(map[string]interface{}); ok {
							if fileID, ok := fileMap["file_id"].(string); ok {
								fmt.Printf("[DEBUG] Found file_id in files array: %s\n", fileID)
								return fileID, nil
							}
							if path, ok := fileMap["path"].(string); ok {
								fmt.Printf("[DEBUG] Found path in files array: %s\n", path)
								return path, nil
							}
						}
					}
				}

				// Check for direct file_id in output
				if fileID, ok := content.Output["file_id"].(string); ok {
					fmt.Printf("[DEBUG] Found file_id in output: %s\n", fileID)
					return fileID, nil
				}
			}

			// Check content field
			if content.Content != nil {
				fmt.Printf("[DEBUG] Content field exists, type: %T\n", content.Content)

				// Try as map
				if contentMap, ok := content.Content.(map[string]interface{}); ok {
					fmt.Printf("[DEBUG] Content is a map with keys: ")
					for key := range contentMap {
						fmt.Printf("%s ", key)
					}
					fmt.Printf("\n")

					// Look for common file-related keys
					if output, ok := contentMap["output"].(string); ok {
						fmt.Printf("[DEBUG] Found output in content map: %s\n", truncateText(output, 500))
					}
					if files, ok := contentMap["files"].([]interface{}); ok {
						fmt.Printf("[DEBUG] Found files array in content map with %d items\n", len(files))
					}
				}

				// Try as string
				if contentStr, ok := content.Content.(string); ok {
					fmt.Printf("[DEBUG] Content as string: %s\n", truncateText(contentStr, 500))
				}
			}

			// Check text field
			if content.Text != "" {
				fmt.Printf("[DEBUG] tool_result text: %s\n", truncateText(content.Text, 500))
			}
		}

		// Check bash_code_execution_tool_result blocks
		if content.Type == "bash_code_execution_tool_result" {
			fmt.Printf("[DEBUG] Found bash_code_execution_tool_result\n")

			// Check output field
			if content.Output != nil {
				fmt.Printf("[DEBUG] Output field exists in bash result\n")
				if output, ok := content.Output["stdout"].(string); ok {
					fmt.Printf("[DEBUG] stdout: %s\n", truncateText(output, 300))
				}
				if files, ok := content.Output["files"].([]interface{}); ok {
					fmt.Printf("[DEBUG] Found files in bash output: %d items\n", len(files))
				}
			}

			// Check content field
			if content.Content != nil {
				if contentMap, ok := content.Content.(map[string]interface{}); ok {
					fmt.Printf("[DEBUG] bash result content keys: ")
					for key := range contentMap {
						fmt.Printf("%s ", key)
					}
					fmt.Printf("\n")
				}
			}

			// Check text field
			if content.Text != "" {
				fmt.Printf("[DEBUG] bash result text: %s\n", truncateText(content.Text, 300))
			}
		}

		// Check server_tool_use blocks
		if content.Type == "server_tool_use" {
			fmt.Printf("[DEBUG] Found server_tool_use with name: %s\n", content.Name)
			if content.Input != nil {
				if code, ok := content.Input["code"].(string); ok {
					fmt.Printf("[DEBUG] Code execution input (first 300 chars): %s\n", truncateText(code, 300))
				}
				if command, ok := content.Input["command"].(string); ok {
					fmt.Printf("[DEBUG] Bash command: %s\n", command)
				}
			}
		}

		// Check for document/file source references (standard API)
		if content.Source != nil {
			fmt.Printf("[DEBUG] Found source field\n")
			if fileID, ok := content.Source["file_id"].(string); ok {
				fmt.Printf("[DEBUG] Found file_id in source: %s\n", fileID)
				return fileID, nil
			}
		}

		// Check text content for file references
		if content.Type == "text" && content.Text != "" {
			patterns := []string{"file_", "file-", "artifact:", "/tmp/", "output.xlsx", ".xlsx"}
			for _, pattern := range patterns {
				if strings.Contains(strings.ToLower(content.Text), strings.ToLower(pattern)) {
					fmt.Printf("[DEBUG] Found pattern '%s' in text\n", pattern)
				}
			}
		}
	}

	fmt.Printf("[DEBUG] No file ID found in standard locations\n")
	fmt.Printf("[DEBUG] Stop reason: %s\n", response.StopReason)

	return "", fmt.Errorf("no file ID found in response - Skills API may not have generated a downloadable file")
}

// GenerateFilename generates an appropriate filename for the export
func GenerateFilename(prefix string) string {
	timestamp := time.Now().Format("20060102_150405")
	return fmt.Sprintf("%s_%s.xlsx", prefix, timestamp)
}
