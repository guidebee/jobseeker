// Package puppeteer provides a Go client for the Puppeteer stealth service.
// The service navigates URLs through a headless Chrome browser with the
// stealth plugin, bypassing bot-detection systems like LinkedIn's.
//
// Set PUPPETEER_SERVICE_URL in the environment to enable this path.
// Example: PUPPETEER_SERVICE_URL=http://localhost:3001
package puppeteer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client calls the Puppeteer service's POST /fetch endpoint.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates a client pointed at the given service base URL
// (e.g. "http://localhost:3001").
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

type fetchRequest struct {
	URL          string            `json:"url"`
	Method       string            `json:"method"`
	Headers      map[string]string `json:"headers,omitempty"`
	ResponseType string            `json:"response_type,omitempty"` // "html" or "json"
}

type htmlResponse struct {
	HTML  string `json:"html"`
	Error string `json:"error"`
}

// FetchHTML navigates to url via the stealth browser and returns the fully
// rendered page's outer HTML. The service must support response_type="html"
// (see patch instructions in pkg/puppeteer/README).
func (c *Client) FetchHTML(url string) (string, error) {
	reqBody := fetchRequest{
		URL:          url,
		Method:       "GET",
		ResponseType: "html",
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.http.Post(c.baseURL+"/fetch", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("puppeteer service unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read service response: %w", err)
	}

	if resp.StatusCode != 200 {
		// Try to extract error message from JSON body.
		var errResp struct {
			Error string `json:"error"`
		}
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Error != "" {
			return "", fmt.Errorf("puppeteer service: %s", errResp.Error)
		}
		return "", fmt.Errorf("puppeteer service returned HTTP %d", resp.StatusCode)
	}

	var result htmlResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse service response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("puppeteer service: %s", result.Error)
	}
	if result.HTML == "" {
		return "", fmt.Errorf("puppeteer service returned empty HTML")
	}

	return result.HTML, nil
}

// FetchLinkedIn calls POST /fetch-linkedin and returns the LinkedIn profile
// page HTML. The service does the Google-search-first approach internally.
func (c *Client) FetchLinkedIn(profileURL string) (string, error) {
	reqBody := struct {
		URL string `json:"url"`
	}{URL: profileURL}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.http.Post(c.baseURL+"/fetch-linkedin", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("puppeteer service unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read service response: %w", err)
	}

	var result htmlResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse service response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("puppeteer service: %s", result.Error)
	}
	if result.HTML == "" {
		return "", fmt.Errorf("puppeteer service returned empty HTML")
	}
	return result.HTML, nil
}

type searchLinkedInResponse struct {
	HTML       string `json:"html"`
	ProfileURL string `json:"profileURL"`
	Error      string `json:"error"`
}

// SearchLinkedIn does a keyword search via the given engine ("bing" or "google"),
// finds the first matching LinkedIn profile, and returns its URL and HTML.
func (c *Client) SearchLinkedIn(keywords, searchEngine string) (profileURL, html string, err error) {
	reqBody := struct {
		Keywords string `json:"keywords"`
		Engine   string `json:"engine,omitempty"`
	}{Keywords: keywords, Engine: searchEngine}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.http.Post(c.baseURL+"/search-linkedin", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", "", fmt.Errorf("puppeteer service unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read service response: %w", err)
	}

	var result searchLinkedInResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("failed to parse service response: %w", err)
	}
	if result.Error != "" {
		return "", "", fmt.Errorf("puppeteer service: %s", result.Error)
	}
	if result.HTML == "" {
		return "", "", fmt.Errorf("puppeteer service returned empty HTML")
	}
	return result.ProfileURL, result.HTML, nil
}

// SearchLinkedInURL does a keyword search via the given engine and returns only
// the discovered LinkedIn profile URL — without fetching the profile HTML.
// Use FetchLinkedIn separately to retrieve the HTML with its own retry loop.
func (c *Client) SearchLinkedInURL(keywords, searchEngine string) (string, error) {
	reqBody := struct {
		Keywords string `json:"keywords"`
		Engine   string `json:"engine,omitempty"`
	}{Keywords: keywords, Engine: searchEngine}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.http.Post(c.baseURL+"/search-url", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("puppeteer service unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read service response: %w", err)
	}

	var result struct {
		ProfileURL string `json:"profileURL"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse service response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("puppeteer service: %s", result.Error)
	}
	if result.ProfileURL == "" {
		return "", fmt.Errorf("puppeteer service returned empty profile URL")
	}
	return result.ProfileURL, nil
}

// Ping checks the service health endpoint.
func (c *Client) Ping() error {
	resp, err := c.http.Get(c.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("puppeteer service unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("puppeteer service health check failed: HTTP %d", resp.StatusCode)
	}
	return nil
}
