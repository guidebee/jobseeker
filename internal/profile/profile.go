package profile

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Profile represents your professional profile
// Used for matching jobs and generating applications
type Profile struct {
	Profile struct {
		Name     string   `yaml:"name"`
		Email    string   `yaml:"email"`
		Phone    string   `yaml:"phone"`
		Location string   `yaml:"location"`
		Skills   []string `yaml:"skills"`

		Experience struct {
			TotalYears   int `yaml:"total_years"`
			BackendYears int `yaml:"backend_years"`
			FrontendYears int `yaml:"frontend_years"`
			DevOpsYears  int `yaml:"devops_years"`
		} `yaml:"experience"`

		Preferences struct {
			JobTypes         []string `yaml:"job_types"`
			WorkArrangements []string `yaml:"work_arrangements"`
			SalaryMin        int      `yaml:"salary_min"`

			Contract struct {
				HourlyRateMin int `yaml:"hourly_rate_min"`
				DailyRateMin  int `yaml:"daily_rate_min"`
			} `yaml:"contract"`

			Locations        []string `yaml:"locations"`
		} `yaml:"preferences"`

		Summary string `yaml:"summary"`
	} `yaml:"profile"`

	JobBoards map[string]struct {
		Enabled    bool     `yaml:"enabled"`
		SearchURLs []string `yaml:"search_urls"`
	} `yaml:"job_boards"`
}

var CurrentProfile *Profile

// LoadProfile reads the config.yaml file
// In Go, it's common to return (value, error) - called the "comma ok" pattern
func LoadProfile(configPath string) (*Profile, error) {
	// Read the file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML into our struct
	var profile Profile
	err = yaml.Unmarshal(data, &profile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Store globally for easy access
	CurrentProfile = &profile
	return &profile, nil
}

// GetSkillsString returns skills as a comma-separated string
// Useful for passing to Claude API
func (p *Profile) GetSkillsString() string {
	if len(p.Profile.Skills) == 0 {
		return ""
	}

	result := p.Profile.Skills[0]
	for i := 1; i < len(p.Profile.Skills); i++ {
		result += ", " + p.Profile.Skills[i]
	}
	return result
}
