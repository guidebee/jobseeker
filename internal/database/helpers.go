package database

import (
	"strings"
)

// DetectJobType attempts to determine if a job is contract or permanent
// based on title, salary text, and URL
func DetectJobType(title, salary, url string) string {
	// Combine all text for analysis
	combined := strings.ToLower(title + " " + salary + " " + url)

	// Contract indicators
	contractKeywords := []string{
		"contract",
		"contractor",
		"freelance",
		"hourly",
		"/hr",
		"per hour",
		"/day",
		"per day",
		"daily rate",
		"day rate",
		"temp",
		"temporary",
		"fixed term",
		"ftc",
	}

	for _, keyword := range contractKeywords {
		if strings.Contains(combined, keyword) {
			return "contract"
		}
	}

	// Permanent indicators
	permanentKeywords := []string{
		"permanent",
		"full-time",
		"full time",
		"perm",
		"per year",
		"per annum",
		"p.a.",
		"salary",
	}

	for _, keyword := range permanentKeywords {
		if strings.Contains(combined, keyword) {
			return "permanent"
		}
	}

	// Default to unknown if we can't determine
	return "unknown"
}

// IsContractRole returns true if the job is a contract role
func (j *Job) IsContractRole() bool {
	return j.JobType == "contract"
}

// IsPermanentRole returns true if the job is a permanent role
func (j *Job) IsPermanentRole() bool {
	return j.JobType == "permanent"
}
