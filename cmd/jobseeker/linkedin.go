package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/guidebee/jobseeker/pkg/browser"
	claudepkg "github.com/guidebee/jobseeker/pkg/claude"
	linkedinpkg "github.com/guidebee/jobseeker/pkg/linkedin"
	puppeteerpkg "github.com/guidebee/jobseeker/pkg/puppeteer"
	"github.com/spf13/cobra"
)

var linkedinCmd = &cobra.Command{
	Use:   "linkedin <user-id>",
	Short: "Fetch and display a LinkedIn public profile",
	Long: `Fetches a public LinkedIn profile by user ID and prints it to the console.

The user ID is the part after /in/ in the LinkedIn URL.

Examples:
  jobseeker linkedin guidebee
  jobseeker linkedin john-doe-123456
  jobseeker linkedin https://www.linkedin.com/in/guidebee/`,
	Args: cobra.ExactArgs(1),
	Run:  runLinkedIn,
}

func runLinkedIn(cmd *cobra.Command, args []string) {
	userID := strings.TrimSpace(args[0])
	var profileURL string
	if strings.HasPrefix(userID, "http") {
		profileURL = userID
	} else {
		profileURL = fmt.Sprintf("https://www.linkedin.com/in/%s/", userID)
	}

	fmt.Printf("Fetching LinkedIn profile: %s\n\n", profileURL)

	// Try the Puppeteer service first (PUPPETEER_SERVICE_URL env var).
	// This is the preferred path on Windows where spawned headless Chrome
	// processes are blocked from making network requests by Windows Defender.
	var profile *linkedinpkg.Profile
	if svcURL := os.Getenv("PUPPETEER_SERVICE_URL"); svcURL != "" {
		log.Printf("Using puppeteer service at %s", svcURL)
		client := puppeteerpkg.NewClient(svcURL)
		if err := client.Ping(); err != nil {
			log.Fatalf("Puppeteer service unreachable: %v\nStart it with: cd puppeteer-service && npm start", err)
		}
		const maxRetries = 5
		for attempt := 1; attempt <= maxRetries; attempt++ {
			html, err := client.FetchLinkedIn(profileURL)
			if err != nil {
				if attempt < maxRetries {
					log.Printf("Attempt %d failed: %v — retrying in 5s...", attempt, err)
					time.Sleep(5 * time.Second)
					continue
				}
				log.Fatalf("Failed to fetch profile via puppeteer service: %v", err)
			}
			var parseErr error
			profile, parseErr = linkedinpkg.ParseHTML(html, profileURL)
			if parseErr != nil {
				if errors.Is(parseErr, linkedinpkg.ErrNonEnglishPage) && attempt < maxRetries {
					log.Printf("Attempt %d: %v — retrying in 5s...", attempt, parseErr)
					time.Sleep(5 * time.Second)
					continue
				}
				log.Fatalf("Failed to parse profile HTML: %v", parseErr)
			}
			break
		}
	} else {
		// Fall back to in-process go-rod browser pool.
		pool := browser.NewPool()
		if err := pool.Init(); err != nil {
			log.Printf("Warning: could not start browser pool (%v) — falling back to direct HTTP", err)
			pool = nil
		}
		if pool != nil {
			defer pool.Close()
		}
		var err error
		profile, err = linkedinpkg.FetchProfile(profileURL, pool)
		if err != nil {
			log.Fatalf("Failed to fetch profile: %v", err)
		}
	}

	if apiKey := os.Getenv("CLAUDE_API_KEY"); apiKey != "" {
		fmt.Println("Inferring skills via Claude AI...")
		client := claudepkg.NewClient(apiKey)
		if err := profile.InferSkills(client.SendMessage); err != nil {
			log.Printf("Warning: could not infer skills: %v", err)
		}
	}

	fmt.Println(profile.FormatAsCV())
}

func init() {
	rootCmd.AddCommand(linkedinCmd)
}
