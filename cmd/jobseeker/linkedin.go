package main

import (
	"fmt"
	"log"
	"strings"

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
  jobseeker linkedin john-doe-123456`,
	Args: cobra.ExactArgs(1),
	Run:  runLinkedIn,
}

func runLinkedIn(cmd *cobra.Command, args []string) {
	userID := strings.TrimSpace(args[0])
	// Allow passing either a full URL or just the user ID.
	var profileURL string
	if strings.HasPrefix(userID, "http") {
		profileURL = userID
	} else {
		profileURL = fmt.Sprintf("https://www.linkedin.com/in/%s/", userID)
	}

	fmt.Printf("Fetching LinkedIn profile: %s\n\n", profileURL)

	profile, err := linkedinpkg.FetchProfile(profileURL)
	if err != nil {
		log.Fatalf("Failed to fetch profile: %v", err)
	}

	fmt.Println(profile.FormatAsCV())
}

func init() {
	rootCmd.AddCommand(linkedinCmd)
}
