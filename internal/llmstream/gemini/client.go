package gemini

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

// DefaultBaseURL is Gemini's direct API endpoint.
const DefaultBaseURL = "https://generativelanguage.googleapis.com"

// DefaultAPIVersion is the REST version used for interactions requests.
const DefaultAPIVersion = "v1beta"

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithBaseURL overrides API origin.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

// WithAPIVersion overrides the REST version path segment.
func WithAPIVersion(version string) Option {
	return func(c *Client) {
		c.apiVersion = version
	}
}

// Client sends streaming requests to Gemini Interactions API.
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
	apiVersion string
}

// New constructs a Client. apiKey is sent as x-goog-api-key.
func New(apiKey string, opts ...Option) *Client {
	client := &Client{
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
		baseURL:    DefaultBaseURL,
		apiVersion: DefaultAPIVersion,
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
	if client.apiVersion == "" {
		client.apiVersion = DefaultAPIVersion
	}
	client.apiVersion = strings.Trim(client.apiVersion, "/")
	return client
}

type streamInteractionRequest struct {
	Model                 string            `json:"model"`
	Input                 []Turn            `json:"input"`
	SystemInstruction     string            `json:"system_instruction,omitempty"`
	Tools                 []Tool            `json:"tools,omitempty"`
	ResponseFormat        map[string]any    `json:"response_format,omitempty"`
	ResponseMIMEType      string            `json:"response_mime_type,omitempty"`
	Stream                bool              `json:"stream"`
	Store                 *bool             `json:"store,omitempty"`
	GenerationConfig      *GenerationConfig `json:"generation_config,omitempty"`
	PreviousInteractionID string            `json:"previous_interaction_id,omitempty"`
}

// CreateInteraction starts POST /{version}/interactions?alt=sse in streaming mode.
func (c *Client) CreateInteraction(ctx context.Context, req InteractionRequest) (*Stream, error) {
	endpoint, err := url.JoinPath(c.baseURL, c.apiVersion, "interactions")
	if err != nil {
		return nil, fmt.Errorf("gemini: invalid base URL: %w", err)
	}

	bodyPayload := streamInteractionRequest{
		Model:                 req.Model,
		Input:                 req.Input,
		SystemInstruction:     req.SystemInstruction,
		Tools:                 req.Tools,
		ResponseFormat:        req.ResponseFormat,
		ResponseMIMEType:      req.ResponseMIMEType,
		Stream:                true,
		Store:                 req.Store,
		GenerationConfig:      req.GenerationConfig,
		PreviousInteractionID: req.PreviousInteractionID,
	}

	bodyBytes, err := json.Marshal(bodyPayload)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("gemini: parse endpoint: %w", err)
	}
	q := u.Query()
	q.Set("alt", "sse")
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	sc := sseclient.New(sseclient.WithHTTPClient(c.httpClient))
	rawStream, err := sc.OpenRequest(httpReq)
	if err != nil {
		return nil, err
	}

	return newStream(rawStream), nil
}

// GetInteractionStream starts GET /{version}/interactions/{id}?stream=true for resume or retrieval in streaming mode.
func (c *Client) GetInteractionStream(ctx context.Context, interactionID string, opt *GetInteractionOptions) (*Stream, error) {
	if interactionID == "" {
		return nil, fmt.Errorf("gemini: interaction id is required")
	}

	endpoint, err := url.JoinPath(c.baseURL, c.apiVersion, "interactions", interactionID)
	if err != nil {
		return nil, fmt.Errorf("gemini: invalid base URL: %w", err)
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("gemini: parse endpoint: %w", err)
	}
	q := u.Query()
	q.Set("stream", "true")
	if opt != nil {
		if opt.LastEventID != "" {
			q.Set("last_event_id", opt.LastEventID)
		}
		if opt.IncludeInput {
			q.Set("include_input", "true")
		}
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	sc := sseclient.New(sseclient.WithHTTPClient(c.httpClient))
	rawStream, err := sc.OpenRequest(httpReq)
	if err != nil {
		return nil, err
	}

	return newStream(rawStream), nil
}
