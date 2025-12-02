package jd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/guidebee/jobseeker/internal/profile"
	"github.com/guidebee/jobseeker/internal/resume"
	"github.com/guidebee/jobseeker/pkg/claude"
)

// JDAnalyzer handles job description analysis using Claude AI
type JDAnalyzer struct {
	claudeClient *claude.Client
	profile      *profile.Profile
	resumes      []*resume.Resume
}

// NewJDAnalyzer creates a new job description analyzer
func NewJDAnalyzer(apiKey string, prof *profile.Profile, resumes []*resume.Resume) *JDAnalyzer {
	return &JDAnalyzer{
		claudeClient: claude.NewClient(apiKey),
		profile:      prof,
		resumes:      resumes,
	}
}

// AnalysisResult represents Claude's analysis of a job description
type AnalysisResult struct {
	MatchScore       int      `json:"match_score"` // 0-100
	Reasoning        string   `json:"reasoning"`
	Pros             []string `json:"pros"`
	Cons             []string `json:"cons"`
	KeySkillsMatched []string `json:"key_skills_matched"`
	MissingSkills    []string `json:"missing_skills"`
	ResumeUsed       string   `json:"resume_used,omitempty"`
}

// AnalyzeJobDescription analyzes a job description against user profile and resumes
func (a *JDAnalyzer) AnalyzeJobDescription(jd *JobDescription) (*AnalysisResult, error) {
	// Build the prompt for Claude
	prompt := a.buildAnalysisPrompt(jd)

	// Send to Claude
	response, err := a.claudeClient.SendMessage(prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to get Claude response: %w", err)
	}

	// Parse Claude's response
	result, err := a.parseAnalysisResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse analysis: %w", err)
	}

	// Add resume used info
	if len(a.resumes) > 0 {
		selectedResume := a.selectBestResume(jd.Content)
		result.ResumeUsed = selectedResume.Filename
	}

	return result, nil
}

// buildAnalysisPrompt creates a detailed prompt for Claude
func (a *JDAnalyzer) buildAnalysisPrompt(jd *JobDescription) string {
	// Select best resume if available
	var resumeContent string
	if len(a.resumes) > 0 {
		selectedResume := a.selectBestResume(jd.Content)
		resumeContent = truncate(selectedResume.Content, 3000)
	} else {
		// Fallback to config-based profile
		resumeContent = fmt.Sprintf(`Skills: %s
Experience: %d years total (%d backend, %d frontend, %d devops)
Summary: %s`,
			a.profile.GetSkillsString(),
			a.profile.Profile.Experience.TotalYears,
			a.profile.Profile.Experience.BackendYears,
			a.profile.Profile.Experience.FrontendYears,
			a.profile.Profile.Experience.DevOpsYears,
			a.profile.Profile.Summary,
		)
	}

	return fmt.Sprintf(`You are a career advisor helping evaluate job opportunities from recruiters.

MY PROFILE/RESUME:
%s

MY PREFERENCES:
- Job types interested in: %s
- Location preferences: %s

JOB DESCRIPTION FROM RECRUITER:
%s

TASK:
Analyze this job description based on my resume/profile and provide:
1. A match score (0-100) based on skills, experience, and job requirements
2. Key reasons for the score (pros and cons)
3. Which of my key skills match this role
4. What skills might be missing or need development
5. Your overall recommendation

Note: Salary and location information may not be provided by recruiter, so focus primarily on skill and experience match.

Respond ONLY with valid JSON in this exact format:
{
  "match_score": 85,
  "reasoning": "Brief overall assessment",
  "pros": ["reason 1", "reason 2", "reason 3"],
  "cons": ["concern 1", "concern 2"],
  "key_skills_matched": ["skill 1", "skill 2", "skill 3"],
  "missing_skills": ["skill 1", "skill 2"]
}`,
		resumeContent,
		strings.Join(a.profile.Profile.Preferences.JobTypes, ", "),
		strings.Join(a.profile.Profile.Preferences.Locations, ", "),
		truncate(jd.Content, 4000),
	)
}

// parseAnalysisResponse extracts the JSON from Claude's response
func (a *JDAnalyzer) parseAnalysisResponse(response string) (*AnalysisResult, error) {
	// Claude might wrap JSON in markdown code blocks, so clean it up
	cleaned := strings.TrimSpace(response)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var result AnalysisResult
	err := json.Unmarshal([]byte(cleaned), &result)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &result, nil
}

// GenerateCoverLetter creates a tailored cover letter for a job description
func (a *JDAnalyzer) GenerateCoverLetter(jd *JobDescription, userInput string) (string, error) {
	// Select best resume if available
	var resumeContent string
	if len(a.resumes) > 0 {
		selectedResume := a.selectBestResume(jd.Content)
		resumeContent = truncate(selectedResume.Content, 3000)
	} else {
		resumeContent = fmt.Sprintf(`Skills: %s
Experience: %d years
Summary: %s`,
			a.profile.GetSkillsString(),
			a.profile.Profile.Experience.TotalYears,
			a.profile.Profile.Summary,
		)
	}

	additionalContext := ""
	if userInput != "" {
		additionalContext = fmt.Sprintf("\n\nADDITIONAL CONTEXT FROM USER:\n%s", userInput)
	}

	prompt := fmt.Sprintf(`Write a professional cover letter for this job application.

MY PROFILE/RESUME:
%s

MY NAME:
%s

JOB DESCRIPTION:
%s
%s

INSTRUCTIONS:
Write a concise, professional cover letter (3-4 paragraphs) that:
1. Opens with enthusiasm for the role
2. Highlights my most relevant experience and skills from my resume
3. Demonstrates understanding of the role requirements
4. Closes with a call to action
5. Uses a professional but personable tone
6. Does NOT use placeholders - write complete sentences

If additional context was provided by the user, incorporate it naturally into the letter.`,
		resumeContent,
		a.profile.Profile.Name,
		truncate(jd.Content, 3000),
		additionalContext,
	)

	coverLetter, err := a.claudeClient.SendMessage(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to generate cover letter: %w", err)
	}

	return coverLetter, nil
}

// selectBestResume chooses the most appropriate resume for a job description
func (a *JDAnalyzer) selectBestResume(jdContent string) *resume.Resume {
	if len(a.resumes) == 0 {
		return nil
	}

	jdLower := strings.ToLower(jdContent)

	// Check for contract vs permanent keywords
	isContract := strings.Contains(jdLower, "contract") ||
		strings.Contains(jdLower, "c2c") ||
		strings.Contains(jdLower, "contractor")

	// Look for resume with matching keywords in filename
	for _, res := range a.resumes {
		filenameLower := strings.ToLower(res.Filename)

		if isContract && strings.Contains(filenameLower, "contract") {
			return res
		}
		if !isContract && strings.Contains(filenameLower, "permanent") {
			return res
		}

		// Check for role-specific keywords
		keywords := []string{"senior", "lead", "architect", "engineer", "developer", "backend", "frontend", "fullstack"}
		for _, keyword := range keywords {
			if strings.Contains(jdLower, keyword) && strings.Contains(filenameLower, keyword) {
				return res
			}
		}
	}

	// Default: return first resume
	return a.resumes[0]
}

// RefineCoverLetter regenerates a cover letter based on user feedback
func (a *JDAnalyzer) RefineCoverLetter(jd *JobDescription, previousLetter string, feedback string) (string, error) {
	// Select best resume if available
	var resumeContent string
	if len(a.resumes) > 0 {
		selectedResume := a.selectBestResume(jd.Content)
		resumeContent = truncate(selectedResume.Content, 3000)
	} else {
		resumeContent = fmt.Sprintf(`Skills: %s
Experience: %d years
Summary: %s`,
			a.profile.GetSkillsString(),
			a.profile.Profile.Experience.TotalYears,
			a.profile.Profile.Summary,
		)
	}

	prompt := fmt.Sprintf(`Refine this cover letter based on user feedback.

MY PROFILE/RESUME:
%s

MY NAME:
%s

JOB DESCRIPTION:
%s

PREVIOUS COVER LETTER:
%s

USER FEEDBACK:
%s

INSTRUCTIONS:
Rewrite the cover letter incorporating the user's feedback while maintaining:
1. Professional tone
2. 3-4 paragraph structure
3. Relevant experience from my resume
4. Enthusiasm for the role
5. Complete sentences (no placeholders)

Make the changes requested in the feedback while keeping what works well.`,
		resumeContent,
		a.profile.Profile.Name,
		truncate(jd.Content, 3000),
		previousLetter,
		feedback,
	)

	refinedLetter, err := a.claudeClient.SendMessage(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to refine cover letter: %w", err)
	}

	return refinedLetter, nil
}

// truncate limits string length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
