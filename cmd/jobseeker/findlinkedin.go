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

var findLinkedInSearchEngine string

var findLinkedInCmd = &cobra.Command{
	Use:   "findlinkedin <keywords...>",
	Short: "Search for a LinkedIn profile by keywords and display it",
	Long: `Searches for the first LinkedIn profile matching the given keywords,
then fetches and displays that profile.

Uses Bing by default (--search-engine=bing). Switch to Google with
--search-engine=google if Bing returns poor results for a query.

Keywords can be any combination of name, company, location, or role that
identifies the person you are looking for.

Examples:
  jobseeker findlinkedin James Shen Australia
  jobseeker findlinkedin "James Shen" "Victorian Electoral Commission"
  jobseeker findlinkedin John Smith Google Engineer Sydney --search-engine=google`,
	Args: cobra.MinimumNArgs(1),
	Run:  runFindLinkedIn,
}

func runFindLinkedIn(cmd *cobra.Command, args []string) {
	keywords := strings.Join(args, " ")
	fmt.Printf("Searching for LinkedIn profile: %q\n\n", keywords)

	svcURL := os.Getenv("PUPPETEER_SERVICE_URL")

	var puppeteerClient *puppeteerpkg.Client
	var pool *browser.Pool

	if svcURL != "" {
		log.Printf("Using puppeteer service at %s", svcURL)
		puppeteerClient = puppeteerpkg.NewClient(svcURL)
		if err := puppeteerClient.Ping(); err != nil {
			log.Fatalf("Puppeteer service unreachable: %v\nStart it with: cd puppeteer-service && npm start", err)
		}
	} else {
		pool = browser.NewPool()
		if err := pool.Init(); err != nil {
			log.Fatalf("Could not start browser pool: %v", err)
		}
		defer pool.Close()
	}

	// Phase 1: search once to discover the LinkedIn profile URL.
	// The search itself (Bing/SerpAPI) is reliable — no need to retry it in a loop.
	var profileURL string
	if puppeteerClient != nil {
		var err error
		profileURL, err = puppeteerClient.SearchLinkedInURL(keywords, findLinkedInSearchEngine)
		if err != nil {
			log.Fatalf("Failed to find LinkedIn profile URL: %v", err)
		}
	} else {
		var html string
		var err error
		profileURL, html, err = pool.SearchAndFetchLinkedIn(keywords, findLinkedInSearchEngine)
		if err != nil {
			log.Fatalf("Failed to find profile: %v", err)
		}
		_ = html
	}

	fmt.Printf("Found profile: %s\n\n", profileURL)

	// Phase 2: fetch and parse the profile HTML, retrying on block or non-English page.
	// Uses FetchLinkedIn (same path as `linkedin` command) — fresh session each attempt.
	const maxRetries = 5
	var profile *linkedinpkg.Profile
	var html string
	for attempt := 1; attempt <= maxRetries; attempt++ {
		var fetchErr error
		if puppeteerClient != nil {
			html, fetchErr = puppeteerClient.FetchLinkedIn(profileURL)
		} else {
			html, fetchErr = pool.FetchLinkedInHTML(profileURL)
		}
		if fetchErr != nil {
			if attempt < maxRetries {
				log.Printf("Fetch attempt %d/%d failed: %v — retrying in 20s...", attempt, maxRetries, fetchErr)
				time.Sleep(20 * time.Second)
				continue
			}
			log.Fatalf("Failed to fetch profile: %v", fetchErr)
		}
		var parseErr error
		profile, parseErr = linkedinpkg.ParseHTML(html, profileURL)
		if parseErr == nil {
			break
		}
		retryable := errors.Is(parseErr, linkedinpkg.ErrNonEnglishPage) || errors.Is(parseErr, linkedinpkg.ErrBlocked)
		if retryable && attempt < maxRetries {
			log.Printf("Attempt %d/%d: %v — retrying in 20s...", attempt, maxRetries, parseErr)
			time.Sleep(20 * time.Second)
			continue
		}
		log.Fatalf("Failed to parse profile: %v", parseErr)
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
	rootCmd.AddCommand(findLinkedInCmd)
	findLinkedInCmd.Flags().StringVar(&findLinkedInSearchEngine, "search-engine", "bing", "Search engine to use: bing or google")
}
