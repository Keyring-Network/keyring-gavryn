package llm

import (
	"os"
	"testing"
)

func TestNewProvider_Local(t *testing.T) {
	cfg := Config{
		Mode: "local",
	}
	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := provider.(LocalProvider); !ok {
		t.Errorf("expected LocalProvider, got %T", provider)
	}
}

func TestNewProvider_Codex(t *testing.T) {
	// Create a temp auth file for testing
	tmpDir := t.TempDir()
	authPath := tmpDir + "/auth.json"
	authData := `{"tokens": {"access_token": "test-token", "refresh_token": "test-refresh", "account_id": "test-account", "id_token": "test-id"}}`
	if err := writeFile(authPath, authData); err != nil {
		t.Fatalf("failed to create auth file: %v", err)
	}

	cfg := Config{
		Mode:          "remote",
		Provider:      "codex",
		Model:         "gpt-5.2-codex",
		CodexAuthPath: authPath,
	}
	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := provider.(*codexProvider); !ok {
		t.Errorf("expected *codexProvider, got %T", provider)
	}
}

func TestNewProvider_OpenAI(t *testing.T) {
	cfg := Config{
		Mode:         "remote",
		Provider:     "openai",
		Model:        "gpt-4",
		OpenAIAPIKey: "test-key",
		BaseURL:      "https://api.openai.com/v1",
	}
	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	openAIProvider, ok := provider.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", provider)
	}
	if openAIProvider.apiKey != "test-key" {
		t.Errorf("expected apiKey to be 'test-key', got %s", openAIProvider.apiKey)
	}
	if openAIProvider.model != "gpt-4" {
		t.Errorf("expected model to be 'gpt-4', got %s", openAIProvider.model)
	}
}

func TestNewProvider_OpencodeZen(t *testing.T) {
	cfg := Config{
		Mode:           "remote",
		Provider:       "opencode-zen",
		Model:          "gpt-4",
		OpenCodeAPIKey: "test-key",
		BaseURL:        "https://opencode.ai/zen/v1",
	}
	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	openCodeProvider, ok := provider.(*OpenCodeProvider)
	if !ok {
		t.Fatalf("expected *OpenCodeProvider, got %T", provider)
	}
	if openCodeProvider.apiKey != "test-key" {
		t.Errorf("expected apiKey to be 'test-key', got %s", openCodeProvider.apiKey)
	}
}

func TestNewProvider_OpenRouter(t *testing.T) {
	cfg := Config{
		Mode:             "remote",
		Provider:         "openrouter",
		Model:            "anthropic/claude-3-opus",
		OpenRouterAPIKey: "router-key",
	}
	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	openAIProvider, ok := provider.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", provider)
	}
	if openAIProvider.apiKey != "router-key" {
		t.Errorf("expected apiKey to be 'router-key', got %s", openAIProvider.apiKey)
	}
	// Should use default OpenRouter base URL
	if openAIProvider.baseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("expected baseURL to be 'https://openrouter.ai/api/v1', got %s", openAIProvider.baseURL)
	}
}

func TestNewProvider_OpenRouter_CustomBaseURL(t *testing.T) {
	cfg := Config{
		Mode:             "remote",
		Provider:         "openrouter",
		Model:            "anthropic/claude-3-opus",
		OpenRouterAPIKey: "router-key",
		BaseURL:          "https://custom.openrouter.ai/api/v1",
	}
	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	openAIProvider, ok := provider.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", provider)
	}
	if openAIProvider.baseURL != "https://custom.openrouter.ai/api/v1" {
		t.Errorf("expected baseURL to be custom URL, got %s", openAIProvider.baseURL)
	}
}

func TestNewProvider_Unsupported(t *testing.T) {
	cfg := Config{
		Mode:     "remote",
		Provider: "unsupported-provider",
	}
	provider, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported provider, got nil")
	}
	if provider != nil {
		t.Errorf("expected nil provider, got %T", provider)
	}
	errUnsupported, ok := err.(ErrUnsupportedProvider)
	if !ok {
		t.Fatalf("expected ErrUnsupportedProvider, got %T", err)
	}
	if errUnsupported.Provider != "unsupported-provider" {
		t.Errorf("expected provider name 'unsupported-provider', got %s", errUnsupported.Provider)
	}
}

func TestDefaultIfEmpty_WithValue(t *testing.T) {
	result := defaultIfEmpty("existing-value", "fallback")
	if result != "existing-value" {
		t.Errorf("expected 'existing-value', got %s", result)
	}
}

func TestDefaultIfEmpty_WithDefault(t *testing.T) {
	result := defaultIfEmpty("", "fallback")
	if result != "fallback" {
		t.Errorf("expected 'fallback', got %s", result)
	}
}

// Helper function to write file for testing
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
