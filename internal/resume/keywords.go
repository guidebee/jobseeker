package resume

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/guidebee/jobseeker/pkg/claude"
)

// KeywordExtraction represents extracted keywords from resume
type KeywordExtraction struct {
	PrimarySkills    []string `json:"primary_skills"`     // Main technical skills
	SecondarySkills  []string `json:"secondary_skills"`   // Supporting skills
	Roles            []string `json:"roles"`              // Job titles/roles
	Industries       []string `json:"industries"`         // Industry keywords
	Certifications   []string `json:"certifications"`     // Certs/qualifications
	SearchKeywords   []string `json:"search_keywords"`    // Suggested search terms
}

// ExtractKeywords uses Claude to analyze resume and extract search keywords
func ExtractKeywords(resume *Resume, claudeClient *claude.Client) (*KeywordExtraction, error) {
	prompt := buildKeywordPrompt(resume.Content)

	response, err := claudeClient.SendMessage(prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to extract keywords: %w", err)
	}

	// Parse response
	keywords, err := parseKeywordResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse keywords: %w", err)
	}

	return keywords, nil
}

// buildKeywordPrompt creates a prompt for Claude to extract keywords
func buildKeywordPrompt(resumeContent string) string {
	return fmt.Sprintf(`Analyze this resume and extract search keywords for job hunting.

RESUME:
%s

TASK:
Extract and categorize keywords that would be useful for job searching. Focus on:
1. Primary technical skills (programming languages, frameworks, tools)
2. Secondary/supporting skills
3. Job roles/titles the person has held or is qualified for
4. Industries/domains with experience
5. Certifications or qualifications
6. Suggested job search keywords (combinations of skills + roles)

Respond ONLY with valid JSON in this exact format:
{
  "primary_skills": ["Go", "Python", "Docker"],
  "secondary_skills": ["Git", "Linux", "Agile"],
  "roles": ["Senior Software Engineer", "Backend Developer", "Tech Lead"],
  "industries": ["FinTech", "E-commerce", "SaaS"],
  "certifications": ["AWS Certified", "Scrum Master"],
  "search_keywords": ["golang developer", "senior backend engineer", "python developer", "devops engineer"]
}

Keep search_keywords specific, relevant, and suitable for job board searches.
Limit to 8-10 most relevant search keywords.`, truncate(resumeContent, 3000))
}

// parseKeywordResponse extracts JSON from Claude's response
func parseKeywordResponse(response string) (*KeywordExtraction, error) {
	// Clean up response
	cleaned := strings.TrimSpace(response)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var keywords KeywordExtraction
	err := json.Unmarshal([]byte(cleaned), &keywords)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &keywords, nil
}

// GenerateSearchURLs creates SEEK search URLs from keywords
func GenerateSearchURLs(keywords *KeywordExtraction, location string) []string {
	var urls []string

	// Generate URLs from search keywords
	for _, keyword := range keywords.SearchKeywords {
		// URL encode the keyword
		encodedKeyword := strings.ReplaceAll(keyword, " ", "+")
		url := fmt.Sprintf("https://www.seek.com.au/jobs?keywords=%s&location=%s",
			encodedKeyword, location)
		urls = append(urls, url)
	}

	// Add role-based searches
	for _, role := range keywords.Roles {
		if len(urls) >= 10 { // Limit total URLs
			break
		}
		encodedRole := strings.ReplaceAll(role, " ", "+")
		url := fmt.Sprintf("https://www.seek.com.au/jobs?keywords=%s&location=%s",
			encodedRole, location)
		urls = append(urls, url)
	}

	return urls
}

// MergeSearchURLs combines dynamic (from resume) and static (from config) URLs
// Removes duplicates
func MergeSearchURLs(dynamicURLs []string, staticURLs []string) []string {
	urlMap := make(map[string]bool)
	var merged []string

	// Add static URLs first (priority)
	for _, url := range staticURLs {
		if url != "" && !urlMap[url] {
			urlMap[url] = true
			merged = append(merged, url)
		}
	}

	// Add dynamic URLs
	for _, url := range dynamicURLs {
		if url != "" && !urlMap[url] {
			urlMap[url] = true
			merged = append(merged, url)
		}
	}

	return merged
}

// truncate limits string length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
