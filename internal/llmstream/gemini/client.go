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
	"strings"

	"github.com/codalotl/codalotl/internal/q/sseclient"
)

const (
	DefaultBaseURL    = "https://generativelanguage.googleapis.com"
	DefaultAPIVersion = "v1beta"
)

type ClientConfig struct {
	APIKey      string
	Backend     Backend
	HTTPClient  *http.Client
	HTTPOptions HTTPOptions

	envVarProvider func() map[string]string
}

type Client struct {
	apiKey      string
	httpClient  *http.Client
	baseURL     string
	apiVersion  string
	baseHeaders http.Header

	Models Models
}

type Models struct {
	client *Client
}

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
			yield(nil, err)
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

type streamGenerateContentRequest struct {
	Contents          []*Content              `json:"contents,omitempty"`
	SystemInstruction *Content                `json:"systemInstruction,omitempty"`
	Tools             []*Tool                 `json:"tools,omitempty"`
	ToolConfig        *ToolConfig             `json:"toolConfig,omitempty"`
	GenerationConfig  *generationConfigFields `json:"generationConfig,omitempty"`
}

type generationConfigFields struct {
	Temperature     *float32        `json:"temperature,omitempty"`
	CandidateCount  int32           `json:"candidateCount,omitempty"`
	MaxOutputTokens int32           `json:"maxOutputTokens,omitempty"`
	StopSequences   []string        `json:"stopSequences,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

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
