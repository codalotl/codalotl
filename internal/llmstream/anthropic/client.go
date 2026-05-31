package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/codalotl/codalotl/internal/q/sseclient"
)

// DefaultBaseURL is Anthropic's direct API endpoint.
const DefaultBaseURL = "https://api.anthropic.com"

// DefaultVersion is sent in anthropic-version when no override is configured.
const DefaultVersion = "2023-06-01"
const requiredBetaContext1M = "context-1m-2025-08-07"

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

// WithBeta appends an anthropic-beta feature for all requests.
func WithBeta(beta string) Option {
	return func(c *Client) {
		c.betas = append(c.betas, beta)
	}
}

// Client sends streaming requests to Anthropic Messages API.
type Client struct {
	apiKey     string       // apiKey is sent as the x-api-key request header.
	httpClient *http.Client // httpClient performs outbound HTTP requests.
	baseURL    string       // baseURL is the Anthropic API origin.
	version    string       // version is sent as the anthropic-version request header.
	betas      []string     // betas are sent as the comma-separated anthropic-beta request header.
}

// New constructs a Client. apiKey is sent as x-api-key.
func New(apiKey string, opts ...Option) *Client {
	client := &Client{
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
		baseURL:    DefaultBaseURL,
		betas:      []string{requiredBetaContext1M},
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

// streamMessageRequest is the JSON body for a streaming Messages API request.
type streamMessageRequest struct {
	Model         string             `json:"model"`                    // Model is the Anthropic model name.
	MaxTokens     int64              `json:"max_tokens"`               // MaxTokens is the maximum number of tokens to generate.
	System        string             `json:"system,omitempty"`         // System is the optional system prompt.
	Messages      []MessageParam     `json:"messages"`                 // Messages is the conversation history to send.
	Tools         []ToolParam        `json:"tools,omitempty"`          // Tools is the set of tools available to the model.
	ToolChoice    *ToolChoiceParam   `json:"tool_choice,omitempty"`    // ToolChoice controls whether and how the model may use tools.
	Temperature   *float64           `json:"temperature,omitempty"`    // Temperature controls sampling; nil omits the parameter.
	ServiceTier   string             `json:"service_tier,omitempty"`   // ServiceTier is the Anthropic service tier.
	StopSequences []string           `json:"stop_sequences,omitempty"` // StopSequences are custom sequences that stop generation.
	Thinking      *ThinkingParam     `json:"thinking,omitempty"`       // Thinking configures Anthropic thinking when set.
	OutputConfig  *OutputConfigParam `json:"output_config,omitempty"`  // OutputConfig configures Anthropic output options when set.
	CacheControl  *CacheControlParam `json:"cache_control,omitempty"`  // CacheControl configures prompt caching for the request.
	Stream        bool               `json:"stream"`                   // Stream requests SSE streaming and is always true for StreamMessages.
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
		OutputConfig:  req.OutputConfig,
		CacheControl:  req.CacheControl,
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
	if len(c.betas) > 0 {
		httpReq.Header.Set("anthropic-beta", strings.Join(c.betas, ","))
	}

	sc := sseclient.New(sseclient.WithHTTPClient(c.httpClient))
	rawStream, err := sc.OpenRequest(httpReq)
	if err != nil {
		return nil, err
	}

	return newStream(rawStream), nil
}
