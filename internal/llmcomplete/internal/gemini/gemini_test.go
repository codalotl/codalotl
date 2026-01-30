package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateContent_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1beta/models/gemini-2.5-pro:generateContent" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if key := r.URL.Query().Get("key"); key != "test-key" {
			t.Fatalf("expected key=test-key, got %q", key)
		}
		if got := strings.ToLower(r.Header.Get("content-type")); !strings.Contains(got, "application/json") {
			t.Fatalf("unexpected content-type: %q", got)
		}
		if got := strings.ToLower(r.Header.Get("accept")); !strings.Contains(got, "application/json") {
			t.Fatalf("unexpected accept: %q", got)
		}

		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		defer r.Body.Close()

		var body GenerateContentRequest
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if len(body.Contents) != 1 {
			t.Fatalf("expected 1 content message, got %d", len(body.Contents))
		}
		if body.Contents[0].Role != "user" {
			t.Fatalf("unexpected role: %q", body.Contents[0].Role)
		}
		if len(body.Contents[0].Parts) != 1 || body.Contents[0].Parts[0].Text == "" {
			t.Fatalf("unexpected parts: %+v", body.Contents[0].Parts)
		}
		if body.SystemInstruction == nil || len(body.SystemInstruction.Parts) != 1 {
			t.Fatalf("expected system instruction")
		}
		if body.GenerationConfig == nil || body.GenerationConfig.MaxOutputTokens != 256 {
			t.Fatalf("unexpected generation config: %+v", body.GenerationConfig)
		}
		if len(body.SafetySettings) != 1 || body.SafetySettings[0].Category != "HARM_CATEGORY_HATE_SPEECH" {
			t.Fatalf("unexpected safety settings: %+v", body.SafetySettings)
		}

		resp := GenerateContentResponse{
			ModelVersion: "gemini-2.5-pro-latest",
			UsageMetadata: &UsageMetadata{
				PromptTokenCount:     12,
				CandidatesTokenCount: 5,
				TotalTokenCount:      17,
			},
			Candidates: []Candidate{
				{
					Content:      Content{Parts: []Part{{Text: "Hello"}, {Text: " "}, {Text: "world!"}}},
					FinishReason: "STOP",
				},
				{
					Content: Content{Parts: []Part{{Text: "Ignored candidate"}}},
				},
			},
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient("test-key")
	client.BaseURL = srv.URL
	client.HTTP = srv.Client()

	req := GenerateContentRequest{
		Model: "gemini-2.5-pro",
		Contents: []Content{
			{Role: "user", Parts: []Part{{Text: "Say hello"}}},
		},
		SystemInstruction: &Content{Parts: []Part{{Text: "You are helpful."}}},
		GenerationConfig:  &GenerationConfig{MaxOutputTokens: 256, Temperature: 0.7},
		SafetySettings: []SafetySetting{
			{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "BLOCK_MEDIUM_AND_ABOVE"},
		},
	}

	resp, err := client.GenerateContent(context.Background(), req)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected non-nil response")
	}
	if got := resp.Text(); got != "Hello world!" {
		t.Fatalf("unexpected Text: %q", got)
	}
}

func TestGenerateContent_ErrorStructured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(APIError{
			Error: struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			}{Code: 400, Message: "Model not available", Status: "INVALID_ARGUMENT"},
		})
	}))
	defer srv.Close()

	client := NewClient("test-key")
	client.BaseURL = srv.URL
	client.HTTP = srv.Client()

	_, err := client.GenerateContent(context.Background(), GenerateContentRequest{
		Model:    "gemini-2.5-pro",
		Contents: []Content{{Parts: []Part{{Text: "hi"}}}},
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "gemini error (INVALID_ARGUMENT): Model not available") {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestGenerateContent_ErrorNonJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server exploded"))
	}))
	defer srv.Close()

	client := NewClient("test-key")
	client.BaseURL = srv.URL
	client.HTTP = srv.Client()

	_, err := client.GenerateContent(context.Background(), GenerateContentRequest{
		Model:    "gemini-2.5-pro",
		Contents: []Content{{Parts: []Part{{Text: "hi"}}}},
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "gemini error: status 500: server exploded") {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestGenerateContent_MissingAPIKey(t *testing.T) {
	client := NewClient("")
	_, err := client.GenerateContent(context.Background(), GenerateContentRequest{
		Model:    "gemini-2.5-flash",
		Contents: []Content{{Parts: []Part{{Text: "hi"}}}},
	})
	if err == nil {
		t.Fatalf("expected error for missing API key")
	}
	if got := err.Error(); got != "missing API key" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestGenerateContent_MissingModel(t *testing.T) {
	client := NewClient("key")
	_, err := client.GenerateContent(context.Background(), GenerateContentRequest{
		Contents: []Content{{Parts: []Part{{Text: "hi"}}}},
	})
	if err == nil {
		t.Fatalf("expected error for missing model")
	}
	if got := err.Error(); got != "missing model" {
		t.Fatalf("unexpected error: %q", got)
	}
}
