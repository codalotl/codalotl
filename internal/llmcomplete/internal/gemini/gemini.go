package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// defaultBaseURL is the Gemini API base URL.
	defaultBaseURL = "https://generativelanguage.googleapis.com"
)

// Client is a minimal Gemini client that supports generating content via text chat.
//
// The client is intentionally small and mirrors the shape of the Anthropic client in this package.
type Client struct {
	// APIKey is the Gemini API key used for authentication.
	APIKey string
	// HTTP is the HTTP client used to make requests. If nil, a default client with timeout is used.
	HTTP *http.Client
	// BaseURL is the Gemini API base URL. If empty, a default value is used.
	BaseURL string
}

// NewClient returns a new Client with sensible defaults applied.
func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 180 * time.Second},
		BaseURL: defaultBaseURL,
	}
}

// GenerateContentRequest is the minimal request payload for the Gemini generateContent API.
//
// The model is specified separately because the REST path encodes it. Only text parts are supported.
type GenerateContentRequest struct {
	Model             string            `json:"-"`
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
	SafetySettings    []SafetySetting   `json:"safetySettings,omitempty"`
}

// Content represents a single conversational message.
type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

// Part is a minimal text-only content part.
type Part struct {
	Text string `json:"text,omitempty"`
}

// GenerationConfig captures basic generation controls supported by the minimal client.
type GenerationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
	TopP            float64 `json:"topP,omitempty"`
	TopK            int     `json:"topK,omitempty"`
}

// SafetySetting represents a minimal safety setting for the request.
type SafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

// GenerateContentResponse is a reduced representation of the Gemini response payload.
type GenerateContentResponse struct {
	Candidates    []Candidate    `json:"candidates"`
	ModelVersion  string         `json:"modelVersion,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// Candidate represents a single generation candidate returned by Gemini.
type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
	Index        int     `json:"index,omitempty"`
}

// UsageMetadata contains token accounting information returned by Gemini.
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount int `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount      int `json:"totalTokenCount,omitempty"`
}

// Text returns the first candidate's concatenated text parts.
func (r *GenerateContentResponse) Text() string {
	if r == nil {
		return ""
	}
	for _, cand := range r.Candidates {
		var b strings.Builder
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				b.WriteString(part.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}

// APIError represents the structured error response returned by Gemini.
type APIError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// GenerateContent sends a generateContent request and returns the parsed response.
func (c *Client) GenerateContent(ctx context.Context, reqPayload GenerateContentRequest) (*GenerateContentResponse, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return nil, errors.New("missing API key")
	}
	if strings.TrimSpace(reqPayload.Model) == "" {
		return nil, errors.New("missing model")
	}

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	base := strings.TrimRight(baseURL, "/")
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", base, url.PathEscape(reqPayload.Model))
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	q := u.Query()
	q.Set("key", c.APIKey)
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "application/json")

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		var apiErr APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			status := apiErr.Error.Status
			if status == "" {
				status = http.StatusText(httpResp.StatusCode)
			}
			return nil, fmt.Errorf("gemini error (%s): %s", status, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("gemini error: status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var out GenerateContentResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &out, nil
}
