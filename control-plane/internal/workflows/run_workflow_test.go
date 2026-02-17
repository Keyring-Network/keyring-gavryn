package workflows

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	tests "go.temporal.io/sdk/testsuite"
)

type WorkflowTestSuite struct {
	suite.Suite
	testSuite *tests.WorkflowTestSuite
	env       *tests.TestWorkflowEnvironment
}

func (s *WorkflowTestSuite) SetupTest() {
	s.testSuite = &tests.WorkflowTestSuite{}
	s.env = s.testSuite.NewTestWorkflowEnvironment()
	s.env.RegisterWorkflow(RunWorkflow)
	s.env.RegisterActivityWithOptions(func(ctx context.Context, input PlanInput) (PlanOutput, error) {
		return PlanOutput{PlanID: "plan-test"}, nil
	}, activity.RegisterOptions{Name: "PlanExecution"})
	s.env.RegisterActivityWithOptions(func(ctx context.Context, input ExecuteInput) (ExecuteOutput, error) {
		return ExecuteOutput{PlanID: input.PlanID}, nil
	}, activity.RegisterOptions{Name: "ExecutePlan"})
	s.env.RegisterActivityWithOptions(func(ctx context.Context, input VerifyInput) (VerifyOutput, error) {
		return VerifyOutput{Status: "completed", CompletionReason: "verified_success"}, nil
	}, activity.RegisterOptions{Name: "VerifyExecution"})
	s.env.RegisterActivityWithOptions(func(ctx context.Context, input RunFailureInput) error {
		return nil
	}, activity.RegisterOptions{Name: "HandleRunFailure"})
}

func (s *WorkflowTestSuite) TearDownTest() {
	s.env.AssertExpectations(s.T())
}

func (s *WorkflowTestSuite) TestRunWorkflow_Success() {
	runID := "run-1"

	s.env.OnActivity("PlanExecution", mock.Anything, PlanInput{RunID: runID, Message: "hello"}).Return(PlanOutput{PlanID: "plan-1"}, nil).Once()
	s.env.OnActivity("ExecutePlan", mock.Anything, ExecuteInput{RunID: runID, Message: "hello", PlanID: "plan-1"}).Return(ExecuteOutput{PlanID: "plan-1"}, nil).Once()
	s.env.OnActivity("VerifyExecution", mock.Anything, VerifyInput{RunID: runID, Message: "hello", PlanID: "plan-1"}).Return(VerifyOutput{Status: "completed", CompletionReason: "verified_success"}, nil).Once()
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(MessageSignalName, "hello")
	}, time.Millisecond)
	s.env.RegisterDelayedCallback(func() {
		s.env.CancelWorkflow()
	}, 2*time.Millisecond)
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(MessageSignalName, "goodbye")
	}, 3*time.Millisecond)

	s.env.ExecuteWorkflow(RunWorkflow, RunInput{RunID: runID})
	s.True(s.env.IsWorkflowCompleted())

	var result RunResult
	err := s.env.GetWorkflowResult(&result)
	s.NoError(err)
	s.Equal("cancelled", result.Status)
}

func (s *WorkflowTestSuite) TestRunWorkflow_Cancellation() {
	s.env.RegisterDelayedCallback(func() {
		s.env.CancelWorkflow()
	}, time.Millisecond)
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(MessageSignalName, "ping")
	}, 2*time.Millisecond)

	s.env.ExecuteWorkflow(RunWorkflow, RunInput{RunID: "run-2"})
	s.True(s.env.IsWorkflowCompleted())

	var result RunResult
	err := s.env.GetWorkflowResult(&result)
	s.NoError(err)
	s.Equal("cancelled", result.Status)
}

func (s *WorkflowTestSuite) TestRunWorkflow_SignalHandling() {
	runID := "run-3"

	s.env.OnActivity("PlanExecution", mock.Anything, PlanInput{RunID: runID, Message: "ping"}).Return(PlanOutput{PlanID: "plan-2"}, nil).Once()
	s.env.OnActivity("ExecutePlan", mock.Anything, ExecuteInput{RunID: runID, Message: "ping", PlanID: "plan-2"}).Return(ExecuteOutput{PlanID: "plan-2"}, nil).Once()
	s.env.OnActivity("VerifyExecution", mock.Anything, VerifyInput{RunID: runID, Message: "ping", PlanID: "plan-2"}).Return(VerifyOutput{Status: "completed", CompletionReason: "verified_success"}, nil).Once()
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(MessageSignalName, "ping")
	}, time.Millisecond)
	s.env.RegisterDelayedCallback(func() {
		s.env.CancelWorkflow()
	}, 2*time.Millisecond)

	s.env.ExecuteWorkflow(RunWorkflow, RunInput{RunID: runID})
	s.True(s.env.IsWorkflowCompleted())
}

func (s *WorkflowTestSuite) TestRunWorkflow_Timeout() {
	s.env.SetTestTimeout(10 * time.Millisecond)
	s.env.ExecuteWorkflow(RunWorkflow, RunInput{RunID: "run-timeout"})

	err := s.env.GetWorkflowError()
	s.Error(err)

	var timeoutErr *temporal.TimeoutError
	s.True(errors.As(err, &timeoutErr))
}

func (s *WorkflowTestSuite) TestRunWorkflow_Retry() {
	runID := "run-retry"
	activityErr := errors.New("activity failed")

	s.env.OnActivity("PlanExecution", mock.Anything, PlanInput{RunID: runID, Message: "ping"}).Return(PlanOutput{PlanID: "plan-3"}, nil).Once()
	s.env.OnActivity("ExecutePlan", mock.Anything, ExecuteInput{RunID: runID, Message: "ping", PlanID: "plan-3"}).Return(ExecuteOutput{}, activityErr).Once()
	s.env.OnActivity("HandleRunFailure", mock.Anything, mock.MatchedBy(func(input RunFailureInput) bool {
		return input.RunID == runID &&
			strings.Contains(input.Error, "execution:") &&
			strings.Contains(input.Error, activityErr.Error())
	})).Return(nil).Once()
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(MessageSignalName, "ping")
	}, time.Millisecond)
	s.env.RegisterDelayedCallback(func() {
		s.env.CancelWorkflow()
	}, 2*time.Millisecond)

	s.env.ExecuteWorkflow(RunWorkflow, RunInput{RunID: runID})
	s.True(s.env.IsWorkflowCompleted())
}

func TestWorkflowTestSuite(t *testing.T) {
	suite.Run(t, new(WorkflowTestSuite))
}
