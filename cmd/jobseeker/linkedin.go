package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/guidebee/jobseeker/pkg/browser"
	claudepkg "github.com/guidebee/jobseeker/pkg/claude"
	linkedinpkg "github.com/guidebee/jobseeker/pkg/linkedin"
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

	// Initialise stealth browser pool.
	pool := browser.NewPool()
	if err := pool.Init(); err != nil {
		log.Printf("Warning: could not start browser pool (%v) — falling back to direct HTTP", err)
		pool = nil
	}
	if pool != nil {
		defer pool.Close()
	}

	profile, err := linkedinpkg.FetchProfile(profileURL, pool)
	if err != nil {
		log.Fatalf("Failed to fetch profile: %v", err)
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
