package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultCodexBaseURL = "https://chatgpt.com/backend-api/codex"
	codexTokenURL       = "https://auth.openai.com/oauth/token"
	codexClientID       = "app_EMoamEEZ73f0CkXaXp7hrann"
)

type codexProvider struct {
	model      string
	baseURL    string
	authPath   string
	sessionID  string
	client     *http.Client
	cachedAuth *codexAuth
	tokenURL   string // Configurable for testing
}

type codexAuth struct {
	AccessToken  string
	RefreshToken string
	AccountID    string
	IDToken      string
}

type codexAuthFile struct {
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
		IDToken      string `json:"id_token"`
	} `json:"tokens"`
}

type codexRefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

func NewCodexProvider(cfg Config) (Provider, error) {
	model := cfg.Model
	if model == "" {
		model = "gpt-5.2-codex"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultCodexBaseURL
	}
	return &codexProvider{
		model:     model,
		baseURL:   strings.TrimRight(baseURL, "/"),
		authPath:  resolveCodexAuthPath(cfg),
		sessionID: uuid.NewString(),
		client:    &http.Client{Timeout: 35 * time.Second},
		tokenURL:  codexTokenURL,
	}, nil
}

func resolveCodexAuthPath(cfg Config) string {
	if cfg.CodexAuthPath != "" {
		return expandHome(cfg.CodexAuthPath)
	}
	codexHome := cfg.CodexHome
	if codexHome == "" {
		codexHome = "~/.codex"
	}
	return filepath.Join(expandHome(codexHome), "auth.json")
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

func (p *codexProvider) Generate(ctx context.Context, messages []Message) (string, error) {
	auth, err := p.loadAuth()
	if err != nil {
		return "", err
	}
	text, err := p.request(ctx, auth, messages)
	if err == nil {
		return text, nil
	}
	if !isUnauthorized(err) || auth.RefreshToken == "" {
		return "", err
	}
	refreshed, refreshErr := p.refreshTokens(ctx, auth.RefreshToken)
	if refreshErr != nil {
		return "", err
	}
	if refreshed.AccessToken != "" {
		auth.AccessToken = refreshed.AccessToken
	}
	if refreshed.RefreshToken != "" {
		auth.RefreshToken = refreshed.RefreshToken
	}
	if refreshed.IDToken != "" {
		auth.IDToken = refreshed.IDToken
		if auth.AccountID == "" {
			auth.AccountID = extractAccountIDFromJWT(auth.IDToken)
		}
	}
	_ = p.persistAuth(auth)
	return p.request(ctx, auth, messages)
}

func (p *codexProvider) request(ctx context.Context, auth *codexAuth, messages []Message) (string, error) {
	inputs, instructions := formatCodexInput(messages)
	payload := map[string]any{
		"model":        p.model,
		"input":        inputs,
		"instructions": instructions,
		"store":        false,
		"stream":       false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("originator", "gavryn")
	req.Header.Set("session_id", p.sessionID)
	req.Header.Set("User-Agent", "gavryn")
	if auth.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", auth.AccountID)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", errUnauthorized
	}
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("codex request failed: %s %s", resp.Status, string(bodyBytes))
	}

	var payloadResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payloadResp); err != nil {
		return "", err
	}
	text := extractCodexOutput(payloadResp)
	if strings.TrimSpace(text) == "" {
		return "", errors.New("codex response was empty")
	}
	return text, nil
}

func (p *codexProvider) loadAuth() (*codexAuth, error) {
	if p.cachedAuth != nil {
		return p.cachedAuth, nil
	}
	data, err := os.ReadFile(p.authPath)
	if err != nil {
		return nil, fmt.Errorf("codex auth.json not found at %s; run `codex login`", p.authPath)
	}
	var authFile codexAuthFile
	if err := json.Unmarshal(data, &authFile); err != nil {
		return nil, err
	}
	if authFile.Tokens.AccessToken == "" {
		return nil, errors.New("codex auth.json missing access_token; run `codex login`")
	}
	auth := &codexAuth{
		AccessToken:  authFile.Tokens.AccessToken,
		RefreshToken: authFile.Tokens.RefreshToken,
		AccountID:    authFile.Tokens.AccountID,
		IDToken:      authFile.Tokens.IDToken,
	}
	if auth.AccountID == "" && auth.IDToken != "" {
		auth.AccountID = extractAccountIDFromJWT(auth.IDToken)
	}
	p.cachedAuth = auth
	return auth, nil
}

func (p *codexProvider) persistAuth(auth *codexAuth) error {
	authFile := codexAuthFile{}
	authFile.Tokens.AccessToken = auth.AccessToken
	authFile.Tokens.RefreshToken = auth.RefreshToken
	authFile.Tokens.AccountID = auth.AccountID
	authFile.Tokens.IDToken = auth.IDToken
	data, err := json.MarshalIndent(authFile, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.authPath, data, 0o600)
}

func (p *codexProvider) refreshTokens(ctx context.Context, refreshToken string) (*codexRefreshResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", codexClientID)
	tokenURL := p.tokenURL
	if tokenURL == "" {
		tokenURL = codexTokenURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("codex token refresh failed: %s %s", resp.Status, string(bodyBytes))
	}
	var refreshed codexRefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&refreshed); err != nil {
		return nil, err
	}
	return &refreshed, nil
}

func formatCodexInput(messages []Message) ([]map[string]any, string) {
	inputs := []map[string]any{}
	instructions := ""
	for _, msg := range messages {
		role := msg.Role
		content := msg.Content
		if role == "" || content == "" {
			continue
		}
		if role == "system" && instructions == "" {
			instructions = content
			continue
		}
		if role == "assistant" {
			inputs = append(inputs, map[string]any{
				"type":    "message",
				"role":    "assistant",
				"content": []map[string]any{{"type": "output_text", "text": content}},
			})
			continue
		}
		inputs = append(inputs, map[string]any{
			"role":    role,
			"content": []map[string]any{{"type": "input_text", "text": content}},
		})
	}
	return inputs, instructions
}

func extractCodexOutput(payload map[string]any) string {
	if outputText, ok := payload["output_text"].(string); ok && outputText != "" {
		return outputText
	}
	output, ok := payload["output"].([]any)
	if !ok {
		return ""
	}
	parts := []string{}
	for _, item := range output {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if itemMap["type"] != "message" {
			continue
		}
		content, ok := itemMap["content"].([]any)
		if !ok {
			continue
		}
		for _, part := range content {
			partMap, ok := part.(map[string]any)
			if !ok {
				continue
			}
			partType, _ := partMap["type"].(string)
			if partType != "output_text" && partType != "text" {
				continue
			}
			text, _ := partMap["text"].(string)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "")
}

func extractAccountIDFromJWT(token string) string {
	if token == "" {
		return ""
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	if authClaims, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if account, ok := authClaims["chatgpt_account_id"].(string); ok {
			return account
		}
	}
	if account, ok := claims["chatgpt_account_id"].(string); ok {
		return account
	}
	return ""
}

var errUnauthorized = errors.New("unauthorized")

func isUnauthorized(err error) bool {
	return errors.Is(err, errUnauthorized)
}
