package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultOpenCodeBaseURL = "https://opencode.ai/zen/v1"
	defaultOpenCodeTimeout = 35 * time.Second
)

type OpenCodeConfig struct {
	APIKey  string
	Model   string
	BaseURL string
}

type OpenCodeProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewOpenCodeProvider(cfg OpenCodeConfig) *OpenCodeProvider {
	return &OpenCodeProvider{
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		baseURL: normalizeOpenCodeBaseURL(cfg.BaseURL),
		client:  &http.Client{Timeout: defaultOpenCodeTimeout},
	}
}

func (p *OpenCodeProvider) Generate(ctx context.Context, messages []Message) (string, error) {
	if p.apiKey == "" {
		return "", errors.New("missing API key for remote provider")
	}
	if p.model == "" {
		return "", errors.New("missing model for remote provider")
	}

	model := normalizeOpenCodeModel(p.model)
	endpoint := p.baseURL + "/chat/completions"
	debugEnabled := openCodeDebugEnabled()
	if debugEnabled {
		fmt.Printf("[DEBUG] OpenCode: model=%q, endpoint=%q, baseURL=%q\n", model, endpoint, p.baseURL)
	}

	// OpenCode Zen uses a unified chat completions endpoint for all models
	// The model parameter routes to the correct backend
	payload := map[string]any{
		"model":    model,
		"messages": messages,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	if debugEnabled {
		fmt.Printf("[DEBUG] OpenCode request: %s\n", string(body))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("opencode request timed out after %s while awaiting response headers", p.client.Timeout)
		}
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("opencode request failed: %s %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
	}

	content, err := parseOpenCodeChatResponse(bodyBytes)
	if err != nil {
		return "", err
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("LLM response was empty")
	}

	return content, nil
}

func normalizeOpenCodeBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if trimmed == "" {
		return defaultOpenCodeBaseURL
	}
	// Normalize old/incorrect URLs to the correct one
	if trimmed == "https://api.opencode.ai/v1" {
		return defaultOpenCodeBaseURL
	}
	return trimmed
}

func normalizeOpenCodeModel(model string) string {
	// Strip the opencode/ prefix if present
	if strings.HasPrefix(model, "opencode/") {
		return strings.TrimPrefix(model, "opencode/")
	}
	return model
}

func parseOpenCodeChatResponse(body []byte) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}

	// Try OpenAI-style choices format
	if choices, ok := payload["choices"].([]any); ok && len(choices) > 0 {
		choice, ok := choices[0].(map[string]any)
		if ok {
			if message, ok := choice["message"].(map[string]any); ok {
				if content, ok := message["content"].(string); ok && content != "" {
					return content, nil
				}
			}
			if content, ok := choice["text"].(string); ok && content != "" {
				return content, nil
			}
		}
	}

	// Try direct content field
	if content, ok := payload["content"].(string); ok && content != "" {
		return content, nil
	}

	// Try output_text field
	if outputText, ok := payload["output_text"].(string); ok && outputText != "" {
		return outputText, nil
	}

	// Try text field
	if text, ok := payload["text"].(string); ok && text != "" {
		return text, nil
	}

	return "", errors.New("LLM response had no content")
}

func openCodeDebugEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("OPENCODE_DEBUG")))
	return value == "1" || value == "true" || value == "yes"
}
