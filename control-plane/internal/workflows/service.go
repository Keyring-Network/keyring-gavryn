package workflows

import (
	"context"
	"fmt"
	"strings"

	"go.temporal.io/sdk/client"
)

const (
	MessageSignalName = "message"
)

type Service struct {
	client    client.Client
	taskQueue string
}

func NewService(client client.Client, taskQueue string) *Service {
	if taskQueue == "" {
		taskQueue = "gavryn-runs"
	}
	return &Service{client: client, taskQueue: taskQueue}
}

func (s *Service) StartRun(ctx context.Context, runID string) error {
	options := client.StartWorkflowOptions{
		ID:        workflowID(runID),
		TaskQueue: s.taskQueue,
	}
	_, err := s.client.ExecuteWorkflow(ctx, options, RunWorkflow, RunInput{RunID: runID})
	return err
}

func (s *Service) SignalMessage(ctx context.Context, runID string, message string) error {
	return s.client.SignalWorkflow(ctx, workflowID(runID), "", MessageSignalName, message)
}

func (s *Service) ResumeRun(ctx context.Context, runID string, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Continue from checkpoint."
	}
	options := client.StartWorkflowOptions{
		ID:        workflowID(runID),
		TaskQueue: s.taskQueue,
	}
	_, err := s.client.SignalWithStartWorkflow(
		ctx,
		workflowID(runID),
		MessageSignalName,
		message,
		options,
		RunWorkflow,
		RunInput{RunID: runID},
	)
	return err
}

func (s *Service) CancelRun(ctx context.Context, runID string) error {
	return s.client.CancelWorkflow(ctx, workflowID(runID), "")
}

func workflowID(runID string) string {
	return fmt.Sprintf("run:%s", runID)
}
