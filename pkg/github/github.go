package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Repo represents a GitHub repository
type Repo struct {
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	Language    string    `json:"language"`
	Stars       int       `json:"stargazers_count"`
	Forks       int       `json:"forks_count"`
	URL         string    `json:"html_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Topics      []string  `json:"topics"`
	IsPrivate   bool      `json:"private"`
}

// FetchUserRepos fetches public repositories for a GitHub user
func FetchUserRepos(username string) ([]*Repo, error) {
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}

	// GitHub API endpoint
	url := fmt.Sprintf("https://api.github.com/users/%s/repos?per_page=100&sort=updated", username)

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers (GitHub requires User-Agent)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "jobseeker-cli")

	// Execute request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repos: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var repos []*Repo
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return repos, nil
}

// GetRepoLanguages returns unique programming languages from repos
func GetRepoLanguages(repos []*Repo) []string {
	languageMap := make(map[string]bool)
	var languages []string

	for _, repo := range repos {
		if repo.Language != "" && !languageMap[repo.Language] {
			languageMap[repo.Language] = true
			languages = append(languages, repo.Language)
		}
	}

	return languages
}

// GetTopRepos returns top N repositories by stars
func GetTopRepos(repos []*Repo, n int) []*Repo {
	if n >= len(repos) {
		return repos
	}

	// Simple bubble sort for top N
	sorted := make([]*Repo, len(repos))
	copy(sorted, repos)

	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			if sorted[j].Stars < sorted[j+1].Stars {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	return sorted[:n]
}
