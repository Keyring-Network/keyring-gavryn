package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewOpenAIProvider_Success(t *testing.T) {
	cfg := OpenAIConfig{
		APIKey:  "test-api-key",
		Model:   "gpt-4",
		BaseURL: "https://api.openai.com/v1",
	}
	provider := NewOpenAIProvider(cfg)
	if provider == nil {
		t.Fatal("expected provider to not be nil")
	}
	if provider.apiKey != "test-api-key" {
		t.Errorf("expected apiKey to be 'test-api-key', got %s", provider.apiKey)
	}
	if provider.model != "gpt-4" {
		t.Errorf("expected model to be 'gpt-4', got %s", provider.model)
	}
	if provider.baseURL != "https://api.openai.com/v1" {
		t.Errorf("expected baseURL to be 'https://api.openai.com/v1', got %s", provider.baseURL)
	}
	if provider.client == nil {
		t.Error("expected client to not be nil")
	}
}

func TestNewOpenAIProvider_DefaultBaseURL(t *testing.T) {
	cfg := OpenAIConfig{
		APIKey: "test-api-key",
		Model:  "gpt-4",
	}
	provider := NewOpenAIProvider(cfg)
	if provider.baseURL != "https://api.openai.com/v1" {
		t.Errorf("expected default baseURL to be 'https://api.openai.com/v1', got %s", provider.baseURL)
	}
}

func TestNewOpenAIProvider_TrimTrailingSlash(t *testing.T) {
	cfg := OpenAIConfig{
		APIKey:  "test-api-key",
		Model:   "gpt-4",
		BaseURL: "https://api.openai.com/v1/",
	}
	provider := NewOpenAIProvider(cfg)
	if provider.baseURL != "https://api.openai.com/v1" {
		t.Errorf("expected baseURL to have trailing slash trimmed, got %s", provider.baseURL)
	}
}

func TestNewOpenAIProvider_MissingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called when API key is missing")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := OpenAIConfig{
		APIKey:  "",
		Model:   "gpt-4",
		BaseURL: server.URL,
	}
	provider := NewOpenAIProvider(cfg)

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for missing API key, got nil")
	}
	if err.Error() != "missing API key for remote provider" {
		t.Errorf("expected specific error message, got: %s", err.Error())
	}
}

func TestNewOpenAIProvider_MissingModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called when model is missing")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := OpenAIConfig{
		APIKey:  "test-api-key",
		Model:   "",
		BaseURL: server.URL,
	}
	provider := NewOpenAIProvider(cfg)

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
	}
	if err.Error() != "missing model for remote provider" {
		t.Errorf("expected specific error message, got: %s", err.Error())
	}
}

func TestOpenAIProvider_Generate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path '/chat/completions', got %s", r.URL.Path)
		}

		// Verify headers
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-api-key" {
			t.Errorf("expected Authorization header 'Bearer test-api-key', got %s", authHeader)
		}
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %s", contentType)
		}

		// Verify request body
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody["model"] != "gpt-4" {
			t.Errorf("expected model 'gpt-4', got %v", reqBody["model"])
		}

		// Send successful response
		response := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "Hello! How can I help you today?",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := OpenAIConfig{
		APIKey:  "test-api-key",
		Model:   "gpt-4",
		BaseURL: server.URL,
	}
	provider := NewOpenAIProvider(cfg)

	messages := []Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
	}
	result, err := provider.Generate(context.Background(), messages)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "Hello! How can I help you today?" {
		t.Errorf("expected 'Hello! How can I help you today?', got %s", result)
	}
}

func TestOpenAIProvider_Generate_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid API key"}`))
	}))
	defer server.Close()

	cfg := OpenAIConfig{
		APIKey:  "invalid-key",
		Model:   "gpt-4",
		BaseURL: server.URL,
	}
	provider := NewOpenAIProvider(cfg)

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for HTTP error, got nil")
	}
	if !errors.Is(err, errors.New("LLM request failed: 401 Unauthorized")) && err.Error() != "LLM request failed: 401 Unauthorized" {
		t.Errorf("expected HTTP error message, got: %s", err.Error())
	}
}

func TestOpenAIProvider_Generate_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	cfg := OpenAIConfig{
		APIKey:  "test-api-key",
		Model:   "gpt-4",
		BaseURL: server.URL,
	}
	provider := NewOpenAIProvider(cfg)

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestOpenAIProvider_Generate_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"choices": []map[string]any{},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := OpenAIConfig{
		APIKey:  "test-api-key",
		Model:   "gpt-4",
		BaseURL: server.URL,
	}
	provider := NewOpenAIProvider(cfg)

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for empty choices, got nil")
	}
	if err.Error() != "LLM response had no choices" {
		t.Errorf("expected 'LLM response had no choices', got: %s", err.Error())
	}
}

func TestOpenAIProvider_Generate_EmptyContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "   ",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := OpenAIConfig{
		APIKey:  "test-api-key",
		Model:   "gpt-4",
		BaseURL: server.URL,
	}
	provider := NewOpenAIProvider(cfg)

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
	if err.Error() != "LLM response was empty" {
		t.Errorf("expected 'LLM response was empty', got: %s", err.Error())
	}
}

func TestOpenAIProvider_Generate_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		select {
		case <-r.Context().Done():
			return
		}
	}))
	defer server.Close()

	cfg := OpenAIConfig{
		APIKey:  "test-api-key",
		Model:   "gpt-4",
		BaseURL: server.URL,
	}
	provider := NewOpenAIProvider(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(ctx, messages)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestOpenAIProvider_Generate_RequestCreationError(t *testing.T) {
	cfg := OpenAIConfig{
		APIKey:  "test-api-key",
		Model:   "gpt-4",
		BaseURL: "http://[invalid-url", // Invalid URL format
	}
	provider := NewOpenAIProvider(cfg)

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestOpenAIProvider_Generate_NetworkError(t *testing.T) {
	cfg := OpenAIConfig{
		APIKey:  "test-api-key",
		Model:   "gpt-4",
		BaseURL: "http://localhost:1", // Port 1 is typically not accessible
	}
	provider := NewOpenAIProvider(cfg)

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for network failure, got nil")
	}
}
