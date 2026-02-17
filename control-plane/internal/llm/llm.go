package llm

import (
	"context"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Provider interface {
	Generate(ctx context.Context, messages []Message) (string, error)
}

type Config struct {
	Mode             string
	Provider         string
	Model            string
	BaseURL          string
	FallbackProvider string
	FallbackModel    string
	FallbackBaseURL  string
	OpenAIAPIKey     string
	OpenRouterAPIKey string
	OpenCodeAPIKey   string
	CodexAuthPath    string
	CodexHome        string
}

func NewProvider(cfg Config) (Provider, error) {
	if cfg.Mode == "local" {
		return LocalProvider{}, nil
	}

	switch cfg.Provider {
	case "codex":
		return NewCodexProvider(cfg)
	case "openai":
		return NewOpenAIProvider(OpenAIConfig{
			APIKey:  cfg.OpenAIAPIKey,
			Model:   cfg.Model,
			BaseURL: cfg.BaseURL,
		}), nil
	case "opencode-zen":
		return NewOpenCodeProvider(OpenCodeConfig{
			APIKey:  cfg.OpenCodeAPIKey,
			Model:   cfg.Model,
			BaseURL: cfg.BaseURL,
		}), nil
	case "openrouter":
		return NewOpenAIProvider(OpenAIConfig{
			APIKey:  cfg.OpenRouterAPIKey,
			Model:   cfg.Model,
			BaseURL: defaultIfEmpty(cfg.BaseURL, "https://openrouter.ai/api/v1"),
		}), nil
	case "kimi-for-coding":
		return NewOpenAIProvider(OpenAIConfig{
			APIKey:  cfg.OpenAIAPIKey,
			Model:   cfg.Model,
			BaseURL: defaultIfEmpty(cfg.BaseURL, "https://api.kimi.com/coding/v1"),
		}), nil
	case "moonshot-ai":
		return NewOpenAIProvider(OpenAIConfig{
			APIKey:  cfg.OpenAIAPIKey,
			Model:   cfg.Model,
			BaseURL: defaultIfEmpty(cfg.BaseURL, "https://api.moonshot.ai/v1"),
		}), nil
	default:
		return nil, ErrUnsupportedProvider{Provider: cfg.Provider}
	}
}

func defaultIfEmpty(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
