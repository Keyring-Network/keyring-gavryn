package main

import (
	"log"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/llm"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/secrets"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store/postgres"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/workflows"
)

var (
	loadConfig = func() (config.Config, error) {
		return config.Load(), nil
	}
	dialTemporal = client.Dial
	newStore     = func(conn string) (*postgres.PostgresStore, error) {
		return postgres.New(conn)
	}
	parseSecretsKey = secrets.ParseKey
	newActivities   = func(st *postgres.PostgresStore, cfg llm.Config, secretsKey []byte, controlPlaneURL string, toolRunnerURL string, opts ...workflows.RunActivitiesOption) *workflows.RunActivities {
		return workflows.NewRunActivities(st, cfg, secretsKey, controlPlaneURL, toolRunnerURL, opts...)
	}
	newWorker       = worker.New
	workerInterrupt = worker.InterruptCh
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	temporalClient, err := dialTemporal(client.Options{
		HostPort: cfg.TemporalAddress,
	})
	if err != nil {
		return err
	}
	if temporalClient != nil {
		defer temporalClient.Close()
	}

	store, err := newStore(cfg.PostgresURL)
	if err != nil {
		return err
	}

	var secretsKey []byte
	if cfg.LLMSecretsKey != "" {
		parsed, err := parseSecretsKey(cfg.LLMSecretsKey)
		if err != nil {
			return err
		}
		secretsKey = parsed
	}

	activities := newActivities(store, llm.Config{
		Mode:             cfg.LLMMode,
		Provider:         cfg.LLMProvider,
		Model:            cfg.LLMModel,
		BaseURL:          cfg.LLMBaseURL,
		FallbackProvider: cfg.LLMFallbackProvider,
		FallbackModel:    cfg.LLMFallbackModel,
		FallbackBaseURL:  cfg.LLMFallbackBaseURL,
		OpenAIAPIKey:     cfg.OpenAIAPIKey,
		OpenRouterAPIKey: cfg.OpenRouterAPIKey,
		OpenCodeAPIKey:   cfg.OpenCodeAPIKey,
		CodexAuthPath:    cfg.CodexAuthPath,
		CodexHome:        cfg.CodexHome,
	}, secretsKey, cfg.ControlPlaneURL, cfg.ToolRunnerURL, workflows.WithMemoryConfig(cfg.MemoryMaxResults, cfg.MemoryMaxEntryChars))

	w := newWorker(temporalClient, cfg.TemporalTaskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.RunWorkflow)
	w.RegisterActivity(activities)

	log.Println("Gavryn worker started")
	if err := w.Run(workerInterrupt()); err != nil {
		return err
	}

	return nil
}
