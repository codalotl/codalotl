package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateMessage_Success(t *testing.T) {
	// Start a mock Anthropic API server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		// Headers
		if got := r.Header.Get("content-type"); !strings.HasPrefix(strings.ToLower(got), "application/json") {
			t.Fatalf("expected application/json content-type, got %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("expected x-api-key=test-key, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != defaultVersion {
			t.Fatalf("expected anthropic-version=%s, got %q", defaultVersion, got)
		}

		// Body
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var reqBody MessageRequest
		if err := json.Unmarshal(data, &reqBody); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if reqBody.Model != "claude-3-5-haiku" {
			t.Fatalf("unexpected model: %q", reqBody.Model)
		}
		if reqBody.MaxTokens != 256 {
			t.Fatalf("unexpected max_tokens: %d", reqBody.MaxTokens)
		}
		if len(reqBody.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(reqBody.Messages))
		}
		if reqBody.Messages[0].Role != "user" || reqBody.Messages[0].Content == "" {
			t.Fatalf("first message must be user with non-empty content: %+v", reqBody.Messages[0])
		}
		if reqBody.System != "You are helpful." {
			t.Fatalf("unexpected system: %q", reqBody.System)
		}

		// Respond with a valid message response containing multiple text blocks.
		resp := MessageResponse{
			ID:         "msg_123",
			Model:      "claude-3-5-haiku-2024-07-15",
			Type:       "message",
			StopReason: "end_turn",
			Content: []ContentBlock{
				{Type: "text", Text: "Hello, "},
				{Type: "tool_use", Text: "IGNORED"}, // should be ignored by Text()
				{Type: "text", Text: "world!"},
			},
			Usage: MessageUsage{InputTokens: 12, OutputTokens: 5},
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient("test-key")
	client.BaseURL = srv.URL
	client.HTTP = srv.Client()
	client.Version = "" // ensure default is used and checked in handler

	req := MessageRequest{
		Model:     "claude-3-5-haiku",
		MaxTokens: 256,
		System:    "You are helpful.",
		Messages: []UserMessageRef{
			{Role: "user", Content: "Say hello"},
			{Role: "assistant", Content: "Sure."},
		},
	}

	resp, err := client.CreateMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected non-nil response")
	}
	if got := resp.Text(); got != "Hello, world!" {
		t.Fatalf("unexpected Text: %q", got)
	}
}

func TestCreateMessage_ErrorStructured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(APIError{
			Type: "error",
			Error: struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}{
				Type:    "not_found_error",
				Message: "Model not found",
			},
		})
	}))
	defer srv.Close()

	client := NewClient("test-key")
	client.BaseURL = srv.URL
	client.HTTP = srv.Client()

	_, err := client.CreateMessage(context.Background(), MessageRequest{Model: "bad", MaxTokens: 1, Messages: []UserMessageRef{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "anthropic error (not_found_error): Model not found") {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestCreateMessage_ErrorNonJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	client := NewClient("test-key")
	client.BaseURL = srv.URL
	client.HTTP = srv.Client()

	_, err := client.CreateMessage(context.Background(), MessageRequest{Model: "bad", MaxTokens: 1, Messages: []UserMessageRef{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "anthropic error: status 500: internal server error") {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestCreateMessage_MissingAPIKey(t *testing.T) {
	client := NewClient("")
	client.BaseURL = "http://invalid.local" // should not be used
	_, err := client.CreateMessage(context.Background(), MessageRequest{Model: "claude", MaxTokens: 1, Messages: []UserMessageRef{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatalf("expected error for missing API key")
	}
	if got := err.Error(); got != "missing API key" {
		t.Fatalf("unexpected error: %q", got)
	}
}
