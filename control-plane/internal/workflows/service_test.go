package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
)

func TestNewService(t *testing.T) {
	mockClient := mocks.NewClient(t)
	service := NewService(mockClient, "gavryn-runs")
	if service == nil {
		t.Fatal("expected service")
	}
}

func TestStartRun_Success(t *testing.T) {
	mockClient := mocks.NewClient(t)
	workflowRun := mocks.NewWorkflowRun(t)
	runID := "run-123"
	taskQueue := "gavryn-runs-test"

	mockClient.On(
		"ExecuteWorkflow",
		mock.Anything,
		mock.MatchedBy(func(opts client.StartWorkflowOptions) bool {
			return opts.ID == workflowID(runID) && opts.TaskQueue == taskQueue
		}),
		mock.Anything,
		RunInput{RunID: runID},
	).Return(workflowRun, nil)

	service := NewService(mockClient, taskQueue)
	err := service.StartRun(context.Background(), runID)
	require.NoError(t, err)
}

func TestStartRun_Error(t *testing.T) {
	mockClient := mocks.NewClient(t)
	runID := "run-err"
	expectedErr := errors.New("start failed")
	taskQueue := "gavryn-runs-test"

	mockClient.On(
		"ExecuteWorkflow",
		mock.Anything,
		mock.MatchedBy(func(opts client.StartWorkflowOptions) bool {
			return opts.ID == workflowID(runID) && opts.TaskQueue == taskQueue
		}),
		mock.Anything,
		RunInput{RunID: runID},
	).Return((*mocks.WorkflowRun)(nil), expectedErr)

	service := NewService(mockClient, taskQueue)
	err := service.StartRun(context.Background(), runID)
	require.ErrorIs(t, err, expectedErr)
}

func TestSignalMessage_Success(t *testing.T) {
	mockClient := mocks.NewClient(t)
	runID := "run-1"
	message := "hello"

	mockClient.On("SignalWorkflow", mock.Anything, workflowID(runID), "", MessageSignalName, message).
		Return(nil)

	service := NewService(mockClient, "gavryn-runs")
	err := service.SignalMessage(context.Background(), runID, message)
	require.NoError(t, err)
}

func TestSignalMessage_NotFound(t *testing.T) {
	mockClient := mocks.NewClient(t)
	runID := "missing"
	message := "hello"
	expectedErr := errors.New("not found")

	mockClient.On("SignalWorkflow", mock.Anything, workflowID(runID), "", MessageSignalName, message).
		Return(expectedErr)

	service := NewService(mockClient, "gavryn-runs")
	err := service.SignalMessage(context.Background(), runID, message)
	require.ErrorIs(t, err, expectedErr)
}

func TestCancelRun_Success(t *testing.T) {
	mockClient := mocks.NewClient(t)
	runID := "run-2"

	mockClient.On("CancelWorkflow", mock.Anything, workflowID(runID), "").Return(nil)

	service := NewService(mockClient, "gavryn-runs")
	err := service.CancelRun(context.Background(), runID)
	require.NoError(t, err)
}

func TestCancelRun_NotFound(t *testing.T) {
	mockClient := mocks.NewClient(t)
	runID := "missing"
	expectedErr := errors.New("not found")

	mockClient.On("CancelWorkflow", mock.Anything, workflowID(runID), "").Return(expectedErr)

	service := NewService(mockClient, "gavryn-runs")
	err := service.CancelRun(context.Background(), runID)
	require.ErrorIs(t, err, expectedErr)
}

func TestResumeRun_Success(t *testing.T) {
	mockClient := mocks.NewClient(t)
	workflowRun := mocks.NewWorkflowRun(t)
	runID := "run-resume"
	message := "continue from checkpoint"
	taskQueue := "gavryn-runs-test"

	mockClient.On(
		"SignalWithStartWorkflow",
		mock.Anything,
		workflowID(runID),
		MessageSignalName,
		message,
		mock.MatchedBy(func(opts client.StartWorkflowOptions) bool {
			return opts.ID == workflowID(runID) && opts.TaskQueue == taskQueue
		}),
		mock.Anything,
		RunInput{RunID: runID},
	).Return(workflowRun, nil)

	service := NewService(mockClient, taskQueue)
	err := service.ResumeRun(context.Background(), runID, message)
	require.NoError(t, err)
}

func TestResumeRun_Error(t *testing.T) {
	mockClient := mocks.NewClient(t)
	runID := "run-resume"
	taskQueue := "gavryn-runs-test"
	expectedErr := errors.New("resume failed")

	mockClient.On(
		"SignalWithStartWorkflow",
		mock.Anything,
		workflowID(runID),
		MessageSignalName,
		"Continue from checkpoint.",
		mock.MatchedBy(func(opts client.StartWorkflowOptions) bool {
			return opts.ID == workflowID(runID) && opts.TaskQueue == taskQueue
		}),
		mock.Anything,
		RunInput{RunID: runID},
	).Return((*mocks.WorkflowRun)(nil), expectedErr)

	service := NewService(mockClient, taskQueue)
	err := service.ResumeRun(context.Background(), runID, "")
	require.ErrorIs(t, err, expectedErr)
}
