package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"go.temporal.io/sdk/client"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/events"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store/postgres"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/workflows"
)

type stubServer struct {
	err error
}

func (s stubServer) Start(ctx context.Context, addr string) error {
	return s.err
}

func captureControlPlaneDeps() func() {
	origLoadConfig := loadConfig
	origNewBroker := newBroker
	origNewStore := newStore
	origEnsureBuiltins := ensureBuiltins
	origDialTemporal := dialTemporal
	origNewWorkflowService := newWorkflowService
	origNewServer := newServer
	origNotifyContext := notifyContext

	return func() {
		loadConfig = origLoadConfig
		newBroker = origNewBroker
		newStore = origNewStore
		ensureBuiltins = origEnsureBuiltins
		dialTemporal = origDialTemporal
		newWorkflowService = origNewWorkflowService
		newServer = origNewServer
		notifyContext = origNotifyContext
	}
}

func TestRunSuccess(t *testing.T) {
	restore := captureControlPlaneDeps()
	t.Cleanup(restore)

	loadConfig = func() (config.Config, error) {
		return config.Config{
			ControlPlanePort: "0",
			PostgresURL:      "postgres://example",
			TemporalAddress:  "localhost:7233",
		}, nil
	}
	newStore = func(_ string) (*postgres.PostgresStore, error) {
		return &postgres.PostgresStore{}, nil
	}
	calledEnsureBuiltins := false
	ensureBuiltins = func(_ context.Context, _ *postgres.PostgresStore) error {
		calledEnsureBuiltins = true
		return nil
	}
	dialTemporal = func(_ client.Options) (client.Client, error) {
		return nil, nil
	}
	newWorkflowService = func(_ client.Client, _ string) *workflows.Service {
		return nil
	}
	newServer = func(_ *postgres.PostgresStore, _ *events.Broker, _ *workflows.Service, _ config.Config) server {
		return stubServer{}
	}
	notifyContext = func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}

	if err := run(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !calledEnsureBuiltins {
		t.Fatal("expected builtin skills bootstrap to be called")
	}
}

func TestRunBuiltinBootstrapFailureIsNonFatal(t *testing.T) {
	restore := captureControlPlaneDeps()
	t.Cleanup(restore)

	loadConfig = func() (config.Config, error) {
		return config.Config{
			ControlPlanePort: "0",
			PostgresURL:      "postgres://example",
			TemporalAddress:  "localhost:7233",
		}, nil
	}
	newStore = func(_ string) (*postgres.PostgresStore, error) {
		return nil, nil
	}
	ensureBuiltins = func(_ context.Context, _ *postgres.PostgresStore) error {
		return errors.New("bootstrap failed")
	}
	dialTemporal = func(_ client.Options) (client.Client, error) {
		return nil, nil
	}
	newWorkflowService = func(_ client.Client, _ string) *workflows.Service {
		return nil
	}
	newServer = func(_ *postgres.PostgresStore, _ *events.Broker, _ *workflows.Service, _ config.Config) server {
		return stubServer{}
	}
	notifyContext = func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}

	if err := run(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestRunConfigLoadFailure(t *testing.T) {
	restore := captureControlPlaneDeps()
	t.Cleanup(restore)

	loadConfig = func() (config.Config, error) {
		return config.Config{}, errors.New("config load failed")
	}

	if err := run(); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunStoreInitFailure(t *testing.T) {
	restore := captureControlPlaneDeps()
	t.Cleanup(restore)

	loadConfig = func() (config.Config, error) {
		return config.Config{PostgresURL: "postgres://example"}, nil
	}
	newStore = func(_ string) (*postgres.PostgresStore, error) {
		return nil, errors.New("store init failed")
	}

	if err := run(); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunTemporalClientFailure(t *testing.T) {
	restore := captureControlPlaneDeps()
	t.Cleanup(restore)

	loadConfig = func() (config.Config, error) {
		return config.Config{
			PostgresURL:     "postgres://example",
			TemporalAddress: "localhost:7233",
		}, nil
	}
	newStore = func(_ string) (*postgres.PostgresStore, error) {
		return nil, nil
	}
	dialTemporal = func(_ client.Options) (client.Client, error) {
		return nil, errors.New("temporal dial failed")
	}

	if err := run(); err == nil {
		t.Fatal("expected error, got nil")
	}
}
