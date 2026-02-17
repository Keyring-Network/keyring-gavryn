package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	ControlPlanePort      string
	ControlPlaneURL       string
	ToolRunnerURL         string
	PostgresURL           string
	TemporalAddress       string
	TemporalTaskQueue     string
	LLMMode               string
	LLMProvider           string
	LLMModel              string
	LLMBaseURL            string
	LLMFallbackProvider   string
	LLMFallbackModel      string
	LLMFallbackBaseURL    string
	OpenAIAPIKey          string
	OpenRouterAPIKey      string
	OpenCodeAPIKey        string
	DiscordWebhookURL     string
	CodexAuthPath         string
	CodexHome             string
	LLMSecretsKey         string
	MemoryMaxResults      int
	MemoryMaxEntryChars   int
	MemoryChunkChars      int
	MemoryChunkOverlap    int
	MemoryMaxChunks       int
	MemoryMinContentChars int
	MemoryMaxContentBytes int
}

func Load() Config {
	controlPlanePort := getEnv("CONTROL_PLANE_PORT", "8080")
	postgresURL := getEnv("POSTGRES_URL", "")
	if postgresURL == "" {
		postgresURL = buildPostgresURL()
	}
	return Config{
		ControlPlanePort:      controlPlanePort,
		ControlPlaneURL:       getEnv("CONTROL_PLANE_URL", "http://localhost:"+controlPlanePort),
		ToolRunnerURL:         getEnv("TOOL_RUNNER_URL", "http://localhost:8081"),
		PostgresURL:           postgresURL,
		TemporalAddress:       getEnv("TEMPORAL_ADDRESS", "localhost:7233"),
		TemporalTaskQueue:     getEnv("TEMPORAL_TASK_QUEUE", "gavryn-runs"),
		LLMMode:               getEnv("LLM_MODE", "remote"),
		LLMProvider:           getEnv("LLM_PROVIDER", "codex"),
		LLMModel:              getEnv("LLM_MODEL", "gpt-5.2-codex"),
		LLMBaseURL:            getEnv("LLM_BASE_URL", ""),
		LLMFallbackProvider:   getEnv("LLM_FALLBACK_PROVIDER", ""),
		LLMFallbackModel:      getEnv("LLM_FALLBACK_MODEL", ""),
		LLMFallbackBaseURL:    getEnv("LLM_FALLBACK_BASE_URL", ""),
		OpenAIAPIKey:          getEnv("OPENAI_API_KEY", ""),
		OpenRouterAPIKey:      getEnv("OPENROUTER_API_KEY", ""),
		OpenCodeAPIKey:        getEnv("OPENCODE_API_KEY", ""),
		DiscordWebhookURL:     getEnv("DISCORD_WEBHOOK_URL", ""),
		CodexAuthPath:         getEnv("CODEX_AUTH_PATH", ""),
		CodexHome:             getEnv("CODEX_HOME", ""),
		LLMSecretsKey:         getEnv("LLM_SECRETS_KEY", ""),
		MemoryMaxResults:      getEnvInt("MEMORY_MAX_RESULTS", 5),
		MemoryMaxEntryChars:   getEnvInt("MEMORY_MAX_ENTRY_CHARS", 400),
		MemoryChunkChars:      getEnvInt("MEMORY_CHUNK_CHARS", 1200),
		MemoryChunkOverlap:    getEnvInt("MEMORY_CHUNK_OVERLAP", 200),
		MemoryMaxChunks:       getEnvInt("MEMORY_MAX_CHUNKS", 6),
		MemoryMinContentChars: getEnvInt("MEMORY_MIN_CONTENT_CHARS", 12),
		MemoryMaxContentBytes: getEnvInt("MEMORY_MAX_CONTENT_BYTES", 20000),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func buildPostgresURL() string {
	user := getEnv("POSTGRES_USER", "gavryn")
	password := getEnv("POSTGRES_PASSWORD", "gavryn")
	host := getEnv("POSTGRES_HOST", "localhost")
	port := getEnv("POSTGRES_PORT", "5432")
	database := getEnv("POSTGRES_DB", "gavryn")
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, database)
}
