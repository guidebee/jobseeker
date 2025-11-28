package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/guidebee/jobseeker/internal/database"
	"github.com/guidebee/jobseeker/internal/resume"
	"github.com/guidebee/jobseeker/pkg/claude"
	"github.com/guidebee/jobseeker/pkg/github"
	"github.com/spf13/cobra"
)

var (
	initForceRefresh bool
	initGithubUser   string
	initLinkedInURL  string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize user profile and cache data",
	Long: `Initializes user profile from config.yaml and caches:
- Resume content for faster analysis
- GitHub repositories (if username provided)
- LinkedIn profile URL
- Extracted search keywords from resume

This command should be run:
- On first use
- After updating resumes
- After changing profile information`,
	Run: runInit,
}

func runInit(cmd *cobra.Command, args []string) {
	fmt.Println("Initializing user profile...")

	// Load profile from config
	prof, err := initApp()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Get or create user
	user, err := database.GetOrCreateUser(
		prof.Profile.Email,
		prof.Profile.Name,
		prof.Profile.Location,
	)
	if err != nil {
		log.Fatalf("Failed to get/create user: %v", err)
	}

	fmt.Printf("✓ User: %s (%s)\n", user.Name, user.Email)

	// Check if profile data already exists
	db := database.GetDB()
	var existingData database.ProfileData
	result := db.Where("user_id = ?", user.ID).First(&existingData)

	if result.Error == nil && !initForceRefresh {
		fmt.Printf("\n✓ Profile already initialized (last updated: %s)\n", existingData.UpdatedAt.Format("2006-01-02 15:04"))
		fmt.Println("  Use --force to refresh")
		return
	}

	fmt.Println("\nLoading profile data...")

	// Load resumes
	resumesDir := "./resumes"
	resumes, err := resume.LoadResumes(resumesDir)
	if err != nil {
		log.Printf("Warning: No resumes found (%v)", err)
		resumes = []*resume.Resume{}
	} else {
		fmt.Printf("✓ Found %d resume(s)\n", len(resumes))
		for _, r := range resumes {
			fmt.Printf("  - %s\n", r.Filename)
		}
	}

	// Convert resumes to JSON
	resumesJSON, err := json.Marshal(resumes)
	if err != nil {
		log.Fatalf("Failed to marshal resumes: %v", err)
	}

	// Extract keywords from resumes
	var keywordsJSON string
	if len(resumes) > 0 {
		apiKey := os.Getenv("CLAUDE_API_KEY")
		if apiKey != "" {
			fmt.Println("\nExtracting keywords from resume...")
			claudeClient := claude.NewClient(apiKey)
			keywords, err := resume.ExtractKeywords(resumes[0], claudeClient)
			if err != nil {
				log.Printf("Warning: Failed to extract keywords: %v", err)
			} else {
				fmt.Printf("✓ Extracted keywords:\n")
				fmt.Printf("  Primary skills: %v\n", keywords.PrimarySkills)
				fmt.Printf("  Roles: %v\n", keywords.Roles)
				keywordsData, _ := json.Marshal(keywords)
				keywordsJSON = string(keywordsData)
			}
		}
	}

	// Fetch GitHub repos
	var githubReposJSON string
	githubUser := initGithubUser
	if githubUser == "" {
		// Try to get from config or environment
		githubUser = os.Getenv("GITHUB_USERNAME")
	}

	if githubUser != "" {
		fmt.Printf("\nFetching GitHub repos for: %s\n", githubUser)
		repos, err := github.FetchUserRepos(githubUser)
		if err != nil {
			log.Printf("Warning: Failed to fetch GitHub repos: %v", err)
		} else {
			fmt.Printf("✓ Found %d repositories\n", len(repos))
			reposData, _ := json.Marshal(repos)
			githubReposJSON = string(reposData)
		}
	}

	// LinkedIn URL
	linkedinURL := initLinkedInURL
	if linkedinURL == "" {
		linkedinURL = os.Getenv("LINKEDIN_URL")
	}

	// Save or update profile data
	profileData := database.ProfileData{
		UserID:         user.ID,
		ResumesJSON:    string(resumesJSON),
		GitHubRepos:    githubReposJSON,
		GitHubUser:     githubUser,
		LinkedInProfile: "", // Can be populated later with scraping
		LinkedInURL:    linkedinURL,
		SearchKeywords: keywordsJSON,
		ResumesCount:   len(resumes),
		LastInitAt:     time.Now(),
		InitVersion:    "1.0",
	}

	if result.Error == nil {
		// Update existing
		profileData.ID = existingData.ID
		if err := db.Save(&profileData).Error; err != nil {
			log.Fatalf("Failed to update profile data: %v", err)
		}
		fmt.Println("\n✓ Profile data updated")
	} else {
		// Create new
		if err := db.Create(&profileData).Error; err != nil {
			log.Fatalf("Failed to save profile data: %v", err)
		}
		fmt.Println("\n✓ Profile data initialized")
	}

	// Show summary
	fmt.Println("\nProfile Summary:")
	fmt.Printf("  User: %s <%s>\n", user.Name, user.Email)
	fmt.Printf("  Location: %s\n", user.Location)
	fmt.Printf("  Plan: %s\n", user.PlanType)
	fmt.Printf("  Resumes: %d cached\n", len(resumes))
	if githubUser != "" {
		fmt.Printf("  GitHub: @%s\n", githubUser)
	}
	if linkedinURL != "" {
		fmt.Printf("  LinkedIn: %s\n", linkedinURL)
	}

	// Show usage stats
	stats, err := database.GetUserStats(user.ID)
	if err == nil && stats["total_jobs"] > 0 {
		fmt.Println("\nUsage Stats:")
		fmt.Printf("  Total jobs: %d\n", stats["total_jobs"])
		fmt.Printf("  Analyzed: %d\n", stats["analyzed_jobs"])
		fmt.Printf("  Recommended: %d\n", stats["recommended_jobs"])
		fmt.Printf("  Applications: %d\n", stats["applications"])
	}

	fmt.Println("\n✓ Initialization complete!")
	fmt.Println("Run 'jobseeker scan' to start finding jobs")
}

func init() {
	initCmd.Flags().BoolVar(&initForceRefresh, "force", false, "Force refresh even if already initialized")
	initCmd.Flags().StringVar(&initGithubUser, "github", "", "GitHub username to fetch repositories")
	initCmd.Flags().StringVar(&initLinkedInURL, "linkedin", "", "LinkedIn profile URL")
}
