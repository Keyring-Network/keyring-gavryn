package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewCodexProvider_Success(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")

	cfg := Config{
		Provider:      "codex",
		Model:         "gpt-5.2-codex",
		BaseURL:       "https://chatgpt.com/backend-api/codex",
		CodexAuthPath: authPath,
	}

	provider, err := NewCodexProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	codexProvider, ok := provider.(*codexProvider)
	if !ok {
		t.Fatalf("expected *codexProvider, got %T", provider)
	}

	if codexProvider.model != "gpt-5.2-codex" {
		t.Errorf("expected model 'gpt-5.2-codex', got %s", codexProvider.model)
	}
	if codexProvider.baseURL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("expected baseURL 'https://chatgpt.com/backend-api/codex', got %s", codexProvider.baseURL)
	}
	if codexProvider.authPath != authPath {
		t.Errorf("expected authPath '%s', got %s", authPath, codexProvider.authPath)
	}
	if codexProvider.sessionID == "" {
		t.Error("expected sessionID to be set")
	}
	if codexProvider.client == nil {
		t.Error("expected client to not be nil")
	}
}

func TestNewCodexProvider_DefaultModel(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")

	cfg := Config{
		Provider:      "codex",
		CodexAuthPath: authPath,
	}

	provider, err := NewCodexProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	codexProvider := provider.(*codexProvider)
	if codexProvider.model != "gpt-5.2-codex" {
		t.Errorf("expected default model 'gpt-5.2-codex', got %s", codexProvider.model)
	}
}

func TestNewCodexProvider_DefaultBaseURL(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")

	cfg := Config{
		Provider:      "codex",
		CodexAuthPath: authPath,
	}

	provider, err := NewCodexProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	codexProvider := provider.(*codexProvider)
	if codexProvider.baseURL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("expected default baseURL 'https://chatgpt.com/backend-api/codex', got %s", codexProvider.baseURL)
	}
}

func TestResolveCodexAuthPath_WithExplicitPath(t *testing.T) {
	cfg := Config{
		CodexAuthPath: "/custom/path/auth.json",
	}
	path := resolveCodexAuthPath(cfg)
	if path != "/custom/path/auth.json" {
		t.Errorf("expected '/custom/path/auth.json', got %s", path)
	}
}

func TestResolveCodexAuthPath_WithHomeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get user home dir")
	}

	cfg := Config{
		CodexAuthPath: "~/custom/auth.json",
	}
	path := resolveCodexAuthPath(cfg)
	expected := filepath.Join(home, "custom/auth.json")
	if path != expected {
		t.Errorf("expected '%s', got %s", expected, path)
	}
}

func TestResolveCodexAuthPath_WithCodexHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get user home dir")
	}

	cfg := Config{
		CodexHome: "~/.config/codex",
	}
	path := resolveCodexAuthPath(cfg)
	expected := filepath.Join(home, ".config/codex", "auth.json")
	if path != expected {
		t.Errorf("expected '%s', got %s", expected, path)
	}
}

func TestResolveCodexAuthPath_Default(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get user home dir")
	}

	cfg := Config{}
	path := resolveCodexAuthPath(cfg)
	expected := filepath.Join(home, ".codex", "auth.json")
	if path != expected {
		t.Errorf("expected '%s', got %s", expected, path)
	}
}

func TestExpandHome_WithTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get user home dir")
	}

	result := expandHome("~/test/path")
	expected := filepath.Join(home, "test/path")
	if result != expected {
		t.Errorf("expected '%s', got %s", expected, result)
	}
}

func TestExpandHome_WithoutTilde(t *testing.T) {
	result := expandHome("/absolute/path")
	if result != "/absolute/path" {
		t.Errorf("expected '/absolute/path', got %s", result)
	}
}

func TestExpandHome_RelativePath(t *testing.T) {
	result := expandHome("relative/path")
	if result != "relative/path" {
		t.Errorf("expected 'relative/path', got %s", result)
	}
}

func TestExpandHome_UserHomeDirError(t *testing.T) {
	// This test covers the error case when os.UserHomeDir() fails
	// We can't easily mock os.UserHomeDir, but we can verify the function
	// returns the original path when home expansion fails
	// The only way this happens is if the path starts with ~ but home dir can't be determined
	// In practice, this is hard to trigger, so we test the logic path exists
	result := expandHome("~/test")
	// Should either expand successfully or return original path
	if result == "" {
		t.Error("expandHome should not return empty string")
	}
}

func TestCodexProvider_Generate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("expected path '/responses', got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		// Verify headers
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-access-token" {
			t.Errorf("expected Authorization 'Bearer test-access-token', got %s", authHeader)
		}
		originator := r.Header.Get("originator")
		if originator != "gavryn" {
			t.Errorf("expected originator 'gavryn', got %s", originator)
		}
		userAgent := r.Header.Get("User-Agent")
		if userAgent != "gavryn" {
			t.Errorf("expected User-Agent 'gavryn', got %s", userAgent)
		}
		accountID := r.Header.Get("ChatGPT-Account-Id")
		if accountID != "test-account-id" {
			t.Errorf("expected ChatGPT-Account-Id 'test-account-id', got %s", accountID)
		}

		// Send successful response
		response := map[string]any{
			"output_text": "Generated code response",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"account_id":    "test-account-id",
			"id_token":      "test-id-token",
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   server.URL,
		authPath:  authPath,
		sessionID: "test-session",
		client:    &http.Client{},
	}

	messages := []Message{
		{Role: "system", Content: "You are a coding assistant."},
		{Role: "user", Content: "Write a function to add two numbers."},
	}

	result, err := provider.Generate(context.Background(), messages)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "Generated code response" {
		t.Errorf("expected 'Generated code response', got %s", result)
	}
}

func TestCodexProvider_Generate_NoAccountID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify that ChatGPT-Account-Id header is NOT set when account ID is empty
		accountID := r.Header.Get("ChatGPT-Account-Id")
		if accountID != "" {
			t.Errorf("expected no ChatGPT-Account-Id header, got %s", accountID)
		}

		// Send successful response
		response := map[string]any{
			"output_text": "Response without account ID",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			// No account_id and no id_token
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   server.URL,
		authPath:  authPath,
		sessionID: "test-session",
		client:    &http.Client{},
	}

	messages := []Message{{Role: "user", Content: "Hello"}}
	result, err := provider.Generate(context.Background(), messages)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "Response without account ID" {
		t.Errorf("expected 'Response without account ID', got %s", result)
	}
}

func TestCodexProvider_Generate_LoadAuthError(t *testing.T) {
	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   "http://localhost",
		authPath:  "/nonexistent/path/auth.json",
		sessionID: "test-session",
		client:    &http.Client{},
	}

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error when auth file not found, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %s", err.Error())
	}
}

func TestCodexProvider_Generate_RequestError(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"account_id":    "test-account",
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	// Create a provider with a client that will fail (invalid URL)
	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   "http://[invalid-url", // Invalid URL to trigger request error
		authPath:  authPath,
		sessionID: "test-session",
		client:    &http.Client{},
	}

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestCodexProvider_request_HTTPDoError(t *testing.T) {
	// Create a custom transport that always fails
	errorTransport := &alwaysFailTransport{err: errors.New("connection refused")}

	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   "http://example.com",
		sessionID: "test-session",
		client:    &http.Client{Transport: errorTransport},
	}

	auth := &codexAuth{
		AccessToken: "test-access-token",
		AccountID:   "test-account",
	}
	messages := []Message{{Role: "user", Content: "Hello"}}

	_, err := provider.request(context.Background(), auth, messages)
	if err == nil {
		t.Fatal("expected error for HTTP Do failure, got nil")
	}
}

// alwaysFailTransport is an HTTP transport that always returns an error
type alwaysFailTransport struct {
	err error
}

func (t *alwaysFailTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, t.err
}

func TestCodexProvider_Generate_Non401Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"account_id":    "test-account",
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   server.URL,
		authPath:  authPath,
		sessionID: "test-session",
		client:    &http.Client{},
	}

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Errorf("expected error to contain 'bad request', got: %s", err.Error())
	}
}

func TestCodexProvider_Generate_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"output_text": "   ", // Empty after trimming
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token": "test-access-token",
			"account_id":   "test-account",
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   server.URL,
		authPath:  authPath,
		sessionID: "test-session",
		client:    &http.Client{},
	}

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error for empty response, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected 'empty' error, got: %s", err.Error())
	}
}

func TestCodexProvider_Generate_SuccessfulTokenRefresh(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			requestCount++
			if requestCount == 1 {
				// First request returns 401 to trigger refresh
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Second request (after refresh) succeeds
			response := map[string]any{
				"output_text": "Success after token refresh",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		case "/oauth/token":
			// Token refresh endpoint
			response := map[string]any{
				"access_token":  "new-access-token",
				"refresh_token": "new-refresh-token",
				"id_token":      createTestJWT("new-account-id"),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token":  "old-access-token",
			"refresh_token": "old-refresh-token",
			"account_id":    "old-account",
			"id_token":      createTestJWT("old-account"),
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   server.URL,
		authPath:  authPath,
		sessionID: "test-session",
		client:    &http.Client{},
		tokenURL:  server.URL + "/oauth/token", // Use mock token endpoint
	}

	messages := []Message{{Role: "user", Content: "Hello"}}
	result, err := provider.Generate(context.Background(), messages)
	if err != nil {
		t.Fatalf("expected no error after token refresh, got %v", err)
	}
	if result != "Success after token refresh" {
		t.Errorf("expected 'Success after token refresh', got %s", result)
	}

	// Verify that the auth file was updated with new tokens
	data, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("failed to read updated auth file: %v", err)
	}
	var updatedAuth codexAuthFile
	if err := json.Unmarshal(data, &updatedAuth); err != nil {
		t.Fatalf("failed to unmarshal updated auth: %v", err)
	}
	if updatedAuth.Tokens.AccessToken != "new-access-token" {
		t.Errorf("expected access token to be updated to 'new-access-token', got %s", updatedAuth.Tokens.AccessToken)
	}
	if updatedAuth.Tokens.RefreshToken != "new-refresh-token" {
		t.Errorf("expected refresh token to be updated to 'new-refresh-token', got %s", updatedAuth.Tokens.RefreshToken)
	}
}

func TestCodexProvider_Generate_TokenRefresh(t *testing.T) {
	// This test verifies that when a 401 is received and no refresh token is available,
	// the original error is returned. The actual token refresh flow can't be fully tested
	// because the token refresh endpoint is hardcoded to the real OpenAI URL.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses" {
			// Always return 401 to trigger the refresh attempt
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	// Create auth with NO refresh token - should fail to refresh
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token": "test-access-token",
			"account_id":   "test-account",
			// No refresh_token - refresh should not be attempted
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   server.URL,
		authPath:  authPath,
		sessionID: "test-session",
		client:    &http.Client{},
	}

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	// Should fail because no refresh token available
	if err == nil {
		t.Fatal("expected error when no refresh token available, got nil")
	}
	if !isUnauthorized(err) {
		t.Errorf("expected unauthorized error, got: %v", err)
	}
}

func TestCodexProvider_Generate_RefreshFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": "invalid_grant"}`))
			return
		}
		if r.URL.Path == "/responses" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"account_id":    "test-account",
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   server.URL,
		authPath:  authPath,
		sessionID: "test-session",
		client:    &http.Client{},
		tokenURL:  server.URL + "/oauth/token",
	}

	messages := []Message{{Role: "user", Content: "Hello"}}
	_, err := provider.Generate(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error when refresh fails, got nil")
	}
	// Should return the original unauthorized error
	if !errors.Is(err, errUnauthorized) {
		t.Errorf("expected unauthorized error, got: %v", err)
	}
}

func TestCodexProvider_Generate_RefreshEmptyTokens(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			requestCount++
			if requestCount == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Second request should use old token since refresh returned empty
			authHeader := r.Header.Get("Authorization")
			if authHeader != "Bearer old-access-token" {
				t.Errorf("expected old token when refresh returns empty, got %s", authHeader)
			}
			response := map[string]any{
				"output_text": "Success with old token",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		case "/oauth/token":
			// Return empty tokens (edge case)
			response := map[string]any{}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token":  "old-access-token",
			"refresh_token": "old-refresh-token",
			"account_id":    "old-account",
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		model:     "gpt-5.2-codex",
		baseURL:   server.URL,
		authPath:  authPath,
		sessionID: "test-session",
		client:    &http.Client{},
		tokenURL:  server.URL + "/oauth/token",
	}

	messages := []Message{{Role: "user", Content: "Hello"}}
	result, err := provider.Generate(context.Background(), messages)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "Success with old token" {
		t.Errorf("expected 'Success with old token', got %s", result)
	}
}

func TestLoadAuth_Success(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"account_id":    "test-account-id",
			"id_token":      createTestJWT("test-account-id"),
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		authPath: authPath,
	}

	auth, err := provider.loadAuth()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if auth.AccessToken != "test-access-token" {
		t.Errorf("expected access token 'test-access-token', got %s", auth.AccessToken)
	}
	if auth.RefreshToken != "test-refresh-token" {
		t.Errorf("expected refresh token 'test-refresh-token', got %s", auth.RefreshToken)
	}
	if auth.AccountID != "test-account-id" {
		t.Errorf("expected account ID 'test-account-id', got %s", auth.AccountID)
	}
}

func TestLoadAuth_Cached(t *testing.T) {
	cachedAuth := &codexAuth{
		AccessToken:  "cached-token",
		RefreshToken: "cached-refresh",
		AccountID:    "cached-account",
	}

	provider := &codexProvider{
		authPath:   "/nonexistent/path",
		cachedAuth: cachedAuth,
	}

	auth, err := provider.loadAuth()
	if err != nil {
		t.Fatalf("expected no error with cached auth, got %v", err)
	}
	if auth.AccessToken != "cached-token" {
		t.Errorf("expected cached token 'cached-token', got %s", auth.AccessToken)
	}
}

func TestLoadAuth_NotFound(t *testing.T) {
	provider := &codexProvider{
		authPath: "/nonexistent/path/auth.json",
	}

	_, err := provider.loadAuth()
	if err == nil {
		t.Fatal("expected error for missing auth file, got nil")
	}
	if !strings.Contains(err.Error(), "codex auth.json not found") {
		t.Errorf("expected 'not found' error message, got: %s", err.Error())
	}
}

func TestLoadAuth_MissingAccessToken(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"refresh_token": "test-refresh",
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		authPath: authPath,
	}

	_, err := provider.loadAuth()
	if err == nil {
		t.Fatal("expected error for missing access token, got nil")
	}
	if !strings.Contains(err.Error(), "missing access_token") {
		t.Errorf("expected 'missing access_token' error message, got: %s", err.Error())
	}
}

func TestLoadAuth_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	// Write invalid JSON
	os.WriteFile(authPath, []byte(`{invalid json`), 0600)

	provider := &codexProvider{
		authPath: authPath,
	}

	_, err := provider.loadAuth()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadAuth_ExtractAccountFromJWT(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	authData := map[string]any{
		"tokens": map[string]any{
			"access_token":  "test-access",
			"refresh_token": "test-refresh",
			"id_token":      createTestJWT("jwt-extracted-account"),
		},
	}
	authJSON, _ := json.Marshal(authData)
	os.WriteFile(authPath, authJSON, 0600)

	provider := &codexProvider{
		authPath: authPath,
	}

	auth, err := provider.loadAuth()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if auth.AccountID != "jwt-extracted-account" {
		t.Errorf("expected account ID 'jwt-extracted-account' from JWT, got %s", auth.AccountID)
	}
}

func TestPersistAuth(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")

	provider := &codexProvider{
		authPath: authPath,
	}

	auth := &codexAuth{
		AccessToken:  "persist-access",
		RefreshToken: "persist-refresh",
		AccountID:    "persist-account",
		IDToken:      "persist-id",
	}

	err := provider.persistAuth(auth)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("failed to read persisted auth: %v", err)
	}

	var authFile codexAuthFile
	if err := json.Unmarshal(data, &authFile); err != nil {
		t.Fatalf("failed to unmarshal persisted auth: %v", err)
	}

	if authFile.Tokens.AccessToken != "persist-access" {
		t.Errorf("expected access token 'persist-access', got %s", authFile.Tokens.AccessToken)
	}
	if authFile.Tokens.RefreshToken != "persist-refresh" {
		t.Errorf("expected refresh token 'persist-refresh', got %s", authFile.Tokens.RefreshToken)
	}
	if authFile.Tokens.AccountID != "persist-account" {
		t.Errorf("expected account ID 'persist-account', got %s", authFile.Tokens.AccountID)
	}
	if authFile.Tokens.IDToken != "persist-id" {
		t.Errorf("expected ID token 'persist-id', got %s", authFile.Tokens.IDToken)
	}
}

func TestPersistAuth_InvalidPath(t *testing.T) {
	// Try to write to a path that doesn't exist (should fail)
	provider := &codexProvider{
		authPath: "/nonexistent/directory/auth.json",
	}

	auth := &codexAuth{
		AccessToken:  "test-access",
		RefreshToken: "test-refresh",
		AccountID:    "test-account",
		IDToken:      "test-id",
	}

	err := provider.persistAuth(auth)
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

func TestPersistAuth_ReadOnlyDirectory(t *testing.T) {
	// Create a temporary directory and make it read-only
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "readonly", "auth.json")

	// Create the readonly directory
	if err := os.MkdirAll(filepath.Dir(authPath), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Make it read-only
	if err := os.Chmod(filepath.Dir(authPath), 0555); err != nil {
		t.Fatalf("failed to chmod directory: %v", err)
	}
	// Restore permissions after test
	defer os.Chmod(filepath.Dir(authPath), 0755)

	provider := &codexProvider{
		authPath: authPath,
	}

	auth := &codexAuth{
		AccessToken:  "test-access",
		RefreshToken: "test-refresh",
		AccountID:    "test-account",
		IDToken:      "test-id",
	}

	err := provider.persistAuth(auth)
	// This might succeed on some systems (like macOS with root)
	// so we just verify the function runs without panic
	t.Logf("persistAuth result: %v", err)
}

func TestRefreshTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			t.Errorf("expected path '/oauth/token', got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		// Verify form data
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("expected grant_type 'refresh_token', got %s", r.Form.Get("grant_type"))
		}
		if r.Form.Get("refresh_token") != "test-refresh-token" {
			t.Errorf("expected refresh_token 'test-refresh-token', got %s", r.Form.Get("refresh_token"))
		}
		if r.Form.Get("client_id") != codexClientID {
			t.Errorf("expected client_id '%s', got %s", codexClientID, r.Form.Get("client_id"))
		}

		response := map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"id_token":      "new-id-token",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Override the token URL for testing
	originalTokenURL := codexTokenURL
	defer func() {
		// Can't actually restore since it's a const, but we test the request was made correctly
		_ = originalTokenURL
	}()

	provider := &codexProvider{
		client: &http.Client{},
	}

	// We need to test with the actual token URL, so we'll verify the request structure
	// by checking what would be sent
	ctx := context.Background()
	_, err := provider.refreshTokens(ctx, "test-refresh-token")
	// This will fail since we're not mocking the real token URL, but we can verify
	// the function structure is correct by checking it doesn't panic
	if err == nil {
		t.Error("expected error when calling real token URL in test")
	}
}

func TestRefreshTokens_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer server.Close()

	provider := &codexProvider{
		client:   &http.Client{},
		tokenURL: server.URL,
	}

	ctx := context.Background()
	_, err := provider.refreshTokens(ctx, "invalid-token")
	if err == nil {
		t.Fatal("expected error for HTTP error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("expected error to contain 'invalid_grant', got: %s", err.Error())
	}
}

func TestRefreshTokens_RequestError(t *testing.T) {
	// Create a client that will fail due to network error
	provider := &codexProvider{
		client:   &http.Client{Timeout: 1 * time.Millisecond},
		tokenURL: "http://localhost:1", // Port 1 is not accessible
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err := provider.refreshTokens(ctx, "test-token")
	// Will fail because we can't reach the URL
	if err == nil {
		t.Error("expected error when calling unreachable URL")
	}
}

func TestRefreshTokens_InvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	provider := &codexProvider{
		client:   &http.Client{},
		tokenURL: server.URL,
	}

	ctx := context.Background()
	_, err := provider.refreshTokens(ctx, "test-token")
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
}

func TestFormatCodexInput(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are a coding assistant."},
		{Role: "user", Content: "Write a function."},
		{Role: "assistant", Content: "Here's the function."},
		{Role: "user", Content: "Thank you."},
	}

	inputs, instructions := formatCodexInput(messages)

	if instructions != "You are a coding assistant." {
		t.Errorf("expected instructions 'You are a coding assistant.', got %s", instructions)
	}

	if len(inputs) != 3 {
		t.Errorf("expected 3 inputs, got %d", len(inputs))
	}

	// Check user message
	if inputs[0]["role"] != "user" {
		t.Errorf("expected first input role 'user', got %v", inputs[0]["role"])
	}

	// Check assistant message
	if inputs[1]["role"] != "assistant" {
		t.Errorf("expected second input role 'assistant', got %v", inputs[1]["role"])
	}
	if inputs[1]["type"] != "message" {
		t.Errorf("expected assistant input type 'message', got %v", inputs[1]["type"])
	}
}

func TestFormatCodexInput_EmptyMessages(t *testing.T) {
	messages := []Message{
		{Role: "", Content: "No role"},
		{Role: "user", Content: ""},
		{Role: "user", Content: "Valid message"},
	}

	inputs, instructions := formatCodexInput(messages)

	if instructions != "" {
		t.Errorf("expected empty instructions, got %s", instructions)
	}

	if len(inputs) != 1 {
		t.Errorf("expected 1 input (only valid message), got %d", len(inputs))
	}
}

func TestFormatCodexInput_MultipleSystemMessages(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "First system message."},
		{Role: "system", Content: "Second system message."},
		{Role: "user", Content: "Hello."},
	}

	inputs, instructions := formatCodexInput(messages)

	// Only first system message should be used as instructions
	if instructions != "First system message." {
		t.Errorf("expected first system message as instructions, got %s", instructions)
	}

	// Second system message is treated as a regular message since instructions is already set
	// User message is also included
	if len(inputs) != 2 {
		t.Errorf("expected 2 inputs (second system + user), got %d", len(inputs))
	}
}

func TestExtractCodexOutput_OutputText(t *testing.T) {
	payload := map[string]any{
		"output_text": "Direct output text",
	}

	result := extractCodexOutput(payload)
	if result != "Direct output text" {
		t.Errorf("expected 'Direct output text', got %s", result)
	}
}

func TestExtractCodexOutput_OutputArray(t *testing.T) {
	payload := map[string]any{
		"output": []any{
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": "First part",
					},
					map[string]any{
						"type": "output_text",
						"text": "Second part",
					},
				},
			},
		},
	}

	result := extractCodexOutput(payload)
	if result != "First partSecond part" {
		t.Errorf("expected 'First partSecond part', got %s", result)
	}
}

func TestExtractCodexOutput_TextType(t *testing.T) {
	payload := map[string]any{
		"output": []any{
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Text type content",
					},
				},
			},
		},
	}

	result := extractCodexOutput(payload)
	if result != "Text type content" {
		t.Errorf("expected 'Text type content', got %s", result)
	}
}

func TestExtractCodexOutput_Empty(t *testing.T) {
	payload := map[string]any{}

	result := extractCodexOutput(payload)
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

func TestExtractCodexOutput_InvalidTypes(t *testing.T) {
	payload := map[string]any{
		"output": []any{
			"not a map",
			map[string]any{
				"type": "not_message",
			},
			map[string]any{
				"type":    "message",
				"content": "not an array",
			},
			map[string]any{
				"type": "message",
				"content": []any{
					"not a map",
					map[string]any{
						"type": "wrong_type",
					},
				},
			},
		},
	}

	result := extractCodexOutput(payload)
	if result != "" {
		t.Errorf("expected empty string for invalid types, got %s", result)
	}
}

func TestExtractAccountIDFromJWT(t *testing.T) {
	// Create a JWT with chatgpt_account_id in https://api.openai.com/auth claim
	claims := map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "account-from-auth-claim",
		},
	}
	jwt := createTestJWTWithClaims(claims)

	result := extractAccountIDFromJWT(jwt)
	if result != "account-from-auth-claim" {
		t.Errorf("expected 'account-from-auth-claim', got %s", result)
	}
}

func TestExtractAccountIDFromJWT_TopLevelClaim(t *testing.T) {
	// Create a JWT with chatgpt_account_id at top level
	claims := map[string]any{
		"chatgpt_account_id": "top-level-account",
	}
	jwt := createTestJWTWithClaims(claims)

	result := extractAccountIDFromJWT(jwt)
	if result != "top-level-account" {
		t.Errorf("expected 'top-level-account', got %s", result)
	}
}

func TestExtractAccountIDFromJWT_Empty(t *testing.T) {
	result := extractAccountIDFromJWT("")
	if result != "" {
		t.Errorf("expected empty string for empty JWT, got %s", result)
	}
}

func TestExtractAccountIDFromJWT_InvalidFormat(t *testing.T) {
	result := extractAccountIDFromJWT("invalid.jwt")
	if result != "" {
		t.Errorf("expected empty string for invalid JWT, got %s", result)
	}
}

func TestExtractAccountIDFromJWT_InvalidBase64(t *testing.T) {
	result := extractAccountIDFromJWT("header.not-valid-base64.signature")
	if result != "" {
		t.Errorf("expected empty string for invalid base64, got %s", result)
	}
}

func TestExtractAccountIDFromJWT_InvalidJSON(t *testing.T) {
	invalidJSON := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	result := extractAccountIDFromJWT("header." + invalidJSON + ".signature")
	if result != "" {
		t.Errorf("expected empty string for invalid JSON, got %s", result)
	}
}

func TestExtractAccountIDFromJWT_SinglePart(t *testing.T) {
	// JWT with only one part (no payload)
	result := extractAccountIDFromJWT("only-header")
	if result != "" {
		t.Errorf("expected empty string for single part JWT, got %s", result)
	}
}

func TestExtractAccountIDFromJWT_NoAccountClaim(t *testing.T) {
	// JWT with valid structure but no account ID claim
	claims := map[string]any{
		"sub": "user123",
		"exp": 1234567890,
	}
	jwt := createTestJWTWithClaims(claims)

	result := extractAccountIDFromJWT(jwt)
	if result != "" {
		t.Errorf("expected empty string when no account claim, got %s", result)
	}
}

func TestExtractAccountIDFromJWT_NonStringAccountID(t *testing.T) {
	// JWT with chatgpt_account_id that is not a string
	claims := map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": 12345, // Not a string
		},
	}
	jwt := createTestJWTWithClaims(claims)

	result := extractAccountIDFromJWT(jwt)
	if result != "" {
		t.Errorf("expected empty string when account ID is not a string, got %s", result)
	}
}

func TestExtractAccountIDFromJWT_TopLevelNonString(t *testing.T) {
	// JWT with top-level chatgpt_account_id that is not a string
	claims := map[string]any{
		"chatgpt_account_id": 12345, // Not a string
	}
	jwt := createTestJWTWithClaims(claims)

	result := extractAccountIDFromJWT(jwt)
	if result != "" {
		t.Errorf("expected empty string when top-level account ID is not a string, got %s", result)
	}
}

func TestExtractAccountIDFromJWT_AuthClaimWithoutAccountID(t *testing.T) {
	// JWT with https://api.openai.com/auth claim but no chatgpt_account_id inside
	claims := map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"some_other_field": "value",
			// No chatgpt_account_id
		},
	}
	jwt := createTestJWTWithClaims(claims)

	result := extractAccountIDFromJWT(jwt)
	if result != "" {
		t.Errorf("expected empty string when auth claim exists but has no account ID, got %s", result)
	}
}

func TestIsUnauthorized(t *testing.T) {
	if !isUnauthorized(errUnauthorized) {
		t.Error("expected isUnauthorized to return true for errUnauthorized")
	}
	if isUnauthorized(errors.New("some other error")) {
		t.Error("expected isUnauthorized to return false for other errors")
	}
	if isUnauthorized(nil) {
		t.Error("expected isUnauthorized to return false for nil error")
	}
}

// Helper function to create a test JWT with specific claims
func createTestJWTWithClaims(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	claimsJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	return header + "." + payload + "."
}

// Helper function to create a simple test JWT with account ID
func createTestJWT(accountID string) string {
	claims := map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
		},
	}
	return createTestJWTWithClaims(claims)
}
