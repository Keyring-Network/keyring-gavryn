package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/llm"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/secrets"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

var newLLMProvider = llm.NewProvider
var encryptLLMSecret = secrets.Encrypt

type llmSettingsRequest struct {
	Mode          string `json:"mode"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	BaseURL       string `json:"base_url"`
	APIKey        string `json:"api_key"`
	CodexAuthPath string `json:"codex_auth_path"`
	CodexHome     string `json:"codex_home"`
}

type llmModelsRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
}

type llmSettingsResponse struct {
	Configured    bool   `json:"configured"`
	Mode          string `json:"mode"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	BaseURL       string `json:"base_url"`
	CodexAuthPath string `json:"codex_auth_path,omitempty"`
	CodexHome     string `json:"codex_home,omitempty"`
	HasAPIKey     bool   `json:"has_api_key"`
	APIKeyHint    string `json:"api_key_hint,omitempty"`
}

func (s *Server) getLLMSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetLLMSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := llmSettingsResponse{
		Configured: false,
		Mode:       s.cfg.LLMMode,
		Provider:   s.cfg.LLMProvider,
		Model:      s.cfg.LLMModel,
		BaseURL:    s.cfg.LLMBaseURL,
	}
	if settings != nil {
		response.Configured = true
		response.Mode = settings.Mode
		response.Provider = settings.Provider
		response.Model = settings.Model
		response.BaseURL = settings.BaseURL
		response.CodexAuthPath = settings.CodexAuthPath
		response.CodexHome = settings.CodexHome
		response.HasAPIKey = settings.APIKeyEnc != ""
		if settings.APIKeyEnc != "" && s.cfg.LLMSecretsKey != "" {
			if key, err := secrets.ParseKey(s.cfg.LLMSecretsKey); err == nil {
				if apiKey, err := secrets.Decrypt(key, settings.APIKeyEnc); err == nil {
					if len(apiKey) >= 4 {
						response.APIKeyHint = apiKey[len(apiKey)-4:]
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) updateLLMSettings(w http.ResponseWriter, r *http.Request) {
	var req llmSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	settings, err := s.store.GetLLMSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mode := firstNonEmpty(req.Mode, s.cfg.LLMMode)
	provider := firstNonEmpty(req.Provider, s.cfg.LLMProvider)
	model := firstNonEmpty(req.Model, s.cfg.LLMModel)
	baseURL := firstNonEmpty(req.BaseURL, s.cfg.LLMBaseURL)
	codexAuthPath := firstNonEmpty(req.CodexAuthPath, s.cfg.CodexAuthPath)
	codexHome := firstNonEmpty(req.CodexHome, s.cfg.CodexHome)
	if settings != nil {
		mode = firstNonEmpty(req.Mode, settings.Mode)
		provider = firstNonEmpty(req.Provider, settings.Provider)
		model = firstNonEmpty(req.Model, settings.Model)
		baseURL = firstNonEmpty(req.BaseURL, settings.BaseURL)
		codexAuthPath = firstNonEmpty(req.CodexAuthPath, settings.CodexAuthPath)
		codexHome = firstNonEmpty(req.CodexHome, settings.CodexHome)
	}

	apiKeyEnc := ""
	if settings != nil {
		apiKeyEnc = settings.APIKeyEnc
	}
	if req.APIKey != "" {
		key, err := secrets.ParseKey(s.cfg.LLMSecretsKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ciphertext, err := encryptLLMSecret(key, req.APIKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		apiKeyEnc = ciphertext
	}
	if providerNeedsKey(provider) && apiKeyEnc == "" && mode != "local" {
		http.Error(w, "API key required for provider", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	createdAt := now
	if settings != nil && settings.CreatedAt != "" {
		createdAt = settings.CreatedAt
	}
	newSettings := store.LLMSettings{
		Mode:          mode,
		Provider:      provider,
		Model:         model,
		BaseURL:       baseURL,
		APIKeyEnc:     apiKeyEnc,
		CodexAuthPath: codexAuthPath,
		CodexHome:     codexHome,
		CreatedAt:     createdAt,
		UpdatedAt:     now,
	}
	if err := s.store.UpsertLLMSettings(r.Context(), newSettings); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.getLLMSettings(w, r)
}

func (s *Server) testLLMSettings(w http.ResponseWriter, r *http.Request) {
	var req llmSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	providerConfig, err := s.buildLLMConfig(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	provider, err := newLLMProvider(providerConfig)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	_, err = provider.Generate(ctx, []llm.Message{{Role: "user", Content: "ping"}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Connected"})
}

func (s *Server) listLLMModels(w http.ResponseWriter, r *http.Request) {
	var req llmModelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	provider := req.Provider
	if provider == "" {
		provider = s.cfg.LLMProvider
		if settings, _ := s.store.GetLLMSettings(r.Context()); settings != nil && settings.Provider != "" {
			provider = settings.Provider
			if req.BaseURL == "" {
				req.BaseURL = settings.BaseURL
			}
			if req.APIKey == "" && settings.APIKeyEnc != "" && s.cfg.LLMSecretsKey != "" {
				if key, err := secrets.ParseKey(s.cfg.LLMSecretsKey); err == nil {
					if apiKey, err := secrets.Decrypt(key, settings.APIKeyEnc); err == nil {
						req.APIKey = apiKey
					}
				}
			}
			if req.Model == "" {
				req.Model = settings.Model
			}
		}
	}

	models, err := fetchModels(provider, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
}

func (s *Server) buildLLMConfig(ctx context.Context, req llmSettingsRequest) (llm.Config, error) {
	mode := firstNonEmpty(req.Mode, s.cfg.LLMMode)
	provider := firstNonEmpty(req.Provider, s.cfg.LLMProvider)
	model := firstNonEmpty(req.Model, s.cfg.LLMModel)
	baseURL := firstNonEmpty(req.BaseURL, s.cfg.LLMBaseURL)
	codexAuthPath := firstNonEmpty(req.CodexAuthPath, s.cfg.CodexAuthPath)
	codexHome := firstNonEmpty(req.CodexHome, s.cfg.CodexHome)

	var apiKey string
	if req.APIKey != "" {
		apiKey = req.APIKey
	} else if settings, err := s.store.GetLLMSettings(ctx); err == nil && settings != nil {
		if providerNeedsKey(provider) && settings.APIKeyEnc != "" {
			key, err := secrets.ParseKey(s.cfg.LLMSecretsKey)
			if err != nil {
				return llm.Config{}, err
			}
			decrypted, err := secrets.Decrypt(key, settings.APIKeyEnc)
			if err != nil {
				return llm.Config{}, err
			}
			apiKey = decrypted
		}
	}
	if providerNeedsKey(provider) && apiKey == "" && mode != "local" {
		return llm.Config{}, errors.New("API key required for provider")
	}

	config := llm.Config{
		Mode:          mode,
		Provider:      provider,
		Model:         model,
		BaseURL:       baseURL,
		OpenAIAPIKey:  apiKey,
		CodexAuthPath: codexAuthPath,
		CodexHome:     codexHome,
	}
	switch provider {
	case "openrouter":
		config.OpenRouterAPIKey = apiKey
		config.OpenAIAPIKey = ""
	case "opencode-zen":
		config.OpenCodeAPIKey = apiKey
		config.OpenAIAPIKey = ""
	}
	return config, nil
}

func fetchModels(provider string, req llmModelsRequest) ([]string, error) {
	if provider == "opencode-zen" {
		models, err := fetchOpenCodeModels()
		if err != nil {
			return nil, err
		}
		return models, nil
	}
	if provider == "kimi-for-coding" {
		models := fetchKimiModels(req)
		return models, nil
	}
	if provider == "moonshot-ai" {
		models := fetchMoonshotModels(req)
		return models, nil
	}
	if provider == "codex" {
		models := []string{"gpt-5.2-codex", "gpt-5.1-codex"}
		return models, nil
	}
	if providerNeedsKey(provider) && req.APIKey == "" {
		return nil, errors.New("API key required to list models")
	}
	baseURL := req.BaseURL
	if baseURL == "" {
		if provider == "openrouter" {
			baseURL = "https://openrouter.ai/api/v1"
		} else {
			baseURL = "https://api.openai.com/v1"
		}
	}
	client := &http.Client{Timeout: 30 * time.Second}
	url := strings.TrimRight(baseURL, "/") + "/models"
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+req.APIKey)
	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("model list failed: %s", resp.Status)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(payload.Data))
	for _, entry := range payload.Data {
		if entry.ID != "" {
			models = append(models, entry.ID)
		}
	}
	sort.Strings(models)
	return models, nil
}

func providerNeedsKey(provider string) bool {
	switch provider {
	case "openai", "openrouter", "opencode-zen", "kimi-for-coding", "moonshot-ai":
		return true
	default:
		return false
	}
}

func fetchKimiModels(req llmModelsRequest) []string {
	// Try to fetch from API if key provided
	if req.APIKey != "" {
		baseURL := req.BaseURL
		if baseURL == "" {
			baseURL = "https://api.kimi.com/coding/v1"
		}
		client := &http.Client{Timeout: 30 * time.Second}
		url := strings.TrimRight(baseURL, "/") + "/models"
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err == nil {
			request.Header.Set("Authorization", "Bearer "+req.APIKey)
			resp, err := client.Do(request)
			if err == nil && resp.StatusCode < 400 {
				defer resp.Body.Close()
				var payload struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && len(payload.Data) > 0 {
					models := make([]string, 0, len(payload.Data))
					for _, entry := range payload.Data {
						if entry.ID != "" {
							models = append(models, entry.ID)
						}
					}
					if len(models) > 0 {
						sort.Strings(models)
						return models
					}
				}
			} else if resp != nil {
				resp.Body.Close()
			}
		}
	}
	// Fallback to hardcoded list
	return []string{
		"kimi-k2",
		"kimi-k2-thinking",
		"kimi-k2.5",
		"kimi-k2.5-free",
	}
}

func fetchMoonshotModels(req llmModelsRequest) []string {
	// Try to fetch from API if key provided
	if req.APIKey != "" {
		baseURL := req.BaseURL
		if baseURL == "" {
			baseURL = "https://api.moonshot.ai/v1"
		}
		client := &http.Client{Timeout: 30 * time.Second}
		url := strings.TrimRight(baseURL, "/") + "/models"
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err == nil {
			request.Header.Set("Authorization", "Bearer "+req.APIKey)
			resp, err := client.Do(request)
			if err == nil && resp.StatusCode < 400 {
				defer resp.Body.Close()
				var payload struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && len(payload.Data) > 0 {
					models := make([]string, 0, len(payload.Data))
					for _, entry := range payload.Data {
						if entry.ID != "" {
							models = append(models, entry.ID)
						}
					}
					if len(models) > 0 {
						sort.Strings(models)
						return models
					}
				}
			} else if resp != nil {
				resp.Body.Close()
			}
		}
	}
	// Fallback to popular Moonshot models
	return []string{
		"moonshot-v1-8k",
		"moonshot-v1-32k",
		"moonshot-v1-128k",
		"moonshot-v1-8k-vision",
		"moonshot-v1-32k-vision",
		"moonshot-v1-128k-vision",
	}
}

func fetchOpenCodeModels() ([]string, error) {
	// Comprehensive fallback list of OpenCode models (sorted alphabetically)
	// Based on actual models available from OpenCode screenshots
	fallbackModels := []string{
		"big-pickle",
		"claude-3-5-haiku",
		"claude-haiku-4-5",
		"claude-opus-4-1",
		"claude-opus-4-5",
		"claude-sonnet-4",
		"claude-sonnet-4-5",
		"gemini-3-flash",
		"gemini-3-pro",
		"glm-4.6",
		"glm-4.7",
		"glm-4.7-free",
		"gpt-5",
		"gpt-5-codex",
		"gpt-5-nano",
		"gpt-5.1",
		"gpt-5.1-codex",
		"gpt-5.1-codex-max",
		"gpt-5.1-codex-mini",
		"gpt-5.2",
		"gpt-5.2-codex",
		"grok-code",
		"kimi-k2",
		"kimi-k2-thinking",
		"kimi-k2.5",
		"kimi-k2.5-free",
		"minimax-m2.1",
		"minimax-m2.1-free",
		"qwen3-coder",
		"trinity-large-preview-free",
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// Try endpoints in order of preference
	endpoints := []string{
		"https://api.opencode.ai/v1/models",
		"https://models.dev/api/models",
		"https://models.dev/api/models.json",
	}

	for _, endpoint := range endpoints {
		resp, err := client.Get(endpoint)
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		// Try parsing as flat array of model IDs first
		var flatPayload []string
		if err := json.NewDecoder(resp.Body).Decode(&flatPayload); err == nil && len(flatPayload) > 0 {
			resp.Body.Close()
			models := dedupeAndSortModels(flatPayload)
			if len(models) > 0 {
				return models, nil
			}
			continue
		}

		// Try OpenAI-compatible format with "data" array
		resp.Body.Close()
		resp, err = client.Get(endpoint)
		if err != nil {
			continue
		}

		var openAIFormat struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&openAIFormat); err == nil && len(openAIFormat.Data) > 0 {
			resp.Body.Close()
			models := make([]string, 0, len(openAIFormat.Data))
			seen := make(map[string]struct{})
			for _, m := range openAIFormat.Data {
				if m.ID != "" {
					if _, exists := seen[m.ID]; !exists {
						seen[m.ID] = struct{}{}
						models = append(models, m.ID)
					}
				}
			}
			sort.Strings(models)
			if len(models) > 0 {
				return models, nil
			}
			continue
		}

		// Try object array format with "id" field
		resp.Body.Close()
		resp, err = client.Get(endpoint)
		if err != nil {
			continue
		}

		var objPayload []struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&objPayload); err == nil && len(objPayload) > 0 {
			resp.Body.Close()
			models := make([]string, 0, len(objPayload))
			seen := make(map[string]struct{})
			for _, m := range objPayload {
				if m.ID != "" {
					if _, exists := seen[m.ID]; !exists {
						seen[m.ID] = struct{}{}
						models = append(models, m.ID)
					}
				}
			}
			sort.Strings(models)
			if len(models) > 0 {
				return models, nil
			}
			continue
		}

		resp.Body.Close()
	}

	// All endpoints failed, return fallback
	return fallbackModels, nil
}

func dedupeAndSortModels(models []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(models))
	for _, m := range models {
		if m != "" {
			if _, exists := seen[m]; !exists {
				seen[m] = struct{}{}
				result = append(result, m)
			}
		}
	}
	sort.Strings(result)
	return result
}

func firstNonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
