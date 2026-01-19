package cvtailor

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/guidebee/jobseeker/internal/jd"
	"github.com/guidebee/jobseeker/internal/profile"
	"github.com/guidebee/jobseeker/internal/resume"
	"github.com/guidebee/jobseeker/pkg/claude"
)

// CVTailor handles tailoring CVs based on job descriptions
type CVTailor struct {
	skillsClient *claude.SkillsClient
	profile      *profile.Profile
	resumes      []*resume.Resume
}

// NewCVTailor creates a new CV tailor
func NewCVTailor(apiKey string, profile *profile.Profile, resumes []*resume.Resume) *CVTailor {
	return &CVTailor{
		skillsClient: claude.NewSkillsClient(apiKey),
		profile:      profile,
		resumes:      resumes,
	}
}

// TailorCV creates a tailored CV based on job description and analysis
func (t *CVTailor) TailorCV(jobDesc *jd.JobDescription, analysis *jd.AnalysisResult, outputDir string) (string, error) {
	// Select the best resume to tailor
	selectedResume := t.selectBestResume(jobDesc, analysis)
	if selectedResume == nil {
		return "", fmt.Errorf("no suitable resume found")
	}

	// Build the prompt for CV tailoring
	prompt := t.buildTailoringPrompt(jobDesc, analysis, selectedResume)

	// Send request to Claude with docx skill
	response, err := t.skillsClient.SendMessageWithDocx(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to generate tailored CV: %w", err)
	}

	// Extract file ID from response
	fileID, err := t.extractFileIDFromResponse(response)
	if err != nil {
		return "", fmt.Errorf("failed to extract file ID from response: %w", err)
	}

	// Generate output filename
	outputFilename := t.generateOutputFilename(jobDesc, outputDir)

	// Download the generated CV
	err = t.skillsClient.DownloadFile(fileID, outputFilename)
	if err != nil {
		return "", fmt.Errorf("failed to download tailored CV: %w", err)
	}

	return outputFilename, nil
}

// selectBestResume selects the most appropriate resume based on job description
func (t *CVTailor) selectBestResume(jobDesc *jd.JobDescription, analysis *jd.AnalysisResult) *resume.Resume {
	// If analysis indicates which resume was used, prefer that one
	if analysis.ResumeUsed != "" {
		for _, r := range t.resumes {
			if r.Filename == analysis.ResumeUsed {
				return r
			}
		}
	}

	// Otherwise, use the resume selector logic
	if len(t.resumes) > 0 {
		// For now, use the first resume - could enhance with keyword matching
		return t.resumes[0]
	}

	return nil
}

// buildTailoringPrompt builds the prompt for Claude to tailor the CV
func (t *CVTailor) buildTailoringPrompt(jobDesc *jd.JobDescription, analysis *jd.AnalysisResult, selectedResume *resume.Resume) string {
	var prompt strings.Builder

	prompt.WriteString("You are an expert CV tailoring assistant. Your task is to create a tailored CV (resume) in Word format (.docx) based on the provided job description and candidate information.\n\n")

	prompt.WriteString("# CANDIDATE INFORMATION\n\n")
	prompt.WriteString(fmt.Sprintf("Name: %s\n", t.profile.Profile.Name))
	prompt.WriteString(fmt.Sprintf("Email: %s\n", t.profile.Profile.Email))
	if t.profile.Profile.Phone != "" {
		prompt.WriteString(fmt.Sprintf("Phone: %s\n", t.profile.Profile.Phone))
	}
	if t.profile.Profile.Location != "" {
		prompt.WriteString(fmt.Sprintf("Location: %s\n", t.profile.Profile.Location))
	}
	prompt.WriteString("\n")

	prompt.WriteString("# ORIGINAL RESUME CONTENT\n\n")
	// Limit resume content to avoid token limits
	resumeContent := selectedResume.Content
	if len(resumeContent) > 6000 {
		resumeContent = resumeContent[:6000] + "\n\n[Resume content truncated for length...]"
	}
	prompt.WriteString(resumeContent)
	prompt.WriteString("\n\n")

	prompt.WriteString("# JOB DESCRIPTION\n\n")
	prompt.WriteString(fmt.Sprintf("Source: %s\n\n", jobDesc.Filename))
	prompt.WriteString("Description:\n")
	// Limit JD content to avoid token limits
	jdContent := jobDesc.Content
	if len(jdContent) > 3000 {
		jdContent = jdContent[:3000] + "\n\n[Job description truncated for length...]"
	}
	prompt.WriteString(jdContent)
	prompt.WriteString("\n\n")

	prompt.WriteString("# ANALYSIS INSIGHTS\n\n")
	prompt.WriteString(fmt.Sprintf("Match Score: %d/100\n", analysis.MatchScore))
	prompt.WriteString(fmt.Sprintf("Overall Assessment: %s\n\n", analysis.Reasoning))

	if len(analysis.KeySkillsMatched) > 0 {
		prompt.WriteString("Key Skills to Emphasize:\n")
		for _, skill := range analysis.KeySkillsMatched {
			prompt.WriteString(fmt.Sprintf("- %s\n", skill))
		}
		prompt.WriteString("\n")
	}

	if len(analysis.MissingSkills) > 0 {
		prompt.WriteString("Skills to De-emphasize or Compensate For:\n")
		for _, skill := range analysis.MissingSkills {
			prompt.WriteString(fmt.Sprintf("- %s\n", skill))
		}
		prompt.WriteString("\n")
	}

	prompt.WriteString("# INSTRUCTIONS\n\n")
	prompt.WriteString("Create a tailored CV in Word format (.docx) that:\n\n")
	prompt.WriteString("1. **Emphasizes Relevant Experience**: Highlight work experience, projects, and achievements that align with the key skills matched.\n\n")
	prompt.WriteString("2. **Uses Job-Specific Keywords**: Incorporate keywords and phrases from the job description naturally throughout the CV, especially in:\n")
	prompt.WriteString("   - Professional summary/objective\n")
	prompt.WriteString("   - Skills section\n")
	prompt.WriteString("   - Work experience descriptions\n\n")
	prompt.WriteString("3. **Reorders/Reorganizes Content**: Prioritize the most relevant experiences and skills for this specific role.\n\n")
	prompt.WriteString("4. **Maintains Accuracy**: Do NOT fabricate experience, skills, or achievements. Only restructure and emphasize existing content.\n\n")
	prompt.WriteString("5. **Professional Formatting**: Use clean, professional formatting with:\n")
	prompt.WriteString("   - Clear section headings (Contact Information, Professional Summary, Skills, Experience, Education, etc.)\n")
	prompt.WriteString("   - Consistent fonts and spacing\n")
	prompt.WriteString("   - Bullet points for achievements and responsibilities\n")
	prompt.WriteString("   - Professional color scheme (optional subtle accent colors)\n\n")
	prompt.WriteString("6. **Addresses Gaps Tactfully**: If there are missing skills, consider:\n")
	prompt.WriteString("   - Highlighting transferable skills\n")
	prompt.WriteString("   - Emphasizing willingness to learn\n")
	prompt.WriteString("   - Showcasing related experience\n\n")
	prompt.WriteString("7. **Optimizes Length**: Keep the CV concise (ideally 2 pages) while including all relevant information.\n\n")
	prompt.WriteString("Please create the tailored CV now using the docx skill. Save it with an appropriate filename and return the file.\n")

	return prompt.String()
}

// extractFileIDFromResponse extracts the file ID from Claude's response
func (t *CVTailor) extractFileIDFromResponse(response *claude.SkillsResponse) (string, error) {
	// Look through content blocks for file references
	for _, content := range response.Content {
		// Check tool_use blocks for code execution with file output
		if content.Type == "tool_use" {
			// Check if this is a code execution that generated a file
			if content.Name == "code_execution" && content.Input != nil {
				// The file might be referenced in the input or we need to look at subsequent blocks
				continue
			}
		}

		// Check for document/file source references
		if content.Source != nil {
			if fileID, ok := content.Source["file_id"].(string); ok {
				return fileID, nil
			}
			if data, ok := content.Source["data"].(string); ok {
				// Sometimes file ID is in the data field
				return data, nil
			}
		}

		// Check text content for file references
		if content.Type == "text" && content.Text != "" {
			// Parse text for file ID patterns or paths
			// This is a fallback and may need adjustment based on actual API responses
			if strings.Contains(content.Text, "file_") {
				// Extract file ID from text if present
				// This would need more sophisticated parsing in production
				parts := strings.Split(content.Text, "file_")
				if len(parts) > 1 {
					// Simple extraction - enhance as needed
					fileIDPart := strings.Fields(parts[1])[0]
					return "file_" + fileIDPart, nil
				}
			}
		}
	}

	// If we get here, try the helper function
	return claude.ExtractFileID(response)
}

// generateOutputFilename generates an appropriate filename for the tailored CV
func (t *CVTailor) generateOutputFilename(jobDesc *jd.JobDescription, outputDir string) string {
	// Extract base filename from job description (without extension)
	baseName := strings.TrimSuffix(jobDesc.Filename, filepath.Ext(jobDesc.Filename))
	baseName = sanitizeFilename(baseName)

	// Truncate if too long
	if len(baseName) > 50 {
		baseName = baseName[:50]
	}

	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102")
	filename := fmt.Sprintf("CV_%s_%s.docx", baseName, timestamp)

	return filepath.Join(outputDir, filename)
}

// sanitizeFilename removes invalid characters from filename
func sanitizeFilename(s string) string {
	// Remove or replace invalid filename characters
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "",
		"?", "",
		"\"", "",
		"<", "",
		">", "",
		"|", "",
		"\n", " ",
		"\r", " ",
		"\t", " ",
	)

	sanitized := replacer.Replace(s)

	// Replace multiple spaces with single space
	for strings.Contains(sanitized, "  ") {
		sanitized = strings.ReplaceAll(sanitized, "  ", " ")
	}

	return strings.TrimSpace(sanitized)
}
