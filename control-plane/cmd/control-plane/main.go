package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.temporal.io/sdk/client"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/api"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/events"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/skills"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store/postgres"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/workflows"
)

type server interface {
	Start(ctx context.Context, addr string) error
}

var (
	loadConfig = func() (config.Config, error) {
		return config.Load(), nil
	}
	newBroker = events.NewBroker
	newStore  = func(conn string) (*postgres.PostgresStore, error) {
		return postgres.New(conn)
	}
	ensureBuiltins = func(ctx context.Context, st *postgres.PostgresStore) error {
		return skills.EnsureBuiltins(ctx, st)
	}
	dialTemporal       = client.Dial
	newWorkflowService = workflows.NewService
	newServer          = func(store *postgres.PostgresStore, broker *events.Broker, workflows *workflows.Service, cfg config.Config) server {
		return api.NewServer(store, broker, workflows, cfg)
	}
	notifyContext = signal.NotifyContext
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
	ctx, cancel := notifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	broker := newBroker()
	store, err := newStore(cfg.PostgresURL)
	if err != nil {
		return err
	}
	if store != nil {
		if err := ensureBuiltins(ctx, store); err != nil {
			log.Printf("warning: failed to bootstrap built-in skills: %v", err)
		}
	}

	workflowClient, err := dialTemporal(client.Options{HostPort: cfg.TemporalAddress})
	if err != nil {
		return err
	}
	if workflowClient != nil {
		defer workflowClient.Close()
	}
	workflowService := newWorkflowService(workflowClient, cfg.TemporalTaskQueue)

	server := newServer(store, broker, workflowService, cfg)

	addr := fmt.Sprintf(":%s", cfg.ControlPlanePort)
	log.Printf("Gavryn control plane listening on %s", addr)
	if err := server.Start(ctx, addr); err != nil {
		return err
	}

	return nil
}
