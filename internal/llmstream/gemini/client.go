package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/q/sseclient"
)

// DefaultBaseURL is the default unversioned Gemini API root.
const DefaultBaseURL = "https://generativelanguage.googleapis.com"

// DefaultAPIVersion is the fixed REST API version appended by this client.
const DefaultAPIVersion = "v1beta"

// ClientConfig configures a Client.
type ClientConfig struct {
	APIKey     string       // APIKey is the Gemini API key. If empty, NewClient reads GOOGLE_API_KEY first, then GEMINI_API_KEY.
	Backend    Backend      // Backend selects the backend implementation. Only BackendGeminiAPI is supported.
	HTTPClient *http.Client // HTTPClient is used for outgoing HTTP requests. Nil uses http.DefaultClient.

	// HTTPOptions supplies client-wide HTTP defaults. BaseURL must be an unversioned API root such as https://host or https://host/custom-prefix, not a versioned root
	// such as https://host/v1beta. This package appends /v1beta/... itself.
	HTTPOptions HTTPOptions

	// Environment variable provider supplies values used for API key and base URL defaults. Nil uses the process environment.
	envVarProvider func() map[string]string
}

// Client is a Gemini streaming client.
type Client struct {
	apiKey      string       // API key sent with each Gemini request.
	httpClient  *http.Client // HTTP client used for outgoing requests.
	baseURL     string       // Unversioned API root used to build request URLs.
	apiVersion  string       // Gemini API version appended to request URLs.
	baseHeaders http.Header  // Client-wide headers copied into outgoing requests.
	Models      Models       // Models exposes model-scoped operations for this client.
}

// Models exposes model-scoped operations.
type Models struct {
	client *Client // Client used to send model-scoped requests.
}

// APIError is a Gemini API error parsed from an HTTP failure response.
type APIError struct {
	StatusCode int    // StatusCode is the numeric HTTP status code or API error code.
	Status     string // Status is the Gemini canonical status or HTTP status text.
	Message    string // Message is the human-readable error message returned by Gemini.
	Reason     string // Reason is the machine-readable error reason from structured details.
	RetryAfter string // RetryAfter is the retry delay reported by Gemini, when present.
}

// Error returns a human-readable Gemini API error string. For a nil receiver, it returns "<nil>".
func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}

	label := "API error"
	if e.IsRateLimit() {
		label = "rate limit exceeded"
	}

	statusParts := make([]string, 0, 2)
	if e.StatusCode > 0 {
		statusParts = append(statusParts, strconv.Itoa(e.StatusCode))
	}
	if e.Status != "" {
		statusParts = append(statusParts, e.Status)
	}

	var b strings.Builder
	b.WriteString("gemini ")
	b.WriteString(label)
	if len(statusParts) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(statusParts, " "))
		b.WriteString(")")
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if e.RetryAfter != "" {
		b.WriteString(" (retry after ")
		b.WriteString(e.RetryAfter)
		b.WriteString(")")
	}
	return b.String()
}

// Retryable reports whether e represents a 429 or 5xx response that may be retried.
func (e *APIError) Retryable() bool {
	if e == nil {
		return false
	}
	return e.StatusCode == http.StatusTooManyRequests || (e.StatusCode >= 500 && e.StatusCode <= 599)
}

// IsRateLimit reports whether e represents a Gemini rate-limit response.
func (e *APIError) IsRateLimit() bool {
	if e == nil {
		return false
	}
	return e.StatusCode == http.StatusTooManyRequests || strings.EqualFold(e.Status, "RESOURCE_EXHAUSTED") || strings.EqualFold(e.Reason, "RATE_LIMIT_EXCEEDED")
}

// NewClient constructs a Client.
//
// If cfg.HTTPOptions.BaseURL is empty, NewClient uses GOOGLE_GEMINI_BASE_URL when set, otherwise DefaultBaseURL. The client always appends /v1beta/... itself when
// constructing Gemini API URLs.
func NewClient(_ context.Context, cfg *ClientConfig) (*Client, error) {
	if cfg == nil {
		cfg = &ClientConfig{}
	}
	if cfg.envVarProvider == nil {
		cfg.envVarProvider = defaultEnvVarProvider
	}

	if cfg.Backend == BackendUnspecified {
		cfg.Backend = BackendGeminiAPI
	}
	if cfg.Backend != BackendGeminiAPI {
		return nil, fmt.Errorf("gemini: unsupported backend %q", cfg.Backend)
	}

	if cfg.APIKey == "" {
		cfg.APIKey = apiKeyFromEnv(cfg.envVarProvider())
	}
	if cfg.APIKey == "" {
		return nil, errors.New("gemini: API key is required")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	baseURL := cfg.HTTPOptions.BaseURL
	if baseURL == "" {
		baseURL = cfg.envVarProvider()["GOOGLE_GEMINI_BASE_URL"]
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	client := &Client{
		apiKey:      cfg.APIKey,
		httpClient:  httpClient,
		baseURL:     baseURL,
		apiVersion:  DefaultAPIVersion,
		baseHeaders: cloneHeaders(cfg.HTTPOptions.Headers),
	}
	client.Models = Models{client: client}
	return client, nil
}

// GenerateContentStream sends streamGenerateContent?alt=sse and yields one GenerateContentResponse per decoded SSE data event.
//
// The client does not accumulate prior chunks. If callers want a response-so-far view, they must accumulate text, tool, and thought state themselves across yielded
// events.
//
// Open errors, non-2xx responses, mid-stream read failures, and JSON decode failures are yielded as (nil, err). After yielding an error, iteration stops. A clean
// end-of-stream returns without yielding an error.
func (m Models) GenerateContentStream(ctx context.Context, model string, contents []*Content, config *GenerateContentConfig) iter.Seq2[*GenerateContentResponse, error] {
	return func(yield func(*GenerateContentResponse, error) bool) {
		client := m.client
		if client == nil {
			yield(nil, errors.New("gemini: nil client"))
			return
		}

		endpoint, err := client.streamGenerateContentURL(model, config)
		if err != nil {
			yield(nil, err)
			return
		}

		bodyBytes, err := json.Marshal(buildStreamRequest(contents, config))
		if err != nil {
			yield(nil, fmt.Errorf("gemini: marshal request: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			yield(nil, fmt.Errorf("gemini: create request: %w", err))
			return
		}
		httpReq.Header = cloneHeaders(client.baseHeaders)
		mergeHeaders(httpReq.Header, headersFromConfig(config))
		httpReq.Header.Set("content-type", "application/json")
		httpReq.Header.Set("x-goog-api-key", client.apiKey)

		streamClient := sseclient.New(sseclient.WithHTTPClient(client.httpClient))
		stream, err := streamClient.OpenRequest(httpReq)
		if err != nil {
			yield(nil, normalizeOpenStreamError(err))
			return
		}
		defer stream.Close()

		for {
			event, err := stream.RecvContext(ctx)
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				yield(nil, fmt.Errorf("gemini: receive stream event: %w", err))
				return
			}
			if strings.TrimSpace(event.Data) == "" {
				continue
			}

			var resp GenerateContentResponse
			if err := json.Unmarshal([]byte(event.Data), &resp); err != nil {
				yield(nil, fmt.Errorf("gemini: decode stream event: %w", err))
				return
			}
			if !yield(&resp, nil) {
				return
			}
		}
	}
}

// The streamGenerateContentRequest type is the JSON body sent to the Gemini streamGenerateContent endpoint.
type streamGenerateContentRequest struct {
	Contents          []*Content              `json:"contents,omitempty"`          // Contents are the conversation messages sent to the model.
	SystemInstruction *Content                `json:"systemInstruction,omitempty"` // SystemInstruction is the optional system-level instruction.
	Tools             []*Tool                 `json:"tools,omitempty"`             // Tools are the function tools made available to the model.
	ToolConfig        *ToolConfig             `json:"toolConfig,omitempty"`        // ToolConfig controls how the model may use tools.
	GenerationConfig  *generationConfigFields `json:"generationConfig,omitempty"`  // GenerationConfig contains supported generation options.
}

// The generationConfigFields type is the supported subset of Gemini generationConfig.
type generationConfigFields struct {
	Temperature     *float32        `json:"temperature,omitempty"`     // Temperature controls sampling randomness when set.
	CandidateCount  int32           `json:"candidateCount,omitempty"`  // CandidateCount requests the number of response candidates.
	MaxOutputTokens int32           `json:"maxOutputTokens,omitempty"` // MaxOutputTokens limits generated output tokens.
	StopSequences   []string        `json:"stopSequences,omitempty"`   // StopSequences stop generation when any sequence is produced.
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`  // ThinkingConfig configures Gemini thinking options.
}

// The buildStreamRequest function converts public stream inputs into the Gemini REST request body.
func buildStreamRequest(contents []*Content, config *GenerateContentConfig) *streamGenerateContentRequest {
	req := &streamGenerateContentRequest{
		Contents: contents,
	}
	if config == nil {
		return req
	}

	req.SystemInstruction = config.SystemInstruction
	req.Tools = config.Tools
	req.ToolConfig = config.ToolConfig

	generation := &generationConfigFields{
		Temperature:     config.Temperature,
		CandidateCount:  config.CandidateCount,
		MaxOutputTokens: config.MaxOutputTokens,
		StopSequences:   config.StopSequences,
		ThinkingConfig:  config.ThinkingConfig,
	}
	if generation.Temperature != nil || generation.CandidateCount != 0 || generation.MaxOutputTokens != 0 || len(generation.StopSequences) > 0 || generation.ThinkingConfig != nil {
		req.GenerationConfig = generation
	}

	return req
}

// The streamGenerateContentURL method builds the SSE streamGenerateContent endpoint URL for model.
func (c *Client) streamGenerateContentURL(model string, config *GenerateContentConfig) (string, error) {
	baseURL := c.baseURL
	if config != nil && config.HTTPOptions != nil && config.HTTPOptions.BaseURL != "" {
		baseURL = config.HTTPOptions.BaseURL
	}

	modelPath, err := formatModelPath(model)
	if err != nil {
		return "", err
	}

	endpoint, err := url.JoinPath(baseURL, c.apiVersion, modelPath+":streamGenerateContent")
	if err != nil {
		return "", fmt.Errorf("gemini: invalid base URL: %w", err)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("gemini: invalid endpoint URL: %w", err)
	}
	query := parsed.Query()
	query.Set("alt", "sse")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func formatModelPath(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", errors.New("gemini: model is required")
	}
	if strings.Contains(model, "?") || strings.Contains(model, "&") || strings.Contains(model, "..") {
		return "", errors.New("gemini: invalid model")
	}
	if strings.HasPrefix(model, "models/") || strings.HasPrefix(model, "tunedModels/") {
		return model, nil
	}
	return "models/" + model, nil
}

func apiKeyFromEnv(env map[string]string) string {
	if env["GOOGLE_API_KEY"] != "" {
		return env["GOOGLE_API_KEY"]
	}
	return env["GEMINI_API_KEY"]
}

// The apiErrorEnvelope type is the outer JSON wrapper used by Gemini error responses.
type apiErrorEnvelope struct {
	Error apiErrorPayload `json:"error"` // Error is the structured Gemini error payload.
}

// The apiErrorPayload type is the structured Gemini error payload.
type apiErrorPayload struct {
	Code    int              `json:"code"`    // Code is the numeric error code.
	Message string           `json:"message"` // Message is the human-readable error message.
	Status  string           `json:"status"`  // Status is the Gemini canonical error status.
	Details []apiErrorDetail `json:"details"` // Details are structured error metadata entries.
}

// The apiErrorDetail type is one structured detail entry from a Gemini error response.
type apiErrorDetail struct {
	Type       string `json:"@type"`      // Type is the structured detail type URL.
	Reason     string `json:"reason"`     // Reason is the machine-readable error reason.
	RetryDelay string `json:"retryDelay"` // RetryDelay is the retry delay reported by Gemini.
}

// The normalizeOpenStreamError function converts unexpected HTTP status stream-open failures into APIError values.
func normalizeOpenStreamError(err error) error {
	var openErr *sseclient.OpenError
	if !errors.As(err, &openErr) || !errors.Is(err, sseclient.ErrUnexpectedStatus) {
		return err
	}

	apiErr := &APIError{}
	if openErr.Response != nil {
		apiErr.StatusCode = openErr.Response.StatusCode
		apiErr.Status = http.StatusText(openErr.Response.StatusCode)
		apiErr.RetryAfter = formatRetryAfter(openErr.Response.Header.Get("Retry-After"))
	}

	body := openErr.ResponseBody
	if len(body) > 0 {
		var envelope apiErrorEnvelope
		if json.Unmarshal(body, &envelope) == nil && (envelope.Error.Message != "" || envelope.Error.Status != "" || envelope.Error.Code != 0) {
			if envelope.Error.Code != 0 {
				apiErr.StatusCode = envelope.Error.Code
			}
			if envelope.Error.Status != "" {
				apiErr.Status = envelope.Error.Status
			}
			apiErr.Message = envelope.Error.Message
			for _, detail := range envelope.Error.Details {
				if apiErr.Reason == "" && detail.Reason != "" {
					apiErr.Reason = detail.Reason
				}
				if apiErr.RetryAfter == "" && detail.RetryDelay != "" {
					apiErr.RetryAfter = detail.RetryDelay
				}
			}
		} else {
			apiErr.Message = strings.TrimSpace(string(body))
		}
	}

	if apiErr.Message == "" && openErr.Response != nil {
		apiErr.Message = openErr.Response.Status
	}

	return apiErr
}

func formatRetryAfter(value string) string {
	if value == "" {
		return ""
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return (time.Duration(seconds) * time.Second).String()
	}
	if when, err := http.ParseTime(value); err == nil {
		delay := time.Until(when).Round(time.Second)
		if delay > 0 {
			return delay.String()
		}
		return ""
	}
	return value
}

func defaultEnvVarProvider() map[string]string {
	out := make(map[string]string)
	for _, key := range []string{"GOOGLE_API_KEY", "GEMINI_API_KEY", "GOOGLE_GEMINI_BASE_URL"} {
		if value, ok := os.LookupEnv(key); ok {
			out[key] = value
		}
	}
	return out
}

func headersFromConfig(config *GenerateContentConfig) http.Header {
	if config == nil || config.HTTPOptions == nil {
		return nil
	}
	return config.HTTPOptions.Headers
}

func cloneHeaders(in http.Header) http.Header {
	if in == nil {
		return make(http.Header)
	}
	return in.Clone()
}

func mergeHeaders(dst, src http.Header) {
	for key, values := range src {
		dst.Del(key)
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
