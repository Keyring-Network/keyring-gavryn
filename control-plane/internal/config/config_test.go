package config

import (
	"os"
	"testing"
)

var allEnvKeys = []string{
	"CONTROL_PLANE_PORT",
	"CONTROL_PLANE_URL",
	"TOOL_RUNNER_URL",
	"POSTGRES_URL",
	"POSTGRES_USER",
	"POSTGRES_PASSWORD",
	"POSTGRES_DB",
	"POSTGRES_HOST",
	"POSTGRES_PORT",
	"TEMPORAL_ADDRESS",
	"TEMPORAL_TASK_QUEUE",
	"LLM_MODE",
	"LLM_PROVIDER",
	"LLM_MODEL",
	"LLM_BASE_URL",
	"LLM_FALLBACK_PROVIDER",
	"LLM_FALLBACK_MODEL",
	"LLM_FALLBACK_BASE_URL",
	"OPENAI_API_KEY",
	"OPENROUTER_API_KEY",
	"DISCORD_WEBHOOK_URL",
	"CODEX_AUTH_PATH",
	"CODEX_HOME",
	"LLM_SECRETS_KEY",
	"MEMORY_MAX_RESULTS",
	"MEMORY_MAX_ENTRY_CHARS",
	"MEMORY_CHUNK_CHARS",
	"MEMORY_CHUNK_OVERLAP",
	"MEMORY_MAX_CHUNKS",
	"MEMORY_MIN_CONTENT_CHARS",
	"MEMORY_MAX_CONTENT_BYTES",
}

func unsetAllEnv(keys []string) {
	for _, key := range keys {
		_ = os.Unsetenv(key)
	}
}

func TestLoad_AllDefaults(t *testing.T) {
	unsetAllEnv(allEnvKeys)

	cfg := Load()

	if cfg.ControlPlanePort != "8080" {
		t.Fatalf("ControlPlanePort = %q, want %q", cfg.ControlPlanePort, "8080")
	}
	if cfg.ControlPlaneURL != "http://localhost:8080" {
		t.Fatalf("ControlPlaneURL = %q, want %q", cfg.ControlPlaneURL, "http://localhost:8080")
	}
	if cfg.ToolRunnerURL != "http://localhost:8081" {
		t.Fatalf("ToolRunnerURL = %q, want %q", cfg.ToolRunnerURL, "http://localhost:8081")
	}
	if cfg.PostgresURL != "postgres://gavryn:gavryn@localhost:5432/gavryn?sslmode=disable" {
		t.Fatalf("PostgresURL = %q, want %q", cfg.PostgresURL, "postgres://gavryn:gavryn@localhost:5432/gavryn?sslmode=disable")
	}
	if cfg.TemporalAddress != "localhost:7233" {
		t.Fatalf("TemporalAddress = %q, want %q", cfg.TemporalAddress, "localhost:7233")
	}
	if cfg.TemporalTaskQueue != "gavryn-runs" {
		t.Fatalf("TemporalTaskQueue = %q, want %q", cfg.TemporalTaskQueue, "gavryn-runs")
	}
	if cfg.LLMMode != "remote" {
		t.Fatalf("LLMMode = %q, want %q", cfg.LLMMode, "remote")
	}
	if cfg.LLMProvider != "codex" {
		t.Fatalf("LLMProvider = %q, want %q", cfg.LLMProvider, "codex")
	}
	if cfg.LLMModel != "gpt-5.2-codex" {
		t.Fatalf("LLMModel = %q, want %q", cfg.LLMModel, "gpt-5.2-codex")
	}
	if cfg.LLMBaseURL != "" {
		t.Fatalf("LLMBaseURL = %q, want %q", cfg.LLMBaseURL, "")
	}
	if cfg.LLMFallbackProvider != "" {
		t.Fatalf("LLMFallbackProvider = %q, want %q", cfg.LLMFallbackProvider, "")
	}
	if cfg.LLMFallbackModel != "" {
		t.Fatalf("LLMFallbackModel = %q, want %q", cfg.LLMFallbackModel, "")
	}
	if cfg.LLMFallbackBaseURL != "" {
		t.Fatalf("LLMFallbackBaseURL = %q, want %q", cfg.LLMFallbackBaseURL, "")
	}
	if cfg.OpenAIAPIKey != "" {
		t.Fatalf("OpenAIAPIKey = %q, want %q", cfg.OpenAIAPIKey, "")
	}
	if cfg.OpenRouterAPIKey != "" {
		t.Fatalf("OpenRouterAPIKey = %q, want %q", cfg.OpenRouterAPIKey, "")
	}
	if cfg.DiscordWebhookURL != "" {
		t.Fatalf("DiscordWebhookURL = %q, want %q", cfg.DiscordWebhookURL, "")
	}
	if cfg.CodexAuthPath != "" {
		t.Fatalf("CodexAuthPath = %q, want %q", cfg.CodexAuthPath, "")
	}
	if cfg.CodexHome != "" {
		t.Fatalf("CodexHome = %q, want %q", cfg.CodexHome, "")
	}
	if cfg.LLMSecretsKey != "" {
		t.Fatalf("LLMSecretsKey = %q, want %q", cfg.LLMSecretsKey, "")
	}
	if cfg.MemoryMaxResults != 5 {
		t.Fatalf("MemoryMaxResults = %d, want %d", cfg.MemoryMaxResults, 5)
	}
	if cfg.MemoryMaxEntryChars != 400 {
		t.Fatalf("MemoryMaxEntryChars = %d, want %d", cfg.MemoryMaxEntryChars, 400)
	}
	if cfg.MemoryChunkChars != 1200 {
		t.Fatalf("MemoryChunkChars = %d, want %d", cfg.MemoryChunkChars, 1200)
	}
	if cfg.MemoryChunkOverlap != 200 {
		t.Fatalf("MemoryChunkOverlap = %d, want %d", cfg.MemoryChunkOverlap, 200)
	}
	if cfg.MemoryMaxChunks != 6 {
		t.Fatalf("MemoryMaxChunks = %d, want %d", cfg.MemoryMaxChunks, 6)
	}
	if cfg.MemoryMinContentChars != 12 {
		t.Fatalf("MemoryMinContentChars = %d, want %d", cfg.MemoryMinContentChars, 12)
	}
	if cfg.MemoryMaxContentBytes != 20000 {
		t.Fatalf("MemoryMaxContentBytes = %d, want %d", cfg.MemoryMaxContentBytes, 20000)
	}
}

func TestLoad_AllEnvVars(t *testing.T) {
	t.Setenv("CONTROL_PLANE_PORT", "9090")
	t.Setenv("CONTROL_PLANE_URL", "https://control-plane.example.test:9090")
	t.Setenv("TOOL_RUNNER_URL", "https://tool-runner.example.test:8081")
	t.Setenv("POSTGRES_URL", "postgres://user:pass@db.example.test:5432/testdb?sslmode=disable")
	t.Setenv("TEMPORAL_ADDRESS", "temporal.example.test:7233")
	t.Setenv("TEMPORAL_TASK_QUEUE", "gavryn-runs-test")
	t.Setenv("LLM_MODE", "local")
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("LLM_MODEL", "gpt-test-model")
	t.Setenv("LLM_BASE_URL", "https://llm.example.test")
	t.Setenv("LLM_FALLBACK_PROVIDER", "openrouter")
	t.Setenv("LLM_FALLBACK_MODEL", "anthropic/claude-3.5-sonnet")
	t.Setenv("LLM_FALLBACK_BASE_URL", "https://openrouter.example.test/v1")
	t.Setenv("OPENAI_API_KEY", "openai-key")
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")
	t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.example.test/webhook")
	t.Setenv("CODEX_AUTH_PATH", "/tmp/codex/auth.json")
	t.Setenv("CODEX_HOME", "/tmp/codex/home")
	t.Setenv("LLM_SECRETS_KEY", "secrets-key")
	t.Setenv("MEMORY_MAX_RESULTS", "9")
	t.Setenv("MEMORY_MAX_ENTRY_CHARS", "512")
	t.Setenv("MEMORY_CHUNK_CHARS", "1500")
	t.Setenv("MEMORY_CHUNK_OVERLAP", "250")
	t.Setenv("MEMORY_MAX_CHUNKS", "4")
	t.Setenv("MEMORY_MIN_CONTENT_CHARS", "8")
	t.Setenv("MEMORY_MAX_CONTENT_BYTES", "4096")

	cfg := Load()

	if cfg.ControlPlanePort != "9090" {
		t.Fatalf("ControlPlanePort = %q, want %q", cfg.ControlPlanePort, "9090")
	}
	if cfg.ControlPlaneURL != "https://control-plane.example.test:9090" {
		t.Fatalf("ControlPlaneURL = %q, want %q", cfg.ControlPlaneURL, "https://control-plane.example.test:9090")
	}
	if cfg.ToolRunnerURL != "https://tool-runner.example.test:8081" {
		t.Fatalf("ToolRunnerURL = %q, want %q", cfg.ToolRunnerURL, "https://tool-runner.example.test:8081")
	}
	if cfg.PostgresURL != "postgres://user:pass@db.example.test:5432/testdb?sslmode=disable" {
		t.Fatalf("PostgresURL = %q, want %q", cfg.PostgresURL, "postgres://user:pass@db.example.test:5432/testdb?sslmode=disable")
	}
	if cfg.TemporalAddress != "temporal.example.test:7233" {
		t.Fatalf("TemporalAddress = %q, want %q", cfg.TemporalAddress, "temporal.example.test:7233")
	}
	if cfg.TemporalTaskQueue != "gavryn-runs-test" {
		t.Fatalf("TemporalTaskQueue = %q, want %q", cfg.TemporalTaskQueue, "gavryn-runs-test")
	}
	if cfg.LLMMode != "local" {
		t.Fatalf("LLMMode = %q, want %q", cfg.LLMMode, "local")
	}
	if cfg.LLMProvider != "openai" {
		t.Fatalf("LLMProvider = %q, want %q", cfg.LLMProvider, "openai")
	}
	if cfg.LLMModel != "gpt-test-model" {
		t.Fatalf("LLMModel = %q, want %q", cfg.LLMModel, "gpt-test-model")
	}
	if cfg.LLMBaseURL != "https://llm.example.test" {
		t.Fatalf("LLMBaseURL = %q, want %q", cfg.LLMBaseURL, "https://llm.example.test")
	}
	if cfg.LLMFallbackProvider != "openrouter" {
		t.Fatalf("LLMFallbackProvider = %q, want %q", cfg.LLMFallbackProvider, "openrouter")
	}
	if cfg.LLMFallbackModel != "anthropic/claude-3.5-sonnet" {
		t.Fatalf("LLMFallbackModel = %q, want %q", cfg.LLMFallbackModel, "anthropic/claude-3.5-sonnet")
	}
	if cfg.LLMFallbackBaseURL != "https://openrouter.example.test/v1" {
		t.Fatalf("LLMFallbackBaseURL = %q, want %q", cfg.LLMFallbackBaseURL, "https://openrouter.example.test/v1")
	}
	if cfg.OpenAIAPIKey != "openai-key" {
		t.Fatalf("OpenAIAPIKey = %q, want %q", cfg.OpenAIAPIKey, "openai-key")
	}
	if cfg.OpenRouterAPIKey != "openrouter-key" {
		t.Fatalf("OpenRouterAPIKey = %q, want %q", cfg.OpenRouterAPIKey, "openrouter-key")
	}
	if cfg.DiscordWebhookURL != "https://discord.example.test/webhook" {
		t.Fatalf("DiscordWebhookURL = %q, want %q", cfg.DiscordWebhookURL, "https://discord.example.test/webhook")
	}
	if cfg.CodexAuthPath != "/tmp/codex/auth.json" {
		t.Fatalf("CodexAuthPath = %q, want %q", cfg.CodexAuthPath, "/tmp/codex/auth.json")
	}
	if cfg.CodexHome != "/tmp/codex/home" {
		t.Fatalf("CodexHome = %q, want %q", cfg.CodexHome, "/tmp/codex/home")
	}
	if cfg.LLMSecretsKey != "secrets-key" {
		t.Fatalf("LLMSecretsKey = %q, want %q", cfg.LLMSecretsKey, "secrets-key")
	}
	if cfg.MemoryMaxResults != 9 {
		t.Fatalf("MemoryMaxResults = %d, want %d", cfg.MemoryMaxResults, 9)
	}
	if cfg.MemoryMaxEntryChars != 512 {
		t.Fatalf("MemoryMaxEntryChars = %d, want %d", cfg.MemoryMaxEntryChars, 512)
	}
	if cfg.MemoryChunkChars != 1500 {
		t.Fatalf("MemoryChunkChars = %d, want %d", cfg.MemoryChunkChars, 1500)
	}
	if cfg.MemoryChunkOverlap != 250 {
		t.Fatalf("MemoryChunkOverlap = %d, want %d", cfg.MemoryChunkOverlap, 250)
	}
	if cfg.MemoryMaxChunks != 4 {
		t.Fatalf("MemoryMaxChunks = %d, want %d", cfg.MemoryMaxChunks, 4)
	}
	if cfg.MemoryMinContentChars != 8 {
		t.Fatalf("MemoryMinContentChars = %d, want %d", cfg.MemoryMinContentChars, 8)
	}
	if cfg.MemoryMaxContentBytes != 4096 {
		t.Fatalf("MemoryMaxContentBytes = %d, want %d", cfg.MemoryMaxContentBytes, 4096)
	}
}

func TestLoad_PartialEnvVars(t *testing.T) {
	t.Setenv("CONTROL_PLANE_PORT", "7070")
	t.Setenv("POSTGRES_USER", "partial")
	t.Setenv("POSTGRES_PASSWORD", "partial")
	t.Setenv("POSTGRES_DB", "partial")
	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_PORT", "5444")
	t.Setenv("LLM_PROVIDER", "openrouter")
	t.Setenv("LLM_BASE_URL", "https://partial-llm.example.test")
	t.Setenv("LLM_FALLBACK_PROVIDER", "openai")
	t.Setenv("LLM_FALLBACK_MODEL", "gpt-4.1-mini")
	t.Setenv("MEMORY_MAX_RESULTS", "not-a-number")

	cfg := Load()

	if cfg.ControlPlanePort != "7070" {
		t.Fatalf("ControlPlanePort = %q, want %q", cfg.ControlPlanePort, "7070")
	}
	if cfg.ControlPlaneURL != "http://localhost:7070" {
		t.Fatalf("ControlPlaneURL = %q, want %q", cfg.ControlPlaneURL, "http://localhost:7070")
	}
	if cfg.ToolRunnerURL != "http://localhost:8081" {
		t.Fatalf("ToolRunnerURL = %q, want %q", cfg.ToolRunnerURL, "http://localhost:8081")
	}
	if cfg.PostgresURL != "postgres://partial:partial@localhost:5444/partial?sslmode=disable" {
		t.Fatalf("PostgresURL = %q, want %q", cfg.PostgresURL, "postgres://partial:partial@localhost:5444/partial?sslmode=disable")
	}
	if cfg.TemporalAddress != "localhost:7233" {
		t.Fatalf("TemporalAddress = %q, want %q", cfg.TemporalAddress, "localhost:7233")
	}
	if cfg.TemporalTaskQueue != "gavryn-runs" {
		t.Fatalf("TemporalTaskQueue = %q, want %q", cfg.TemporalTaskQueue, "gavryn-runs")
	}
	if cfg.LLMMode != "remote" {
		t.Fatalf("LLMMode = %q, want %q", cfg.LLMMode, "remote")
	}
	if cfg.LLMProvider != "openrouter" {
		t.Fatalf("LLMProvider = %q, want %q", cfg.LLMProvider, "openrouter")
	}
	if cfg.LLMModel != "gpt-5.2-codex" {
		t.Fatalf("LLMModel = %q, want %q", cfg.LLMModel, "gpt-5.2-codex")
	}
	if cfg.LLMBaseURL != "https://partial-llm.example.test" {
		t.Fatalf("LLMBaseURL = %q, want %q", cfg.LLMBaseURL, "https://partial-llm.example.test")
	}
	if cfg.LLMFallbackProvider != "openai" {
		t.Fatalf("LLMFallbackProvider = %q, want %q", cfg.LLMFallbackProvider, "openai")
	}
	if cfg.LLMFallbackModel != "gpt-4.1-mini" {
		t.Fatalf("LLMFallbackModel = %q, want %q", cfg.LLMFallbackModel, "gpt-4.1-mini")
	}
	if cfg.LLMFallbackBaseURL != "" {
		t.Fatalf("LLMFallbackBaseURL = %q, want %q", cfg.LLMFallbackBaseURL, "")
	}
	if cfg.OpenAIAPIKey != "" {
		t.Fatalf("OpenAIAPIKey = %q, want %q", cfg.OpenAIAPIKey, "")
	}
	if cfg.OpenRouterAPIKey != "" {
		t.Fatalf("OpenRouterAPIKey = %q, want %q", cfg.OpenRouterAPIKey, "")
	}
	if cfg.DiscordWebhookURL != "" {
		t.Fatalf("DiscordWebhookURL = %q, want %q", cfg.DiscordWebhookURL, "")
	}
	if cfg.CodexAuthPath != "" {
		t.Fatalf("CodexAuthPath = %q, want %q", cfg.CodexAuthPath, "")
	}
	if cfg.CodexHome != "" {
		t.Fatalf("CodexHome = %q, want %q", cfg.CodexHome, "")
	}
	if cfg.LLMSecretsKey != "" {
		t.Fatalf("LLMSecretsKey = %q, want %q", cfg.LLMSecretsKey, "")
	}
	if cfg.MemoryMaxResults != 5 {
		t.Fatalf("MemoryMaxResults = %d, want %d", cfg.MemoryMaxResults, 5)
	}
}

func TestLoad_EmptyEnvVars(t *testing.T) {
	t.Setenv("CONTROL_PLANE_PORT", "")
	t.Setenv("CONTROL_PLANE_URL", "")
	t.Setenv("TOOL_RUNNER_URL", "")
	t.Setenv("POSTGRES_URL", "")
	t.Setenv("POSTGRES_USER", "")
	t.Setenv("POSTGRES_PASSWORD", "")
	t.Setenv("POSTGRES_DB", "")
	t.Setenv("POSTGRES_HOST", "")
	t.Setenv("POSTGRES_PORT", "")
	t.Setenv("TEMPORAL_ADDRESS", "")
	t.Setenv("TEMPORAL_TASK_QUEUE", "")
	t.Setenv("LLM_MODE", "")
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("DISCORD_WEBHOOK_URL", "")
	t.Setenv("CODEX_AUTH_PATH", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("LLM_SECRETS_KEY", "")
	t.Setenv("MEMORY_MAX_RESULTS", "")
	t.Setenv("MEMORY_MAX_ENTRY_CHARS", "")
	t.Setenv("MEMORY_CHUNK_CHARS", "")
	t.Setenv("MEMORY_CHUNK_OVERLAP", "")
	t.Setenv("MEMORY_MAX_CHUNKS", "")
	t.Setenv("MEMORY_MIN_CONTENT_CHARS", "")
	t.Setenv("MEMORY_MAX_CONTENT_BYTES", "")

	cfg := Load()

	if cfg.ControlPlanePort != "8080" {
		t.Fatalf("ControlPlanePort = %q, want %q", cfg.ControlPlanePort, "8080")
	}
	if cfg.ControlPlaneURL != "http://localhost:8080" {
		t.Fatalf("ControlPlaneURL = %q, want %q", cfg.ControlPlaneURL, "http://localhost:8080")
	}
	if cfg.ToolRunnerURL != "http://localhost:8081" {
		t.Fatalf("ToolRunnerURL = %q, want %q", cfg.ToolRunnerURL, "http://localhost:8081")
	}
	if cfg.PostgresURL != "postgres://gavryn:gavryn@localhost:5432/gavryn?sslmode=disable" {
		t.Fatalf("PostgresURL = %q, want %q", cfg.PostgresURL, "postgres://gavryn:gavryn@localhost:5432/gavryn?sslmode=disable")
	}
	if cfg.TemporalAddress != "localhost:7233" {
		t.Fatalf("TemporalAddress = %q, want %q", cfg.TemporalAddress, "localhost:7233")
	}
	if cfg.TemporalTaskQueue != "gavryn-runs" {
		t.Fatalf("TemporalTaskQueue = %q, want %q", cfg.TemporalTaskQueue, "gavryn-runs")
	}
	if cfg.LLMMode != "remote" {
		t.Fatalf("LLMMode = %q, want %q", cfg.LLMMode, "remote")
	}
	if cfg.LLMProvider != "codex" {
		t.Fatalf("LLMProvider = %q, want %q", cfg.LLMProvider, "codex")
	}
	if cfg.LLMModel != "gpt-5.2-codex" {
		t.Fatalf("LLMModel = %q, want %q", cfg.LLMModel, "gpt-5.2-codex")
	}
	if cfg.LLMBaseURL != "" {
		t.Fatalf("LLMBaseURL = %q, want %q", cfg.LLMBaseURL, "")
	}
	if cfg.OpenAIAPIKey != "" {
		t.Fatalf("OpenAIAPIKey = %q, want %q", cfg.OpenAIAPIKey, "")
	}
	if cfg.OpenRouterAPIKey != "" {
		t.Fatalf("OpenRouterAPIKey = %q, want %q", cfg.OpenRouterAPIKey, "")
	}
	if cfg.DiscordWebhookURL != "" {
		t.Fatalf("DiscordWebhookURL = %q, want %q", cfg.DiscordWebhookURL, "")
	}
	if cfg.CodexAuthPath != "" {
		t.Fatalf("CodexAuthPath = %q, want %q", cfg.CodexAuthPath, "")
	}
	if cfg.CodexHome != "" {
		t.Fatalf("CodexHome = %q, want %q", cfg.CodexHome, "")
	}
	if cfg.LLMSecretsKey != "" {
		t.Fatalf("LLMSecretsKey = %q, want %q", cfg.LLMSecretsKey, "")
	}
	if cfg.MemoryMaxResults != 5 {
		t.Fatalf("MemoryMaxResults = %d, want %d", cfg.MemoryMaxResults, 5)
	}
}

func TestGetEnv_WithValue(t *testing.T) {
	t.Setenv("CONFIG_TEST_KEY", "value")

	value := getEnv("CONFIG_TEST_KEY", "fallback")

	if value != "value" {
		t.Fatalf("getEnv returned %q, want %q", value, "value")
	}
}

func TestGetEnv_WithFallback(t *testing.T) {
	_ = os.Unsetenv("CONFIG_TEST_KEY")

	value := getEnv("CONFIG_TEST_KEY", "fallback")

	if value != "fallback" {
		t.Fatalf("getEnv returned %q, want %q", value, "fallback")
	}
}

func TestGetEnv_EmptyString(t *testing.T) {
	t.Setenv("CONFIG_TEST_KEY", "")

	value := getEnv("CONFIG_TEST_KEY", "fallback")

	if value != "fallback" {
		t.Fatalf("getEnv returned %q, want %q", value, "fallback")
	}
}
