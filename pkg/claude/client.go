package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	APIBaseURL     = "https://api.anthropic.com/v1/messages"
	APIVersion     = "2023-06-01"
	DefaultModel   = "claude-sonnet-4-5-20250929"
	DefaultMaxTokens = 4096
)

// Client handles communication with Claude API
type Client struct {
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// NewClient creates a new Claude API client
func NewClient(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		Model:  DefaultModel,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second, // Go tip: always set timeouts!
		},
	}
}

// Message represents a single message in the conversation
type Message struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

// Request is the structure for Claude API requests
type Request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []Message `json:"messages"`
}

// Response is the structure for Claude API responses
type Response struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model        string `json:"model"`
	StopReason   string `json:"stop_reason"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// SendMessage sends a message to Claude and returns the response
// This is the main function you'll use to interact with Claude
func (c *Client) SendMessage(userMessage string) (string, error) {
	// Prepare the request body
	reqBody := Request{
		Model:     c.Model,
		MaxTokens: DefaultMaxTokens,
		Messages: []Message{
			{
				Role:    "user",
				Content: userMessage,
			},
		},
	}

	// Convert to JSON
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", APIBaseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers (required by Anthropic API)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", APIVersion)

	// Send request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close() // Go tip: always close response bodies!

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var apiResp Response
	err = json.Unmarshal(body, &apiResp)
	if err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract text from response
	if len(apiResp.Content) > 0 {
		return apiResp.Content[0].Text, nil
	}

	return "", fmt.Errorf("no content in response")
}
