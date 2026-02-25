package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/codalotl/codalotl/internal/q/sseclient"
)

// DefaultBaseURL is Anthropic's direct API endpoint.
const DefaultBaseURL = "https://api.anthropic.com"

// DefaultVersion is sent in anthropic-version when no override is configured.
const DefaultVersion = "2023-06-01"

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithBaseURL overrides API origin (ex: proxy/testing endpoint).
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

// WithVersion overrides anthropic-version header value.
func WithVersion(version string) Option {
	return func(c *Client) {
		c.version = version
	}
}

// WithBeta appends an anthropic-beta header value for all requests.
func WithBeta(beta string) Option {
	return func(c *Client) {
		c.betas = append(c.betas, beta)
	}
}

// Client sends streaming requests to Anthropic Messages API.
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
	version    string
	betas      []string
}

// New constructs a Client. apiKey is sent as x-api-key.
func New(apiKey string, opts ...Option) *Client {
	client := &Client{
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
		baseURL:    DefaultBaseURL,
		version:    DefaultVersion,
	}
	for _, opt := range opts {
		opt(client)
	}
	if client.httpClient == nil {
		client.httpClient = http.DefaultClient
	}
	if client.baseURL == "" {
		client.baseURL = DefaultBaseURL
	}
	if client.version == "" {
		client.version = DefaultVersion
	}
	return client
}

type streamMessageRequest struct {
	Model         string           `json:"model"`
	MaxTokens     int64            `json:"max_tokens"`
	System        string           `json:"system,omitempty"`
	Messages      []MessageParam   `json:"messages"`
	Tools         []ToolParam      `json:"tools,omitempty"`
	ToolChoice    *ToolChoiceParam `json:"tool_choice,omitempty"`
	Temperature   *float64         `json:"temperature,omitempty"`
	ServiceTier   string           `json:"service_tier,omitempty"`
	StopSequences []string         `json:"stop_sequences,omitempty"`
	Thinking      *ThinkingParam   `json:"thinking,omitempty"`
	Stream        bool             `json:"stream"`
}

// StreamMessages starts POST /v1/messages in streaming mode.
func (c *Client) StreamMessages(ctx context.Context, req MessageRequest) (*Stream, error) {
	endpoint, err := url.JoinPath(c.baseURL, "/v1/messages")
	if err != nil {
		return nil, fmt.Errorf("anthropic: invalid base URL: %w", err)
	}

	bodyPayload := streamMessageRequest{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		System:        req.System,
		Messages:      req.Messages,
		Tools:         req.Tools,
		ToolChoice:    req.ToolChoice,
		Temperature:   req.Temperature,
		ServiceTier:   req.ServiceTier,
		StopSequences: req.StopSequences,
		Thinking:      req.Thinking,
		Stream:        true,
	}

	bodyBytes, err := json.Marshal(bodyPayload)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", c.version)
	for _, beta := range c.betas {
		httpReq.Header.Add("anthropic-beta", beta)
	}

	sc := sseclient.New(sseclient.WithHTTPClient(c.httpClient))
	rawStream, err := sc.OpenRequest(httpReq)
	if err != nil {
		return nil, err
	}

	return newStream(rawStream), nil
}
