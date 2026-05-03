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

	// Phase 1: search to find the LinkedIn profile URL.
	var profileURL, html string
	const maxSearchRetries = 5
	for attempt := 1; attempt <= maxSearchRetries; attempt++ {
		var err error
		if puppeteerClient != nil {
			profileURL, html, err = puppeteerClient.SearchLinkedIn(keywords, findLinkedInSearchEngine)
		} else {
			profileURL, html, err = pool.SearchAndFetchLinkedIn(keywords, findLinkedInSearchEngine)
		}
		if err != nil {
			if attempt < maxSearchRetries {
				log.Printf("Search attempt %d failed: %v — retrying in 5s...", attempt, err)
				time.Sleep(5 * time.Second)
				continue
			}
			log.Fatalf("Failed to find profile: %v", err)
		}
		break
	}

	fmt.Printf("Found profile: %s\n\n", profileURL)

	// Phase 2: parse the HTML, retrying with a fresh fetch on non-English pages.
	// A non-English page means the proxy/IP landed on a non-English region;
	// re-fetching picks a fresh session which may land on an English region.
	const maxParseRetries = 5
	var profile *linkedinpkg.Profile
	for attempt := 1; attempt <= maxParseRetries; attempt++ {
		var parseErr error
		profile, parseErr = linkedinpkg.ParseHTML(html, profileURL)
		if parseErr == nil {
			break
		}
		if errors.Is(parseErr, linkedinpkg.ErrNonEnglishPage) && attempt < maxParseRetries {
			log.Printf("Attempt %d: non-English page — re-fetching %s in 5s...", attempt, profileURL)
			time.Sleep(5 * time.Second)
			var fetchErr error
			if puppeteerClient != nil {
				html, fetchErr = puppeteerClient.FetchLinkedIn(profileURL)
			} else {
				html, fetchErr = pool.FetchLinkedInHTML(profileURL)
			}
			if fetchErr != nil {
				log.Printf("Re-fetch attempt %d failed: %v — retrying...", attempt, fetchErr)
			}
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
