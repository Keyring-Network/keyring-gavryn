package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type RunInput struct {
	RunID   string
	Message string
}

type RunResult struct {
	Status string
}

func RunWorkflow(ctx workflow.Context, input RunInput) (RunResult, error) {
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 20 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	logger := workflow.GetLogger(ctx)
	messageCh := workflow.GetSignalChannel(ctx, MessageSignalName)

	for {
		selector := workflow.NewSelector(ctx)
		selector.AddReceive(messageCh, func(c workflow.ReceiveChannel, more bool) {
			var msg string
			c.Receive(ctx, &msg)
			logger.Info("received message", "message", msg)
			planResult := PlanOutput{}
			if err := workflow.ExecuteActivity(ctx, "PlanExecution", PlanInput{
				RunID:   input.RunID,
				Message: msg,
			}).Get(ctx, &planResult); err != nil {
				logger.Error("planning activity failed", "error", err)
				failureInput := RunFailureInput{
					RunID: input.RunID,
					Error: "planning: " + err.Error(),
				}
				if failureErr := workflow.ExecuteActivity(ctx, "HandleRunFailure", failureInput).Get(ctx, nil); failureErr != nil {
					logger.Error("failed to persist run failure event", "error", failureErr)
				}
				return
			}

			executeResult := ExecuteOutput{}
			if err := workflow.ExecuteActivity(ctx, "ExecutePlan", ExecuteInput{
				RunID:   input.RunID,
				Message: msg,
				PlanID:  planResult.PlanID,
			}).Get(ctx, &executeResult); err != nil {
				logger.Error("execution activity failed", "error", err)
				failureInput := RunFailureInput{
					RunID: input.RunID,
					Error: "execution: " + err.Error(),
				}
				if failureErr := workflow.ExecuteActivity(ctx, "HandleRunFailure", failureInput).Get(ctx, nil); failureErr != nil {
					logger.Error("failed to persist run failure event", "error", failureErr)
				}
				return
			}

			verifyResult := VerifyOutput{}
			if err := workflow.ExecuteActivity(ctx, "VerifyExecution", VerifyInput{
				RunID:   input.RunID,
				Message: msg,
				PlanID:  executeResult.PlanID,
			}).Get(ctx, &verifyResult); err != nil {
				logger.Error("verification activity failed", "error", err)
				failureInput := RunFailureInput{
					RunID: input.RunID,
					Error: "verification: " + err.Error(),
				}
				if failureErr := workflow.ExecuteActivity(ctx, "HandleRunFailure", failureInput).Get(ctx, nil); failureErr != nil {
					logger.Error("failed to persist run failure event", "error", failureErr)
				}
				return
			}
		})
		selector.Select(ctx)

		if ctx.Err() != nil {
			return RunResult{Status: "cancelled"}, nil
		}
	}

}
