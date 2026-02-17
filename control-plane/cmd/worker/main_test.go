package main

import (
	"errors"
	"testing"

	"github.com/nexus-rpc/sdk-go/nexus"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/llm"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store/postgres"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/workflows"
)

type stubWorker struct {
	runErr   error
	startErr error
}

func (s *stubWorker) RegisterWorkflow(w interface{}) {}

func (s *stubWorker) RegisterWorkflowWithOptions(w interface{}, options workflow.RegisterOptions) {}

func (s *stubWorker) RegisterDynamicWorkflow(w interface{}, options workflow.DynamicRegisterOptions) {
}

func (s *stubWorker) RegisterActivity(a interface{}) {}

func (s *stubWorker) RegisterActivityWithOptions(a interface{}, options activity.RegisterOptions) {}

func (s *stubWorker) RegisterDynamicActivity(a interface{}, options activity.DynamicRegisterOptions) {
}

func (s *stubWorker) RegisterNexusService(_ *nexus.Service) {}

func (s *stubWorker) Start() error {
	return s.startErr
}

func (s *stubWorker) Run(_ <-chan interface{}) error {
	return s.runErr
}

func (s *stubWorker) Stop() {}

func captureWorkerDeps() func() {
	origLoadConfig := loadConfig
	origDialTemporal := dialTemporal
	origNewStore := newStore
	origParseSecretsKey := parseSecretsKey
	origNewActivities := newActivities
	origNewWorker := newWorker
	origWorkerInterrupt := workerInterrupt

	return func() {
		loadConfig = origLoadConfig
		dialTemporal = origDialTemporal
		newStore = origNewStore
		parseSecretsKey = origParseSecretsKey
		newActivities = origNewActivities
		newWorker = origNewWorker
		workerInterrupt = origWorkerInterrupt
	}
}

func TestRunSuccess(t *testing.T) {
	restore := captureWorkerDeps()
	t.Cleanup(restore)

	loadConfig = func() (config.Config, error) {
		return config.Config{
			PostgresURL:     "postgres://example",
			TemporalAddress: "localhost:7233",
			ControlPlaneURL: "http://localhost:8080",
		}, nil
	}
	dialTemporal = func(_ client.Options) (client.Client, error) {
		return nil, nil
	}
	newStore = func(_ string) (*postgres.PostgresStore, error) {
		return &postgres.PostgresStore{}, nil
	}
	newActivities = func(_ *postgres.PostgresStore, _ llm.Config, _ []byte, _ string, _ string, _ ...workflows.RunActivitiesOption) *workflows.RunActivities {
		return &workflows.RunActivities{}
	}
	newWorker = func(_ client.Client, _ string, _ worker.Options) worker.Worker {
		return &stubWorker{}
	}
	workerInterrupt = func() <-chan interface{} {
		return make(chan interface{})
	}

	if err := run(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestRunConfigLoadFailure(t *testing.T) {
	restore := captureWorkerDeps()
	t.Cleanup(restore)

	loadConfig = func() (config.Config, error) {
		return config.Config{}, errors.New("config load failed")
	}

	if err := run(); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunTemporalClientFailure(t *testing.T) {
	restore := captureWorkerDeps()
	t.Cleanup(restore)

	loadConfig = func() (config.Config, error) {
		return config.Config{TemporalAddress: "localhost:7233"}, nil
	}
	dialTemporal = func(_ client.Options) (client.Client, error) {
		return nil, errors.New("temporal dial failed")
	}

	if err := run(); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunSecretsKeyParseFailure(t *testing.T) {
	restore := captureWorkerDeps()
	t.Cleanup(restore)

	loadConfig = func() (config.Config, error) {
		return config.Config{
			PostgresURL:     "postgres://example",
			TemporalAddress: "localhost:7233",
			LLMSecretsKey:   "bad-key",
		}, nil
	}
	dialTemporal = func(_ client.Options) (client.Client, error) {
		return nil, nil
	}
	newStore = func(_ string) (*postgres.PostgresStore, error) {
		return &postgres.PostgresStore{}, nil
	}
	parseSecretsKey = func(_ string) ([]byte, error) {
		return nil, errors.New("parse failed")
	}
	newWorker = func(_ client.Client, _ string, _ worker.Options) worker.Worker {
		return &stubWorker{}
	}
	workerInterrupt = func() <-chan interface{} {
		return make(chan interface{})
	}

	if err := run(); err == nil {
		t.Fatal("expected error, got nil")
	}
}
