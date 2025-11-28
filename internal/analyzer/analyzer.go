package analyzer

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/guidebee/jobseeker/internal/database"
	"github.com/guidebee/jobseeker/internal/profile"
	"github.com/guidebee/jobseeker/internal/resume"
	"github.com/guidebee/jobseeker/pkg/claude"
)

// Analyzer handles job matching using Claude AI
type Analyzer struct {
	claudeClient *claude.Client
	profile      *profile.Profile
	resumes      []*resume.Resume
	useResumes   bool
}

// NewAnalyzer creates a new job analyzer
func NewAnalyzer(apiKey string, prof *profile.Profile) *Analyzer {
	return &Analyzer{
		claudeClient: claude.NewClient(apiKey),
		profile:      prof,
		resumes:      nil,
		useResumes:   false,
	}
}

// LoadResumes attempts to load resumes from the resumes directory
func (a *Analyzer) LoadResumes(resumesDir string) error {
	resumes, err := resume.LoadResumes(resumesDir)
	if err != nil {
		return err
	}

	if len(resumes) == 0 {
		return fmt.Errorf("no resumes found in %s", resumesDir)
	}

	a.resumes = resumes
	a.useResumes = true

	log.Printf("Loaded %d resume(s) from %s", len(resumes), resumesDir)
	for _, r := range resumes {
		log.Printf("  - %s", r.Filename)
	}

	return nil
}

// UseResumes returns true if analyzer is using resumes
func (a *Analyzer) UseResumes() bool {
	return a.useResumes && len(a.resumes) > 0
}

// GetResumeUsed returns the filename of the resume used for a job
func (a *Analyzer) GetResumeUsed(job *database.Job) string {
	if !a.useResumes || len(a.resumes) == 0 {
		return ""
	}
	selectedResume := resume.SelectBestResume(a.resumes, job.Title, job.JobType)
	return selectedResume.Filename
}

// AnalysisResult represents Claude's analysis of a job
type AnalysisResult struct {
	MatchScore int    `json:"match_score"` // 0-100
	Reasoning  string `json:"reasoning"`
	Pros       []string `json:"pros"`
	Cons       []string `json:"cons"`
}

// AnalyzeJob sends job details to Claude for analysis
// Returns a match score (0-100) and detailed reasoning
func (a *Analyzer) AnalyzeJob(job *database.Job) (*AnalysisResult, error) {
	// Build the prompt for Claude
	prompt := a.buildAnalysisPrompt(job)

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

	return result, nil
}

// buildAnalysisPrompt creates a detailed prompt for Claude
func (a *Analyzer) buildAnalysisPrompt(job *database.Job) string {
	// Build salary/rate preference text based on job type
	salaryPref := ""
	if job.JobType == "contract" {
		salaryPref = fmt.Sprintf("Contract rates: $%d+/hour or $%d+/day",
			a.profile.Profile.Preferences.Contract.HourlyRateMin,
			a.profile.Profile.Preferences.Contract.DailyRateMin)
	} else {
		salaryPref = fmt.Sprintf("Permanent salary: $%d+/year", a.profile.Profile.Preferences.SalaryMin)
	}

	// Use resume content if available, otherwise fall back to config
	if a.useResumes && len(a.resumes) > 0 {
		return a.buildResumeBasedPrompt(job, salaryPref)
	}

	return a.buildConfigBasedPrompt(job, salaryPref)
}

// buildResumeBasedPrompt creates a prompt using resume content
func (a *Analyzer) buildResumeBasedPrompt(job *database.Job, salaryPref string) string {
	// Select best resume for this job
	selectedResume := resume.SelectBestResume(a.resumes, job.Title, job.JobType)

	return fmt.Sprintf(`You are a career advisor helping evaluate job opportunities.

MY RESUME:
%s

MY PREFERENCES:
- %s
- Job types interested in: %s
- Location preferences: %s

JOB POSTING:
- Title: %s
- Company: %s
- Location: %s
- Salary/Rate: %s
- Job Type: %s (detected)
- Description: %s
- Requirements: %s

TASK:
Analyze this job opportunity based on my resume and provide:
1. A match score (0-100) based on skills, experience from resume, and job requirements
2. Key reasons for the score (pros and cons)
3. Your recommendation
4. Evaluate if compensation meets expectations
5. Identify which resume experiences are most relevant

Respond ONLY with valid JSON in this exact format:
{
  "match_score": 85,
  "reasoning": "Brief overall assessment",
  "pros": ["reason 1", "reason 2"],
  "cons": ["concern 1", "concern 2"]
}`,
		truncate(selectedResume.Content, 2000), // Limit resume length
		salaryPref,
		strings.Join(a.profile.Profile.Preferences.JobTypes, ", "),
		strings.Join(a.profile.Profile.Preferences.Locations, ", "),
		job.Title,
		job.Company,
		job.Location,
		job.Salary,
		job.JobType,
		truncate(job.Description, 1000),
		truncate(job.Requirements, 500),
	)
}

// buildConfigBasedPrompt creates a prompt using config.yaml (fallback)
func (a *Analyzer) buildConfigBasedPrompt(job *database.Job, salaryPref string) string {
	return fmt.Sprintf(`You are a career advisor helping evaluate job opportunities.

MY PROFILE:
- Skills: %s
- Experience: %d years total (%d backend, %d frontend, %d devops)
- Location: %s
- %s
- Job types interested in: %s
- Summary: %s

JOB POSTING:
- Title: %s
- Company: %s
- Location: %s
- Salary/Rate: %s
- Job Type: %s (detected)
- Description: %s
- Requirements: %s

TASK:
Analyze this job opportunity and provide:
1. A match score (0-100) based on skills, experience, and preferences
2. Key reasons for the score (pros and cons)
3. Your recommendation
4. For contract roles, evaluate if the rate meets minimum expectations
5. For permanent roles, evaluate if the salary meets minimum expectations

Respond ONLY with valid JSON in this exact format:
{
  "match_score": 85,
  "reasoning": "Brief overall assessment",
  "pros": ["reason 1", "reason 2"],
  "cons": ["concern 1", "concern 2"]
}`,
		a.profile.GetSkillsString(),
		a.profile.Profile.Experience.TotalYears,
		a.profile.Profile.Experience.BackendYears,
		a.profile.Profile.Experience.FrontendYears,
		a.profile.Profile.Experience.DevOpsYears,
		a.profile.Profile.Location,
		salaryPref,
		strings.Join(a.profile.Profile.Preferences.JobTypes, ", "),
		a.profile.Profile.Summary,
		job.Title,
		job.Company,
		job.Location,
		job.Salary,
		job.JobType,
		truncate(job.Description, 1000),
		truncate(job.Requirements, 500),
	)
}

// parseAnalysisResponse extracts the JSON from Claude's response
func (a *Analyzer) parseAnalysisResponse(response string) (*AnalysisResult, error) {
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

// truncate limits string length (useful for API token limits)
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// GenerateCoverLetter creates a tailored cover letter for a job
func (a *Analyzer) GenerateCoverLetter(job *database.Job) (string, error) {
	prompt := fmt.Sprintf(`Write a professional cover letter for this job application.

MY PROFILE:
- Name: %s
- Skills: %s
- Experience: %d years
- Summary: %s

JOB:
- Title: %s
- Company: %s
- Description: %s

Write a concise, professional cover letter (3-4 paragraphs).
Focus on relevant experience and enthusiasm for the role.
Do not use placeholders - write complete sentences.`,
		a.profile.Profile.Name,
		a.profile.GetSkillsString(),
		a.profile.Profile.Experience.TotalYears,
		a.profile.Profile.Summary,
		job.Title,
		job.Company,
		truncate(job.Description, 800),
	)

	coverLetter, err := a.claudeClient.SendMessage(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to generate cover letter: %w", err)
	}

	return coverLetter, nil
}
