package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	SkillsAPIBaseURL = "https://api.anthropic.com/v1/messages"
	FilesAPIBaseURL  = "https://api.anthropic.com/v1/files"
)

// SkillsClient handles communication with Claude Skills API
type SkillsClient struct {
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// NewSkillsClient creates a new Claude Skills API client
func NewSkillsClient(apiKey string) *SkillsClient {
	return &SkillsClient{
		APIKey: apiKey,
		Model:  DefaultModel,
		HTTPClient: &http.Client{
			Timeout: 300 * time.Second, // 5 minutes for complex file operations
		},
	}
}

// Tool represents a tool in the request
type Tool struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// Skill represents a skill configuration
type Skill struct {
	Type    string `json:"type"`
	SkillID string `json:"skill_id"`
	Version string `json:"version"`
}

// Container represents the skills container configuration
type Container struct {
	Skills []Skill `json:"skills"`
}

// SkillsRequest is the structure for Claude Skills API requests
type SkillsRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []Message `json:"messages"`
	Tools     []Tool    `json:"tools,omitempty"`
	Container Container `json:"container,omitempty"`
}

// ContentBlock represents different types of content in the response
type ContentBlock struct {
	Type     string                 `json:"type"`
	Text     string                 `json:"text,omitempty"`
	ID       string                 `json:"id,omitempty"`
	Name     string                 `json:"name,omitempty"`
	Input    map[string]interface{} `json:"input,omitempty"`
	Output   map[string]interface{} `json:"output,omitempty"`
	Source   map[string]interface{} `json:"source,omitempty"`
	Content  interface{}            `json:"content,omitempty"` // Can be string or array
	ToolUseID string                `json:"tool_use_id,omitempty"`
}

// SkillsResponse is the structure for Claude Skills API responses
type SkillsResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	StopSequence string         `json:"stop_sequence,omitempty"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// SendMessageWithSkill sends a message to Claude with specified skill enabled
func (c *SkillsClient) SendMessageWithSkill(userMessage string, skillID string) (*SkillsResponse, error) {
	// Prepare the request body with specified skill
	reqBody := SkillsRequest{
		Model:     c.Model,
		MaxTokens: 8192, // Larger token limit for document operations
		Messages: []Message{
			{
				Role:    "user",
				Content: userMessage,
			},
		},
		Tools: []Tool{
			{
				Type: "code_execution_20250825",
				Name: "code_execution",
			},
		},
		Container: Container{
			Skills: []Skill{
				{
					Type:    "anthropic",
					SkillID: skillID,
					Version: "latest",
				},
			},
		},
	}

	// Convert to JSON
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", SkillsAPIBaseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers (required by Anthropic Skills API)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", APIVersion)
	req.Header.Set("anthropic-beta", "code-execution-2025-08-25,skills-2025-10-02")

	// Send request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var apiResp SkillsResponse
	err = json.Unmarshal(body, &apiResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &apiResp, nil
}

// SendMessageWithDocx sends a message to Claude with docx skill enabled
func (c *SkillsClient) SendMessageWithDocx(userMessage string) (*SkillsResponse, error) {
	return c.SendMessageWithSkill(userMessage, "docx")
}

// SendMessageWithXlsx sends a message to Claude with xlsx skill enabled
func (c *SkillsClient) SendMessageWithXlsx(userMessage string) (*SkillsResponse, error) {
	return c.SendMessageWithSkill(userMessage, "xlsx")
}

// DownloadFile downloads a file from Claude API by file ID
func (c *SkillsClient) DownloadFile(fileID, outputPath string) error {
	// Create HTTP request
	url := fmt.Sprintf("%s/%s/content", FilesAPIBaseURL, fileID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", APIVersion)

	// Send request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Create output directory if it doesn't exist
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Copy response body to file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// ExtractFileID extracts file ID from Skills API response
func ExtractFileID(response *SkillsResponse) (string, error) {
	for _, content := range response.Content {
		if content.Type == "tool_use" && content.Name == "code_execution" {
			// Check for file in the output
			if _, ok := content.Input["code"].(string); ok {
				// Look for file references in the code execution
				// This is a simplified extraction - actual implementation may vary
				if source, ok := content.Source["data"].(string); ok {
					return source, nil
				}
			}
		}
		// Check for document references in source
		if content.Source != nil {
			if fileID, ok := content.Source["file_id"].(string); ok {
				return fileID, nil
			}
		}
	}

	return "", fmt.Errorf("no file ID found in response")
}
