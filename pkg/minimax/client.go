package minimax

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	APIBaseURL      = "https://api.minimax.io/v1/text/chatcompletion_v2"
	// MiniMax retired the M2.x models (error 2069, November 2026); M1 is the model that answers. Override with MINIMAX_MODEL.
	DefaultModel    = "MiniMax-M1"
	DefaultMaxTokens = 8192
)

// Client handles communication with MiniMax API
type Client struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient creates a new MiniMax API client
func NewClient(apiKey string) *Client {
	model := DefaultModel
	if override := os.Getenv("MINIMAX_MODEL"); override != "" {
		model = override
	}
	return &Client{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: APIBaseURL,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model               string    `json:"model"`
	Messages            []message `json:"messages"`
	MaxCompletionTokens int       `json:"max_completion_tokens"`
}

type response struct {
	// MiniMax reports account problems (a rejected key, no balance, rate limits) as HTTP 200 with this block and no choices.
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// SendMessage sends a user message to MiniMax and returns the text response
func (c *Client) SendMessage(userMessage string) (string, error) {
	reqBody := request{
		Model: c.Model,
		Messages: []message{
			{Role: "user", Content: userMessage},
		},
		MaxCompletionTokens: DefaultMaxTokens,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.BaseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp response
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.BaseResp.StatusCode != 0 {
		return "", fmt.Errorf("MiniMax API error %d: %s", apiResp.BaseResp.StatusCode, apiResp.BaseResp.StatusMsg)
	}

	if len(apiResp.Choices) > 0 {
		if apiResp.Choices[0].Message.Content == "" {
			return "", fmt.Errorf("MiniMax returned an empty answer (finish reason %q)", apiResp.Choices[0].FinishReason)
		}
		return apiResp.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("MiniMax returned no choices and no error")
}