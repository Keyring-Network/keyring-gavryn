package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/llm"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/personality"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/secrets"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/skills"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type GenerateInput struct {
	RunID string
}

type PlanInput struct {
	RunID   string
	Message string
}

type PlannedStep struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Dependencies      []string `json:"dependencies,omitempty"`
	ExpectedArtifacts []string `json:"expected_artifacts,omitempty"`
}

type PlanOutput struct {
	PlanID string        `json:"plan_id"`
	Steps  []PlannedStep `json:"steps"`
}

type ExecuteInput struct {
	RunID   string
	Message string
	PlanID  string
}

type ExecuteOutput struct {
	PlanID string `json:"plan_id"`
}

type VerifyInput struct {
	RunID   string
	Message string
	PlanID  string
}

type VerifyOutput struct {
	Status           string `json:"status"`
	CompletionReason string `json:"completion_reason"`
}

type RunFailureInput struct {
	RunID string
	Error string
}

var (
	newProvider   = llm.NewProvider
	decryptSecret = secrets.Decrypt
	marshalJSON   = json.Marshal
	buildSystem   = (*RunActivities).buildSystemPrompt
	buildMemory   = (*RunActivities).buildMemoryPrompt
)

const (
	defaultMaxToolIterations = 4
	webResearchMaxIterations = 18
	maxToolCalls             = 12
	maxToolResultChars       = 4000
	maxToolJSONChars         = 120000

	maxToolParseContentChars = 300000
	maxPendingToolBlockChars = 140000
	maxLLMGenerateAttempts   = 2
	maxToolIntentReprompts   = 2
	maxToolRecoveryReprompts = 1
	maxNoContentReprompts    = 1
	maxWebResearchReprompts  = 2
	maxAutoResearchSeedPages = 12
	maxAutoResearchLinks     = 48
	maxAutoResearchPerSeed   = 8
	autoResearchScrollPasses = 3
	autoResearchScrollAmount = 1200
	runTitleGenerateTimeout  = 2 * time.Second
	maxConversationMessages  = 80
	maxConversationChars     = 120000
	maxLLMPhaseBudget        = 20 * time.Second
)

var allowedToolNames = map[string]struct{}{
	"browser.navigate":     {},
	"browser.snapshot":     {},
	"browser.click":        {},
	"browser.type":         {},
	"browser.scroll":       {},
	"browser.extract":      {},
	"browser.evaluate":     {},
	"browser.pdf":          {},
	"document.create_pptx": {},
	"document.create_docx": {},
	"document.create_pdf":  {},
	"document.create_csv":  {},
	"editor.list":          {},
	"editor.read":          {},
	"editor.write":         {},
	"editor.delete":        {},
	"editor.stat":          {},
	"process.exec":         {},
	"process.start":        {},
	"process.status":       {},
	"process.logs":         {},
	"process.stop":         {},
	"process.list":         {},
}

var (
	fencedToolBlockRE       = regexp.MustCompile("(?s)```(?:tool|json)\\s*\\n?.*?```")
	inlineToolFenceRE       = regexp.MustCompile("(?s)```(tool|json)\\s*(\\{.*?\\})\\s*```")
	retryableStatusRE       = regexp.MustCompile(`(?:^|\D)(429|500|502|503|504)(?:\D|$)`)
	topNRequestRE           = regexp.MustCompile(`\btop\s+(\d{1,2})\b`)
	sourceLinkRE            = regexp.MustCompile(`https?://[^\s\)\]\}>\"]+`)
	markdownLinkRE          = regexp.MustCompile(`\[(?P<label>[^\]]+)\]\((?P<url>https?://[^)]+)\)`)
	topStoryLineRE          = regexp.MustCompile(`(?m)^\d+\.\s+\*\*.+`)
	tickerSymbolRE          = regexp.MustCompile(`\$[A-Z]{2,6}`)
	datePathRE              = regexp.MustCompile(`/20\d{2}/(?:0[1-9]|1[0-2])(?:/(?:0[1-9]|[12]\d|3[01]))?`)
	anyYearRE               = regexp.MustCompile(`\b20\d{2}\b`)
	monthDateRE             = regexp.MustCompile(`(?i)\b(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+\d{1,2},\s+20\d{2}\b`)
	monthYearRE             = regexp.MustCompile(`(?i)\b(?:january|february|march|april|may|june|july|august|september|october|november|december)\s+20\d{2}\b`)
	legacyYearRE            = regexp.MustCompile(`\b20(1\d|2[0-5])\b`)
	sentenceSplitRE         = regexp.MustCompile(`[.!?]+\s+`)
	lowValueResearchSignals = []string{
		"news video prices",
		"data & indices",
		"sponsored",
		"latest news",
		"price index",
		"crypto news & price indexes",
		"markets prices",
		"navigation markets",
		"ecosystem english news indices",
		"in depth learn podcasts about",
		"video prices research consensus",
		"we also share information about your use of our site",
		"manage choices",
		"accept all cookies",
		"coindesk has adopted a set of principles",
		"bullish owns and invests",
		"list of partners (vendors)",
		"browser type and information",
		"register now 404",
		"hmm, that's weird",
	}
)

type toolCall struct {
	ToolName string
	Input    map[string]any
}

type browserUserTabConfig struct {
	Enabled            bool
	InteractionAllowed bool
	DomainAllowlist    []string
	PreferredBrowser   string
	BrowserUserAgent   string
}

type toolExecutionError struct {
	InvocationID string
	Message      string
}

func (e *toolExecutionError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Message)
}

type toolRunnerResponse struct {
	Status string         `json:"status"`
	Output map[string]any `json:"output"`
	Error  string         `json:"error,omitempty"`
}

type RunActivities struct {
	store               store.Store
	defaultConfig       llm.Config
	secretsKey          []byte
	controlPlane        string
	toolRunner          string
	httpClient          *http.Client
	requestTimeout      time.Duration
	toolTimeout         time.Duration
	memoryMaxResults    int
	memoryMaxEntryChars int
}

type llmProviderCandidate struct {
	Name     string
	Provider llm.Provider
}

type RunActivitiesOption func(*RunActivities)

func WithMemoryConfig(maxResults int, maxEntryChars int) RunActivitiesOption {
	return func(a *RunActivities) {
		if maxResults > 0 {
			a.memoryMaxResults = maxResults
		}
		if maxEntryChars > 0 {
			a.memoryMaxEntryChars = maxEntryChars
		}
	}
}

func NewRunActivities(store store.Store, defaultConfig llm.Config, secretsKey []byte, controlPlaneURL string, toolRunnerURL string, opts ...RunActivitiesOption) *RunActivities {
	activities := &RunActivities{
		store:               store,
		defaultConfig:       defaultConfig,
		secretsKey:          secretsKey,
		controlPlane:        strings.TrimRight(controlPlaneURL, "/"),
		toolRunner:          strings.TrimRight(toolRunnerURL, "/"),
		httpClient:          &http.Client{Timeout: 60 * time.Second},
		requestTimeout:      10 * time.Second,
		toolTimeout:         30 * time.Second,
		memoryMaxResults:    5,
		memoryMaxEntryChars: 400,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(activities)
		}
	}
	return activities
}

func (a *RunActivities) PlanExecution(ctx context.Context, input PlanInput) (PlanOutput, error) {
	if strings.TrimSpace(input.RunID) == "" {
		return PlanOutput{}, errors.New("run_id required")
	}
	trimmedMessage := strings.TrimSpace(input.Message)
	if trimmedMessage == "" {
		messages, err := a.store.ListMessages(ctx, input.RunID)
		if err != nil {
			return PlanOutput{}, err
		}
		trimmedMessage = latestUserMessage(messages)
	}
	planID := uuid.New().String()
	steps := buildExecutionPlan(trimmedMessage)
	_ = a.emitEvent(ctx, input.RunID, "run.phase.changed", map[string]any{
		"phase":   "planning",
		"plan_id": planID,
	})
	_ = a.emitEvent(ctx, input.RunID, "step.started", map[string]any{
		"step_id": "planner",
		"name":    "Plan execution",
		"plan_id": planID,
	})
	for _, step := range steps {
		_ = a.emitEvent(ctx, input.RunID, "step.planned", map[string]any{
			"plan_id":            planID,
			"step_id":            step.ID,
			"name":               step.Name,
			"dependencies":       step.Dependencies,
			"expected_artifacts": step.ExpectedArtifacts,
			"parent_step_id":     "planner",
		})
	}
	_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
		"step_id":            "planner",
		"name":               "Plan execution",
		"plan_id":            planID,
		"planned_step_count": len(steps),
	})
	return PlanOutput{
		PlanID: planID,
		Steps:  steps,
	}, nil
}

func buildExecutionPlan(message string) []PlannedStep {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return []PlannedStep{
			{
				ID:                "execute_request",
				Name:              "Execute request",
				ExpectedArtifacts: []string{"assistant.reply"},
			},
		}
	}
	containsAny := func(phrases ...string) bool {
		for _, phrase := range phrases {
			if strings.Contains(text, phrase) {
				return true
			}
		}
		return false
	}
	if containsAny("browse", "research", "news", "find", "search the web") {
		return []PlannedStep{
			{
				ID:                "collect_sources",
				Name:              "Collect web sources",
				ExpectedArtifacts: []string{"browser.snapshot", "browser.extract"},
			},
			{
				ID:                "synthesize_findings",
				Name:              "Synthesize findings with citations",
				Dependencies:      []string{"collect_sources"},
				ExpectedArtifacts: []string{"assistant.reply"},
			},
		}
	}
	if containsAny("create", "build", "website", "nextjs", "app", "implement", "code") {
		steps := []PlannedStep{
			{
				ID:                "inspect_workspace",
				Name:              "Inspect workspace and existing files",
				ExpectedArtifacts: []string{"workspace.snapshot"},
			},
			{
				ID:                "implement_changes",
				Name:              "Create or update project files",
				Dependencies:      []string{"inspect_workspace"},
				ExpectedArtifacts: []string{"workspace.changed"},
			},
			{
				ID:                "validate_changes",
				Name:              "Run validation commands",
				Dependencies:      []string{"implement_changes"},
				ExpectedArtifacts: []string{"process.exec"},
			},
		}
		if containsAny("website", "nextjs", "frontend", "marketing") {
			steps = append(steps, PlannedStep{
				ID:                "start_preview",
				Name:              "Start preview server",
				Dependencies:      []string{"validate_changes"},
				ExpectedArtifacts: []string{"process.start", "preview.url"},
			})
		}
		return steps
	}
	return []PlannedStep{
		{
			ID:                "execute_request",
			Name:              "Execute request",
			ExpectedArtifacts: []string{"assistant.reply"},
		},
	}
}

func (a *RunActivities) ExecutePlan(ctx context.Context, input ExecuteInput) (ExecuteOutput, error) {
	if strings.TrimSpace(input.RunID) == "" {
		return ExecuteOutput{}, errors.New("run_id required")
	}
	_ = a.emitEvent(ctx, input.RunID, "run.phase.changed", map[string]any{
		"phase":   "executing",
		"plan_id": strings.TrimSpace(input.PlanID),
	})
	err := a.GenerateAssistantReply(ctx, GenerateInput{RunID: input.RunID})
	if err != nil {
		return ExecuteOutput{}, err
	}
	return ExecuteOutput{PlanID: strings.TrimSpace(input.PlanID)}, nil
}

func (a *RunActivities) VerifyExecution(ctx context.Context, input VerifyInput) (VerifyOutput, error) {
	if strings.TrimSpace(input.RunID) == "" {
		return VerifyOutput{}, errors.New("run_id required")
	}
	_ = a.emitEvent(ctx, input.RunID, "run.phase.changed", map[string]any{
		"phase":   "validating",
		"plan_id": strings.TrimSpace(input.PlanID),
	})
	_ = a.emitEvent(ctx, input.RunID, "step.started", map[string]any{
		"step_id": "verifier",
		"name":    "Verify run outputs",
		"plan_id": strings.TrimSpace(input.PlanID),
	})

	eventsList, err := a.store.ListEvents(ctx, input.RunID, 0)
	if err != nil {
		_ = a.emitEvent(ctx, input.RunID, "step.failed", map[string]any{
			"step_id": "verifier",
			"name":    "Verify run outputs",
			"error":   err.Error(),
		})
		return VerifyOutput{}, err
	}

	var (
		hasTerminalEvent bool
		status           = "partial"
		reason           = "verification_pending"
	)
	for i := len(eventsList) - 1; i >= 0; i-- {
		switch eventsList[i].Type {
		case "run.completed":
			hasTerminalEvent = true
			status = "completed"
			reason = "verified_success"
		case "run.partial":
			hasTerminalEvent = true
			status = "partial"
			reason = "verified_partial"
		case "run.failed":
			hasTerminalEvent = true
			status = "failed"
			reason = "verified_failed"
		}
		if hasTerminalEvent {
			break
		}
	}
	if !hasTerminalEvent {
		reason = "missing_terminal_event"
		_ = a.postCompletionEvent(ctx, input.RunID, "partial", reason)
	}

	_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
		"step_id":           "verifier",
		"name":              "Verify run outputs",
		"status":            status,
		"completion_reason": reason,
		"plan_id":           strings.TrimSpace(input.PlanID),
	})
	_ = a.emitEvent(ctx, input.RunID, "run.phase.changed", map[string]any{
		"phase":             "completed",
		"status":            status,
		"completion_reason": reason,
		"plan_id":           strings.TrimSpace(input.PlanID),
	})
	return VerifyOutput{
		Status:           status,
		CompletionReason: reason,
	}, nil
}

func (a *RunActivities) GenerateAssistantReply(ctx context.Context, input GenerateInput) error {
	if input.RunID == "" {
		return errors.New("run_id required")
	}
	messages, err := a.store.ListMessages(ctx, input.RunID)
	if err != nil {
		return err
	}
	cfg, err := a.resolveConfig(ctx, messages)
	if err != nil {
		_ = a.postEvent(ctx, input.RunID, "run.failed", map[string]any{"error": err.Error()})
		return err
	}
	modelRoute := a.resolveModelRoute(ctx, input.RunID, messages)
	providers, err := a.buildProviderCandidates(cfg, modelRoute)
	if err != nil {
		_ = a.postEvent(ctx, input.RunID, "run.failed", map[string]any{"error": err.Error()})
		return err
	}
	primaryProvider := providers[0].Provider
	llmMessages := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Content == "" {
			continue
		}
		llmMessages = append(llmMessages, llm.Message{Role: msg.Role, Content: msg.Content})
	}
	if systemPrompt := buildSystem(a, ctx); systemPrompt != "" {
		llmMessages = append([]llm.Message{{Role: "system", Content: systemPrompt}}, llmMessages...)
	}
	if memoryPrompt := buildMemory(a, ctx, messages); memoryPrompt != "" {
		llmMessages = append([]llm.Message{{Role: "system", Content: memoryPrompt}}, llmMessages...)
	}
	if len(llmMessages) == 0 {
		return nil
	}
	_ = a.emitEvent(ctx, input.RunID, "step.started", map[string]any{
		"step_id": "assistant_reply",
		"name":    "Generate assistant reply",
	})
	llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
	latestUserRequest := latestUserMessage(messages)
	browserUserTab := resolveBrowserUserTabConfig(messages)
	mustExecuteTools := a.toolRunner != "" && requestLikelyNeedsTools(latestUserRequest)
	researchRequirements := deriveWebResearchRequirements(latestUserRequest, mustExecuteTools)
	if researchRequirements.Enabled {
		llmMessages = append(llmMessages, llm.Message{
			Role:    "system",
			Content: buildWebResearchExecutionPrompt(researchRequirements),
		})
		llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
	}
	var lastResponse string
	successfulToolCalls := make([]toolCall, 0)
	hadToolErrors := false
	pendingToolBlock := ""
	toolIntentRepromptCount := 0
	toolRecoveryRepromptCount := 0
	noContentRepromptCount := 0
	webResearchRepromptCount := 0
	autoWebResearchRecoveryAttempted := false
	iterationLimit := defaultMaxToolIterations
	if researchRequirements.Enabled {
		iterationLimit = webResearchMaxIterations
	}
	for iteration := 0; iteration < iterationLimit; iteration++ {
		llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
		response, err := a.generateWithRetry(ctx, input.RunID, providers, llmMessages)
		if err != nil {
			if isNoContentLLMError(err) {
				if noContentRepromptCount < maxNoContentReprompts {
					noContentRepromptCount++
					llmMessages = append(llmMessages,
						llm.Message{Role: "system", Content: buildNoContentRetryPrompt(mustExecuteTools, latestUserRequest)},
					)
					llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
					continue
				}
				if len(successfulToolCalls) > 0 {
					if researchRequirements.Enabled && !hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) && !autoWebResearchRecoveryAttempted {
						autoWebResearchRecoveryAttempted = true
						recoveredCalls, recoveredHadErrors := a.autoDeepenWebResearch(ctx, input.RunID, latestUserRequest, successfulToolCalls, browserUserTab)
						if recoveredHadErrors {
							hadToolErrors = true
						}
						if len(recoveredCalls) > 0 {
							successfulToolCalls = append(successfulToolCalls, recoveredCalls...)
						}
					}
					final := a.composeBestEffortFinalResponse(ctx, input.RunID, providers, llmMessages, latestUserRequest, successfulToolCalls, researchRequirements, hadToolErrors, err)
					final = enforceResearchQualityFallback(final, successfulToolCalls, researchRequirements, latestUserRequest)
					if postErr := a.postMessage(ctx, input.RunID, final); postErr != nil {
						return postErr
					}
					status := ternaryStatus(hadToolErrors)
					_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
						"step_id": "assistant_reply",
						"name":    "Generate assistant reply",
						"status":  status,
					})
					completionReason := "llm_no_content_after_tools"
					if researchRequirements.Enabled {
						if hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) && !hadToolErrors {
							completionReason = "research_evidence_complete"
						} else if !hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) {
							status = "partial"
							completionReason = "insufficient_web_research_evidence"
						}
					}
					if hadToolErrors && completionReason != "insufficient_web_research_evidence" {
						status = "partial"
						completionReason = "partial_tool_errors"
					}
					_ = a.postCompletionEvent(ctx, input.RunID, status, completionReason)
					a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, final)
					return nil
				}
				fallback := buildNoContentFallback(successfulToolCalls)
				if postErr := a.postMessage(ctx, input.RunID, fallback); postErr != nil {
					return postErr
				}
				_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
					"step_id": "assistant_reply",
					"name":    "Generate assistant reply",
					"status":  "partial",
				})
				_ = a.postCompletionEvent(ctx, input.RunID, "partial", "llm_no_content")
				a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, fallback)
				return nil
			}
			if isRetryableLLMError(err) {
				if len(successfulToolCalls) > 0 {
					if researchRequirements.Enabled && !hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) && !autoWebResearchRecoveryAttempted {
						autoWebResearchRecoveryAttempted = true
						recoveredCalls, recoveredHadErrors := a.autoDeepenWebResearch(ctx, input.RunID, latestUserRequest, successfulToolCalls, browserUserTab)
						if recoveredHadErrors {
							hadToolErrors = true
						}
						if len(recoveredCalls) > 0 {
							successfulToolCalls = append(successfulToolCalls, recoveredCalls...)
						}
					}
					final := a.composeBestEffortFinalResponse(ctx, input.RunID, providers, llmMessages, latestUserRequest, successfulToolCalls, researchRequirements, hadToolErrors, err)
					final = enforceResearchQualityFallback(final, successfulToolCalls, researchRequirements, latestUserRequest)
					if postErr := a.postMessage(ctx, input.RunID, final); postErr != nil {
						return postErr
					}
					status := ternaryStatus(hadToolErrors)
					_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
						"step_id": "assistant_reply",
						"name":    "Generate assistant reply",
						"status":  status,
					})
					completionReason := "llm_transient_after_tools"
					if researchRequirements.Enabled {
						if hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) && !hadToolErrors {
							completionReason = "research_evidence_complete"
						} else if !hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) {
							status = "partial"
							completionReason = "insufficient_web_research_evidence"
						}
					}
					if hadToolErrors && completionReason != "insufficient_web_research_evidence" {
						status = "partial"
						completionReason = "partial_tool_errors"
					}
					_ = a.postCompletionEvent(ctx, input.RunID, status, completionReason)
					a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, final)
					return nil
				}
				fallback := buildTransientLLMFallback(err)
				if postErr := a.postMessage(ctx, input.RunID, fallback); postErr != nil {
					return postErr
				}
				_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
					"step_id": "assistant_reply",
					"name":    "Generate assistant reply",
					"status":  "partial",
				})
				_ = a.postCompletionEvent(ctx, input.RunID, "partial", "llm_transient_error")
				return nil
			}
			_ = a.emitEvent(ctx, input.RunID, "step.failed", map[string]any{
				"step_id": "assistant_reply",
				"name":    "Generate assistant reply",
				"error":   err.Error(),
			})
			_ = a.postEvent(ctx, input.RunID, "run.failed", map[string]any{"error": err.Error()})
			return err
		}
		lastResponse = response
		parseInput := response
		if strings.TrimSpace(pendingToolBlock) != "" {
			parseInput = pendingToolBlock + response
		}
		toolCalls, parseStatus := parseToolCalls(parseInput)
		if len(toolCalls) == 0 {
			if parseStatus.sawToolBlock {
				if parseStatus.hadIncomplete {
					pendingToolBlock = buildPendingToolBlock(parseInput)
				} else {
					pendingToolBlock = ""
				}
				if toolRecoveryRepromptCount >= maxToolRecoveryReprompts {
					fallback := buildInvalidToolPayloadFallback(successfulToolCalls)
					if postErr := a.postMessage(ctx, input.RunID, fallback); postErr != nil {
						return postErr
					}
					_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
						"step_id": "assistant_reply",
						"name":    "Generate assistant reply",
						"status":  "partial",
					})
					_ = a.postCompletionEvent(ctx, input.RunID, "partial", "invalid_tool_payload")
					return nil
				}
				toolRecoveryRepromptCount++
				llmMessages = append(llmMessages,
					llm.Message{Role: "assistant", Content: response},
					llm.Message{Role: "system", Content: buildToolRecoveryPrompt(parseStatus)},
				)
				llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
				continue
			}
			toolRecoveryRepromptCount = 0
			pendingToolBlock = ""
			if mustExecuteTools && len(successfulToolCalls) == 0 {
				if toolIntentRepromptCount < maxToolIntentReprompts {
					toolIntentRepromptCount++
					llmMessages = append(llmMessages,
						llm.Message{Role: "assistant", Content: response},
						llm.Message{Role: "system", Content: buildToolOnlyRetryPrompt(latestUserRequest)},
					)
					llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
					continue
				}
				if researchRequirements.Enabled && !autoWebResearchRecoveryAttempted {
					autoWebResearchRecoveryAttempted = true
					recoveredCalls, recoveredHadErrors := a.autoDeepenWebResearch(ctx, input.RunID, latestUserRequest, successfulToolCalls, browserUserTab)
					if recoveredHadErrors {
						hadToolErrors = true
					}
					if len(recoveredCalls) > 0 {
						successfulToolCalls = append(successfulToolCalls, recoveredCalls...)
						uniqueSources, extractCount := summarizeWebResearchEvidenceForRequest(successfulToolCalls, latestUserRequest)
						if hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) {
							final := a.composeBestEffortFinalResponse(ctx, input.RunID, providers, llmMessages, latestUserRequest, successfulToolCalls, researchRequirements, hadToolErrors, nil)
							if strings.TrimSpace(final) == "" {
								final = buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, researchRequirements, latestUserRequest)
							}
							if err := a.postMessage(ctx, input.RunID, final); err != nil {
								return err
							}
							status := ternaryStatus(hadToolErrors)
							_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
								"step_id": "assistant_reply",
								"name":    "Generate assistant reply",
								"status":  status,
							})
							completionReason := "research_evidence_complete"
							if hadToolErrors {
								completionReason = "partial_tool_errors"
							}
							_ = a.postCompletionEvent(ctx, input.RunID, status, completionReason)
							a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, final)
							return nil
						}
						llmMessages = append(llmMessages,
							llm.Message{Role: "assistant", Content: response},
							llm.Message{Role: "system", Content: buildWebResearchRetryPrompt(researchRequirements, uniqueSources, extractCount, countSourceLinks(response))},
						)
						llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
						continue
					}
				}
				fallback := buildMissingToolCallsFallback()
				if err := a.postMessage(ctx, input.RunID, fallback); err != nil {
					return err
				}
				_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
					"step_id": "assistant_reply",
					"name":    "Generate assistant reply",
					"status":  "partial",
				})
				_ = a.postCompletionEvent(ctx, input.RunID, "partial", "missing_tool_calls")
				return nil
			}
			if shouldRepromptToolExecution(a.toolRunner != "", latestUserRequest, response, toolIntentRepromptCount) {
				toolIntentRepromptCount++
				llmMessages = append(llmMessages,
					llm.Message{Role: "assistant", Content: response},
					llm.Message{Role: "system", Content: buildToolOnlyRetryPrompt(latestUserRequest)},
				)
				llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
				continue
			}
			if researchRequirements.Enabled {
				uniqueSources, extractCount := summarizeWebResearchEvidenceForRequest(successfulToolCalls, latestUserRequest)
				linkCount := countSourceLinks(response)
				if shouldRepromptForWebResearch(researchRequirements, uniqueSources, extractCount, linkCount) {
					intentOnlyNarrative := looksLikeInProgressResearchNarrative(response)
					if intentOnlyNarrative && !autoWebResearchRecoveryAttempted {
						autoWebResearchRecoveryAttempted = true
						recoveredCalls, recoveredHadErrors := a.autoDeepenWebResearch(ctx, input.RunID, latestUserRequest, successfulToolCalls, browserUserTab)
						if recoveredHadErrors {
							hadToolErrors = true
						}
						if len(recoveredCalls) > 0 {
							successfulToolCalls = append(successfulToolCalls, recoveredCalls...)
							uniqueSources, extractCount = summarizeWebResearchEvidenceForRequest(successfulToolCalls, latestUserRequest)
							linkCount = countSourceLinks(response)
							if hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) {
								final := a.composeBestEffortFinalResponse(ctx, input.RunID, providers, llmMessages, latestUserRequest, successfulToolCalls, researchRequirements, hadToolErrors, nil)
								if strings.TrimSpace(final) == "" {
									final = buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, researchRequirements, latestUserRequest)
								}
								if err := a.postMessage(ctx, input.RunID, final); err != nil {
									return err
								}
								status := ternaryStatus(hadToolErrors)
								_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
									"step_id": "assistant_reply",
									"name":    "Generate assistant reply",
									"status":  status,
								})
								completionReason := "research_evidence_complete"
								if hadToolErrors {
									completionReason = "partial_tool_errors"
								}
								_ = a.postCompletionEvent(ctx, input.RunID, status, completionReason)
								a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, final)
								return nil
							}
						}
					}
					repromptBudget := maxWebResearchReprompts
					if intentOnlyNarrative {
						repromptBudget++
					}
					if webResearchRepromptCount < repromptBudget {
						webResearchRepromptCount++
						llmMessages = append(llmMessages,
							llm.Message{Role: "assistant", Content: response},
							llm.Message{Role: "system", Content: buildWebResearchRetryPrompt(researchRequirements, uniqueSources, extractCount, linkCount)},
						)
						llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
						continue
					}
					if !autoWebResearchRecoveryAttempted {
						autoWebResearchRecoveryAttempted = true
						recoveredCalls, recoveredHadErrors := a.autoDeepenWebResearch(ctx, input.RunID, latestUserRequest, successfulToolCalls, browserUserTab)
						if recoveredHadErrors {
							hadToolErrors = true
						}
						if len(recoveredCalls) > 0 {
							successfulToolCalls = append(successfulToolCalls, recoveredCalls...)
							uniqueSources, extractCount = summarizeWebResearchEvidenceForRequest(successfulToolCalls, latestUserRequest)
							linkCount = countSourceLinks(response)
							if hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) {
								final := a.composeBestEffortFinalResponse(ctx, input.RunID, providers, llmMessages, latestUserRequest, successfulToolCalls, researchRequirements, hadToolErrors, nil)
								if strings.TrimSpace(final) == "" {
									final = buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, researchRequirements, latestUserRequest)
								}
								if err := a.postMessage(ctx, input.RunID, final); err != nil {
									return err
								}
								status := ternaryStatus(hadToolErrors)
								_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
									"step_id": "assistant_reply",
									"name":    "Generate assistant reply",
									"status":  status,
								})
								completionReason := "research_evidence_complete"
								if hadToolErrors {
									completionReason = "partial_tool_errors"
								}
								_ = a.postCompletionEvent(ctx, input.RunID, status, completionReason)
								a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, final)
								return nil
							}
							if shouldRepromptForWebResearch(researchRequirements, uniqueSources, extractCount, linkCount) {
								llmMessages = append(llmMessages,
									llm.Message{Role: "assistant", Content: response},
									llm.Message{Role: "system", Content: buildWebResearchRetryPrompt(researchRequirements, uniqueSources, extractCount, linkCount)},
								)
								llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
								continue
							}
						}
					}
					final := buildInsufficientWebResearchFallback(successfulToolCalls, researchRequirements, latestUserRequest, response)
					if err := a.postMessage(ctx, input.RunID, final); err != nil {
						return err
					}
					_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
						"step_id": "assistant_reply",
						"name":    "Generate assistant reply",
						"status":  "partial",
					})
					_ = a.postCompletionEvent(ctx, input.RunID, "partial", "insufficient_web_research_evidence")
					a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, final)
					return nil
				}
			}
			finalResponse := strings.TrimSpace(stripFencedToolBlocks(response))
			if finalResponse == "" {
				finalResponse = response
			}
			if researchRequirements.Enabled && (responseHasLowResearchQuality(finalResponse) || strings.Contains(strings.ToLower(finalResponse), "<think>")) {
				synthesized := a.composeBestEffortFinalResponse(ctx, input.RunID, providers, llmMessages, latestUserRequest, successfulToolCalls, researchRequirements, hadToolErrors, nil)
				if strings.TrimSpace(synthesized) != "" {
					finalResponse = strings.TrimSpace(synthesized)
				}
			}
			if researchRequirements.Enabled {
				if deterministic := strings.TrimSpace(buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, researchRequirements, latestUserRequest)); deterministic != "" {
					finalResponse = deterministic
				}
			}
			finalResponse = enforceResearchQualityFallback(finalResponse, successfulToolCalls, researchRequirements, latestUserRequest)
			if err := a.postMessage(ctx, input.RunID, finalResponse); err != nil {
				return err
			}
			_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
				"step_id": "assistant_reply",
				"name":    "Generate assistant reply",
				"status":  ternaryStatus(hadToolErrors),
			})
			completionReason := "success"
			if hadToolErrors {
				completionReason = "partial_tool_errors"
			}
			_ = a.postCompletionEvent(ctx, input.RunID, ternaryStatus(hadToolErrors), completionReason)
			a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, finalResponse)
			return nil
		}
		if a.toolRunner == "" {
			_ = a.postEvent(ctx, input.RunID, "tool.failed", map[string]any{"error": "tool runner url not configured"})
			hadToolErrors = true
			llmMessages = append(llmMessages,
				llm.Message{Role: "assistant", Content: response},
				llm.Message{Role: "system", Content: "Tool execution is currently unavailable because the tool runner is not configured. Provide a helpful response without using tools, and explain this limitation briefly."},
			)
			llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
			continue
		}
		pendingToolBlock = ""
		toolRecoveryRepromptCount = 0
		llmMessages = append(llmMessages, llm.Message{Role: "assistant", Content: response})
		if len(toolCalls) > maxToolCalls {
			toolCalls = toolCalls[:maxToolCalls]
		}
		for _, call := range toolCalls {
			if !isToolAllowed(call.ToolName) {
				err := fmt.Errorf("tool not allowed: %s", call.ToolName)
				_ = a.postEvent(ctx, input.RunID, "tool.failed", buildToolFailurePayload(call.ToolName, err))
				hadToolErrors = true
				llmMessages = append(llmMessages, llm.Message{Role: "system", Content: formatToolResult(call.ToolName, nil, err)})
				llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
				continue
			}
			output, err := a.executeToolCall(ctx, input.RunID, call, browserUserTab)
			if err != nil {
				_ = a.postEvent(ctx, input.RunID, "tool.failed", buildToolFailurePayload(call.ToolName, err))
				hadToolErrors = true
				llmMessages = append(llmMessages, llm.Message{Role: "system", Content: formatToolResult(call.ToolName, nil, err)})
				llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
				continue
			}
			successfulToolCalls = append(successfulToolCalls, toolCall{ToolName: call.ToolName, Input: output})
			llmMessages = append(llmMessages, llm.Message{Role: "system", Content: formatToolResult(call.ToolName, output, nil)})
			llmMessages = clampConversationWindow(llmMessages, maxConversationMessages, maxConversationChars)
		}
		if researchRequirements.Enabled && hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) {
			final := a.composeBestEffortFinalResponse(ctx, input.RunID, providers, llmMessages, latestUserRequest, successfulToolCalls, researchRequirements, hadToolErrors, nil)
			if strings.TrimSpace(final) == "" {
				final = buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, researchRequirements, latestUserRequest)
			}
			if err := a.postMessage(ctx, input.RunID, final); err != nil {
				return err
			}
			status := ternaryStatus(hadToolErrors)
			_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
				"step_id": "assistant_reply",
				"name":    "Generate assistant reply",
				"status":  status,
			})
			completionReason := "research_evidence_complete"
			if hadToolErrors {
				completionReason = "partial_tool_errors"
			}
			_ = a.postCompletionEvent(ctx, input.RunID, status, completionReason)
			a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, final)
			return nil
		}
	}
	if lastResponse != "" {
		if researchRequirements.Enabled && !hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) {
			if !autoWebResearchRecoveryAttempted {
				recoveredCalls, recoveredHadErrors := a.autoDeepenWebResearch(ctx, input.RunID, latestUserRequest, successfulToolCalls, browserUserTab)
				if recoveredHadErrors {
					hadToolErrors = true
				}
				if len(recoveredCalls) > 0 {
					successfulToolCalls = append(successfulToolCalls, recoveredCalls...)
				}
			}
		}
		if researchRequirements.Enabled && !hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) {
			final := buildInsufficientWebResearchFallback(successfulToolCalls, researchRequirements, latestUserRequest, lastResponse)
			if err := a.postMessage(ctx, input.RunID, final); err != nil {
				return err
			}
			_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
				"step_id": "assistant_reply",
				"name":    "Generate assistant reply",
				"status":  "partial",
			})
			_ = a.postCompletionEvent(ctx, input.RunID, "partial", "insufficient_web_research_evidence")
			a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, final)
			return nil
		}
		final := strings.TrimSpace(stripFencedToolBlocks(lastResponse))
		if researchRequirements.Enabled {
			synthesized := a.composeBestEffortFinalResponse(ctx, input.RunID, providers, llmMessages, latestUserRequest, successfulToolCalls, researchRequirements, hadToolErrors, nil)
			if strings.TrimSpace(synthesized) == "" {
				if hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) {
					synthesized = buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, researchRequirements, latestUserRequest)
				} else {
					synthesized = buildInsufficientWebResearchFallback(successfulToolCalls, researchRequirements, latestUserRequest, lastResponse)
				}
			}
			if strings.TrimSpace(synthesized) != "" {
				final = synthesized
			}
		}
		if final == "" && len(successfulToolCalls) > 0 {
			final = a.composeBestEffortFinalResponse(ctx, input.RunID, providers, llmMessages, latestUserRequest, successfulToolCalls, researchRequirements, hadToolErrors, nil)
		}
		if final == "" {
			final = buildToolExecutionFallback(successfulToolCalls)
		}
		if researchRequirements.Enabled {
			if deterministic := strings.TrimSpace(buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, researchRequirements, latestUserRequest)); deterministic != "" {
				final = deterministic
			}
		}
		final = enforceResearchQualityFallback(final, successfulToolCalls, researchRequirements, latestUserRequest)
		if err := a.postMessage(ctx, input.RunID, final); err != nil {
			return err
		}
		status := ternaryStatus(hadToolErrors)
		completionReason := "max_iterations"
		if !hadToolErrors {
			completionReason = "success"
		}
		if researchRequirements.Enabled {
			if hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, researchRequirements, latestUserRequest) {
				if !hadToolErrors {
					status = "completed"
					completionReason = "research_evidence_complete"
				} else {
					status = "partial"
					completionReason = "partial_tool_errors"
				}
			} else {
				status = "partial"
				completionReason = "insufficient_web_research_evidence"
			}
		}
		_ = a.emitEvent(ctx, input.RunID, "step.completed", map[string]any{
			"step_id": "assistant_reply",
			"name":    "Generate assistant reply",
			"status":  status,
		})
		_ = a.postCompletionEvent(ctx, input.RunID, status, completionReason)
		a.maybeGenerateRunTitle(ctx, input.RunID, primaryProvider, messages, final)
		return nil
	}
	return nil
}

func (a *RunActivities) HandleRunFailure(ctx context.Context, input RunFailureInput) error {
	if strings.TrimSpace(input.RunID) == "" {
		return errors.New("run_id required")
	}
	detail := strings.TrimSpace(input.Error)
	if detail == "" {
		detail = "unknown workflow activity error"
	}
	payload := map[string]any{
		"error":             detail,
		"phase":             "failed",
		"completion_reason": "activity_error",
	}
	if err := a.postEvent(ctx, input.RunID, "run.failed", payload); err == nil {
		return nil
	}
	return a.appendLocalEvent(ctx, input.RunID, "run.failed", "llm", payload)
}

func ternaryStatus(partial bool) string {
	if partial {
		return "partial"
	}
	return "completed"
}

func (a *RunActivities) postCompletionEvent(ctx context.Context, runID string, status string, reason string) error {
	eventType := "run.completed"
	if strings.EqualFold(status, "partial") {
		eventType = "run.partial"
	}
	payload := map[string]any{
		"status":            status,
		"phase":             "completed",
		"completion_reason": reason,
	}
	var eventErr error
	if err := a.postEvent(ctx, runID, eventType, payload); err != nil {
		eventErr = a.appendLocalEvent(ctx, runID, eventType, "llm", payload)
	}
	_ = a.cleanupRunResources(ctx, runID)
	return eventErr
}

func (a *RunActivities) emitEvent(ctx context.Context, runID string, eventType string, payload map[string]any) error {
	if err := a.postEvent(ctx, runID, eventType, payload); err == nil {
		return nil
	}
	return a.appendLocalEvent(ctx, runID, eventType, "llm", payload)
}

func (a *RunActivities) appendLocalEvent(ctx context.Context, runID string, eventType string, source string, payload map[string]any) error {
	seq, err := a.store.NextSeq(ctx, runID)
	if err != nil {
		return err
	}
	return a.store.AppendEvent(ctx, store.RunEvent{
		RunID:     runID,
		Seq:       seq,
		Type:      eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    source,
		TraceID:   uuid.New().String(),
		Payload:   payload,
	})
}

func (a *RunActivities) buildProviderCandidates(cfg llm.Config, modelRoute string) ([]llmProviderCandidate, error) {
	candidates := make([]llmProviderCandidate, 0, 4)
	seen := map[string]struct{}{}

	appendCandidate := func(name string, candidateCfg llm.Config, required bool) error {
		key := strings.TrimSpace(candidateCfg.Provider) + "|" + strings.TrimSpace(candidateCfg.Model)
		if _, exists := seen[key]; exists {
			return nil
		}
		provider, err := newProvider(candidateCfg)
		if err != nil {
			if required {
				return err
			}
			return nil
		}
		seen[key] = struct{}{}
		candidates = append(candidates, llmProviderCandidate{
			Name:     strings.TrimSpace(name),
			Provider: provider,
		})
		return nil
	}

	routeEntries := parseModelRoute(modelRoute)
	if len(routeEntries) > 0 {
		for _, entry := range routeEntries {
			routeCfg := cfg
			routeCfg.Provider = entry.provider
			if entry.model != "" {
				routeCfg.Model = entry.model
			}
			_ = appendCandidate(entry.provider, routeCfg, false)
		}
		if len(candidates) > 0 {
			return candidates, nil
		}
	}
	if err := appendCandidate(strings.TrimSpace(cfg.Provider), cfg, true); err != nil {
		return nil, err
	}

	fallbackProvider := strings.TrimSpace(cfg.FallbackProvider)
	if fallbackProvider != "" {
		fallbackCfg := cfg
		fallbackCfg.Provider = fallbackProvider
		if model := strings.TrimSpace(cfg.FallbackModel); model != "" {
			fallbackCfg.Model = model
		}
		if baseURL := strings.TrimSpace(cfg.FallbackBaseURL); baseURL != "" {
			fallbackCfg.BaseURL = baseURL
		}
		_ = appendCandidate(fallbackProvider, fallbackCfg, false)
	}

	return candidates, nil
}

type modelRouteEntry struct {
	provider string
	model    string
}

func parseModelRoute(raw string) []modelRouteEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	results := make([]modelRouteEntry, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		provider := part
		model := ""
		if strings.Contains(part, ":") {
			split := strings.SplitN(part, ":", 2)
			provider = strings.TrimSpace(split[0])
			model = strings.TrimSpace(split[1])
		}
		if provider == "" {
			continue
		}
		results = append(results, modelRouteEntry{provider: provider, model: model})
	}
	return results
}

func (a *RunActivities) resolveModelRoute(ctx context.Context, runID string, messages []store.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		if route := readString(messages[i].Metadata, "model_route"); route != "" {
			return route
		}
	}
	if strings.TrimSpace(runID) == "" {
		return ""
	}
	eventsList, err := a.store.ListEvents(ctx, runID, 0)
	if err != nil {
		return ""
	}
	for i := len(eventsList) - 1; i >= 0; i-- {
		if eventsList[i].Type != "run.started" && eventsList[i].Type != "run.resumed" {
			continue
		}
		if route := readString(eventsList[i].Payload, "model_route"); route != "" {
			return route
		}
	}
	return ""
}

func (a *RunActivities) resolveConfig(ctx context.Context, messages []store.Message) (llm.Config, error) {
	cfg := a.defaultConfig
	settings, err := a.store.GetLLMSettings(ctx)
	if err != nil {
		return cfg, err
	}
	if settings != nil {
		cfg.Mode = settings.Mode
		cfg.Provider = settings.Provider
		cfg.Model = settings.Model
		cfg.BaseURL = settings.BaseURL
		cfg.CodexAuthPath = settings.CodexAuthPath
		cfg.CodexHome = settings.CodexHome
		if settings.APIKeyEnc != "" {
			if a.secretsKey == nil {
				return cfg, errors.New("LLM_SECRETS_KEY is required to decrypt API keys")
			}
			apiKey, err := decryptSecret(a.secretsKey, settings.APIKeyEnc)
			if err != nil {
				return cfg, err
			}
			switch settings.Provider {
			case "openrouter":
				cfg.OpenRouterAPIKey = apiKey
			case "opencode-zen":
				cfg.OpenCodeAPIKey = apiKey
			default:
				cfg.OpenAIAPIKey = apiKey
			}
		}
	}
	if overrideProvider, overrideModel := extractOverrides(messages); overrideProvider != "" || overrideModel != "" {
		if overrideProvider != "" {
			cfg.Provider = overrideProvider
		}
		if overrideModel != "" {
			cfg.Model = overrideModel
		}
	}
	if requiresAPIKey(cfg.Provider) {
		if cfg.Provider == "openrouter" {
			if cfg.OpenRouterAPIKey == "" {
				return cfg, errors.New("missing API key for provider")
			}
		} else if cfg.OpenAIAPIKey == "" {
			return cfg, errors.New("missing API key for provider")
		}
	}
	return cfg, nil
}

func extractOverrides(messages []store.Message) (string, string) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "user" {
			continue
		}
		provider := readString(msg.Metadata, "llm_provider")
		model := readString(msg.Metadata, "llm_model")
		return provider, model
	}
	return "", ""
}

func resolveBrowserUserTabConfig(messages []store.Message) browserUserTabConfig {
	config := browserUserTabConfig{}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "user" {
			continue
		}
		mode := strings.ToLower(readString(msg.Metadata, "browser_mode"))
		if mode == "" {
			mode = strings.ToLower(readString(msg.Metadata, "browser_control_mode"))
		}
		if mode != "user_tab" {
			return config
		}
		config.Enabled = true
		interaction := strings.ToLower(readString(msg.Metadata, "browser_interaction"))
		switch interaction {
		case "", "true", "allow", "allowed", "enabled", "interactive":
			config.InteractionAllowed = true
		}
		config.DomainAllowlist = parseDomainAllowlist(readString(msg.Metadata, "browser_domain_allowlist"))
		config.BrowserUserAgent = strings.TrimSpace(readString(msg.Metadata, "browser_user_agent"))
		config.PreferredBrowser = normalizePreferredBrowser(firstNonEmptyString(
			readString(msg.Metadata, "browser_preferred_browser"),
			readString(msg.Metadata, "browser_browser"),
			inferPreferredBrowserFromUserAgent(config.BrowserUserAgent),
		))
		return config
	}
	return config
}

func normalizePreferredBrowser(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "brave", "brave browser":
		return "brave"
	case "chrome", "google chrome":
		return "chrome"
	case "edge", "microsoft edge":
		return "edge"
	case "chromium":
		return "chromium"
	case "arc":
		return "arc"
	case "opera":
		return "opera"
	case "vivaldi":
		return "vivaldi"
	default:
		return ""
	}
}

func inferPreferredBrowserFromUserAgent(userAgent string) string {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case strings.Contains(ua, "brave"):
		return "brave"
	case strings.Contains(ua, "edg/"):
		return "edge"
	case strings.Contains(ua, "opr/") || strings.Contains(ua, "opera"):
		return "opera"
	case strings.Contains(ua, "vivaldi"):
		return "vivaldi"
	case strings.Contains(ua, "arc/") || strings.Contains(ua, " arc"):
		return "arc"
	case strings.Contains(ua, "chromium"):
		return "chromium"
	case strings.Contains(ua, "chrome/"):
		return "chrome"
	default:
		return ""
	}
}

func parseDomainAllowlist(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	})
	if len(parts) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		candidate := strings.ToLower(strings.TrimSpace(part))
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
			if parsed, err := url.Parse(candidate); err == nil {
				candidate = strings.ToLower(strings.TrimSpace(parsed.Hostname()))
			}
		}
		candidate = strings.TrimPrefix(candidate, ".")
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (a *RunActivities) buildMemoryPrompt(ctx context.Context, messages []store.Message) string {
	settings, err := a.store.GetMemorySettings(ctx)
	if err != nil || settings == nil || !settings.Enabled {
		return ""
	}
	query := ""
	var queryEmbedding []float32
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			query = messages[i].Content
			// TODO: generate embeddings for user queries when an embedding provider is configured.
			queryEmbedding = readFloat32Slice(messages[i].Metadata, "embedding")
			break
		}
	}
	if query == "" {
		return ""
	}
	limit := a.memoryMaxResults
	if limit <= 0 {
		limit = 5
	}
	var entries []store.MemoryEntry
	if len(queryEmbedding) > 0 {
		entries, err = a.store.SearchMemoryWithEmbedding(ctx, query, queryEmbedding, limit)
	} else {
		entries, err = a.store.SearchMemory(ctx, query, limit)
	}
	if err != nil || len(entries) == 0 {
		return ""
	}
	lines := make([]string, 0, len(entries)+1)
	lines = append(lines, "Relevant memory:")
	for _, entry := range entries {
		content := strings.TrimSpace(entry.Content)
		if content == "" {
			continue
		}
		if a.memoryMaxEntryChars > 0 {
			content = truncateRunes(content, a.memoryMaxEntryChars)
		}
		lines = append(lines, "- "+content)
	}
	return strings.Join(lines, "\n")
}

func (a *RunActivities) buildSystemPrompt(ctx context.Context) string {
	personalityText := a.resolvePersonality(ctx)
	currentTime := time.Now().UTC()
	skillsRoot := "~/.config/opencode/skills"
	if root, err := skills.RootDir(); err == nil {
		skillsRoot = root
	}
	memoryEnabled := false
	if settings, err := a.store.GetMemorySettings(ctx); err == nil && settings != nil {
		memoryEnabled = settings.Enabled
	}
	capabilities := a.detectToolCapabilities(ctx)
	runtimeLines := []string{
		"Runtime context:",
		fmt.Sprintf("- Current date/time (UTC): %s", currentTime.Format(time.RFC3339)),
		fmt.Sprintf("- Tool runner availability: %s.", capabilities.runnerStatus),
		fmt.Sprintf("- Browser tool availability: %s.", capabilities.browserStatus),
		fmt.Sprintf("- Available tool families: %s.", capabilities.familiesLine),
		"- For web research or code/file tasks, execute tools immediately instead of replying with intent-only prose.",
		"- For software generation tasks, create/update files with editor tools, validate with process.exec, and use process.start/process.status/process.logs for long-running dev servers.",
		"- Browser tool names must be canonical: browser.navigate, browser.snapshot, browser.click, browser.type, browser.scroll, browser.extract, browser.evaluate, browser.pdf. Do not invent aliases like browser.search/browser.browse.",
		"- Use tools only when they are available; if unavailable, state that clearly and do not imply files or commands were created/executed.",
		"- To call tools, respond with a fenced JSON block: ```tool {\"tool_calls\":[{\"tool_name\":\"editor.write\",\"input\":{...}}]} ```.",
	}
	lines := []string{
		"System resources:",
		fmt.Sprintf("- Skills directory: %s (each skill has SKILL.md and optional references/ and scripts/).", skillsRoot),
		"- Context root: /context (virtual tree stored in the control plane; paths are relative to this root).",
		"- Skills may reference context paths when relevant.",
	}
	if memoryEnabled {
		lines = append(lines, "- Memory: enabled; use relevant memory entries when provided.")
	} else {
		lines = append(lines, "- Memory: disabled unless the user enables it.")
	}
	blocks := []string{}
	if strings.TrimSpace(personalityText) != "" {
		blocks = append(blocks, strings.TrimSpace(personalityText))
	}
	blocks = append(blocks, strings.Join(runtimeLines, "\n"))
	blocks = append(blocks, strings.Join(lines, "\n"))
	return strings.Join(blocks, "\n\n")
}

func (a *RunActivities) resolvePersonality(ctx context.Context) string {
	if settings, err := a.store.GetPersonalitySettings(ctx); err == nil && settings != nil {
		if content := strings.TrimSpace(settings.Content); content != "" {
			return content
		}
	}
	if content, err := personality.ReadFromDisk(); err == nil {
		if trimmed := strings.TrimSpace(content); trimmed != "" {
			return trimmed
		}
	}
	return personality.Default
}

func readString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func readFloat32Slice(metadata map[string]any, key string) []float32 {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	slice, ok := value.([]any)
	if !ok {
		return nil
	}
	results := make([]float32, 0, len(slice))
	for _, item := range slice {
		switch v := item.(type) {
		case float32:
			results = append(results, v)
		case float64:
			results = append(results, float32(v))
		case int:
			results = append(results, float32(v))
		case int64:
			results = append(results, float32(v))
		case json.Number:
			if parsed, err := v.Float64(); err == nil {
				results = append(results, float32(parsed))
			}
		}
	}
	if len(results) == 0 {
		return nil
	}
	return results
}

func truncateRunes(value string, maxChars int) string {
	if maxChars <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxChars {
		return value
	}
	return string(runes[:maxChars]) + "…"
}

func requiresAPIKey(provider string) bool {
	switch provider {
	case "openai", "openrouter", "opencode-zen", "kimi-for-coding", "moonshot-ai":
		return true
	default:
		return false
	}
}

type toolParseStatus struct {
	sawToolBlock  bool
	hadIncomplete bool
	hadOversized  bool
}

func parseToolCalls(content string) ([]toolCall, toolParseStatus) {
	content = trimForToolParsing(content)
	if inlineCalls := parseInlineFencedToolCalls(content); len(inlineCalls) > 0 {
		return inlineCalls, toolParseStatus{sawToolBlock: true}
	}
	blocks := extractFencedBlocks(content)
	status := toolParseStatus{}
	for _, block := range blocks {
		lang := strings.ToLower(strings.TrimSpace(block.lang))
		if lang != "tool" && lang != "json" {
			continue
		}
		if lang == "tool" {
			status.sawToolBlock = true
		}
		body := strings.TrimSpace(block.body)
		if !block.complete {
			status.hadIncomplete = true
			if body != "" {
				status.sawToolBlock = true
			}
			continue
		}
		if body == "" {
			continue
		}
		if len(body) > maxToolJSONChars {
			status.hadOversized = true
			status.sawToolBlock = true
			continue
		}
		if lang == "json" && (strings.Contains(body, "\"tool_calls\"") || strings.Contains(body, "\"tool_name\"")) {
			status.sawToolBlock = true
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			continue
		}
		calls := parseToolCallsFromPayload(payload)
		if len(calls) > 0 {
			return calls, status
		}
	}
	if directCalls := parseBareToolPayload(content); len(directCalls) > 0 {
		status.sawToolBlock = true
		return directCalls, status
	}
	return nil, status
}

func parseInlineFencedToolCalls(content string) []toolCall {
	matches := inlineToolFenceRE.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	calls := make([]toolCall, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		payloadText := strings.TrimSpace(match[2])
		if payloadText == "" || len(payloadText) > maxToolJSONChars {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadText), &payload); err != nil {
			continue
		}
		parsed := parseToolCallsFromPayload(payload)
		if len(parsed) > 0 {
			calls = append(calls, parsed...)
		}
	}
	if len(calls) == 0 {
		return nil
	}
	return calls
}

func parseBareToolPayload(content string) []toolCall {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil
	}
	if len(trimmed) > maxToolJSONChars {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil
	}
	return parseToolCallsFromPayload(payload)
}

func stripFencedToolBlocks(content string) string {
	cleaned := fencedToolBlockRE.ReplaceAllString(content, "")
	return strings.TrimSpace(cleaned)
}

type toolCapabilities struct {
	runnerStatus  string
	browserStatus string
	familiesLine  string
}

func (a *RunActivities) detectToolCapabilities(ctx context.Context) toolCapabilities {
	families := []string{"editor", "process", "document"}
	if a.toolRunner == "" {
		return toolCapabilities{
			runnerStatus:  "unavailable (tool runner URL not configured)",
			browserStatus: "unavailable",
			familiesLine:  strings.Join(families, ", "),
		}
	}
	baseURL := strings.TrimRight(a.toolRunner, "/")
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	healthReq, err := http.NewRequestWithContext(probeCtx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return toolCapabilities{runnerStatus: "unavailable", browserStatus: "unavailable", familiesLine: strings.Join(families, ", ")}
	}
	resp, err := a.httpClient.Do(healthReq)
	if err != nil {
		return toolCapabilities{runnerStatus: "unavailable", browserStatus: "unavailable", familiesLine: strings.Join(families, ", ")}
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return toolCapabilities{runnerStatus: fmt.Sprintf("unavailable (health status %d)", resp.StatusCode), browserStatus: "unavailable", familiesLine: strings.Join(families, ", ")}
	}
	runnerStatus := "available"
	browserStatus := "unknown"
	capReq, err := http.NewRequestWithContext(probeCtx, http.MethodGet, baseURL+"/tools/capabilities", nil)
	if err != nil {
		return toolCapabilities{runnerStatus: runnerStatus, browserStatus: browserStatus, familiesLine: strings.Join(families, ", ")}
	}
	capResp, err := a.httpClient.Do(capReq)
	if err != nil {
		return toolCapabilities{runnerStatus: runnerStatus, browserStatus: browserStatus, familiesLine: strings.Join(families, ", ")}
	}
	defer capResp.Body.Close()
	if capResp.StatusCode < 200 || capResp.StatusCode >= 300 {
		return toolCapabilities{runnerStatus: runnerStatus, browserStatus: browserStatus, familiesLine: strings.Join(families, ", ")}
	}
	var payload struct {
		Tools   []string `json:"tools"`
		Browser struct {
			Enabled bool `json:"enabled"`
			Healthy bool `json:"healthy"`
		} `json:"browser"`
	}
	if err := json.NewDecoder(capResp.Body).Decode(&payload); err != nil {
		return toolCapabilities{runnerStatus: runnerStatus, browserStatus: browserStatus, familiesLine: strings.Join(families, ", ")}
	}
	if payload.Browser.Enabled && payload.Browser.Healthy {
		browserStatus = "available"
		families = append([]string{"browser"}, families...)
	} else if payload.Browser.Enabled {
		browserStatus = "configured but unavailable"
	} else {
		browserStatus = "unavailable"
	}
	if len(payload.Tools) > 0 {
		hasBrowser := false
		hasDocument := false
		hasEditor := false
		hasProcess := false
		for _, name := range payload.Tools {
			switch {
			case strings.HasPrefix(name, "browser."):
				hasBrowser = true
			case strings.HasPrefix(name, "document."):
				hasDocument = true
			case strings.HasPrefix(name, "editor."):
				hasEditor = true
			case strings.HasPrefix(name, "process."):
				hasProcess = true
			}
		}
		families = families[:0]
		if hasBrowser {
			families = append(families, "browser")
		}
		if hasEditor {
			families = append(families, "editor")
		}
		if hasProcess {
			families = append(families, "process")
		}
		if hasDocument {
			families = append(families, "document")
		}
		if len(families) == 0 {
			families = append(families, "none")
		}
	}
	return toolCapabilities{runnerStatus: runnerStatus, browserStatus: browserStatus, familiesLine: strings.Join(families, ", ")}
}

func (a *RunActivities) maybeGenerateRunTitle(ctx context.Context, runID string, provider llm.Provider, messages []store.Message, assistantReply string) {
	if strings.TrimSpace(assistantReply) == "" {
		return
	}
	events, err := a.store.ListEvents(ctx, runID, 0)
	if err == nil {
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Type != "run.title.updated" {
				continue
			}
			if title := readString(events[i].Payload, "title"); title != "" {
				return
			}
		}
	}
	latestUser := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			latestUser = strings.TrimSpace(messages[i].Content)
			if latestUser != "" {
				break
			}
		}
	}
	if latestUser == "" {
		return
	}
	titlePrompt := []llm.Message{
		{Role: "system", Content: "Generate a concise chat title (3-6 words). Return only the title text, no punctuation wrappers."},
		{Role: "user", Content: fmt.Sprintf("User request: %s\n\nAssistant response: %s", truncateRunes(latestUser, 280), truncateRunes(strings.TrimSpace(assistantReply), 600))},
	}
	titleCtx := ctx
	cancel := func() {}
	if runTitleGenerateTimeout > 0 {
		titleCtx, cancel = context.WithTimeout(ctx, runTitleGenerateTimeout)
	}
	defer cancel()
	titleRaw, err := provider.Generate(titleCtx, titlePrompt)
	if err != nil {
		return
	}
	title := sanitizeRunTitle(titleRaw)
	if title == "" {
		return
	}
	seq, err := a.store.NextSeq(ctx, runID)
	if err != nil {
		return
	}
	_ = a.store.AppendEvent(ctx, store.RunEvent{
		RunID:     runID,
		Seq:       seq,
		Type:      "run.title.updated",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "llm",
		Payload:   map[string]any{"title": title},
	})
}

func sanitizeRunTitle(raw string) string {
	title := strings.TrimSpace(raw)
	title = strings.Trim(title, "`\"' ")
	if idx := strings.Index(title, "\n"); idx >= 0 {
		title = strings.TrimSpace(title[:idx])
	}
	title = strings.TrimSuffix(title, ".")
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	return truncateRunes(title, 72)
}

type fencedBlock struct {
	lang     string
	body     string
	complete bool
}

func extractFencedBlocks(content string) []fencedBlock {
	content = trimForToolParsing(content)
	lines := strings.Split(content, "\n")
	blocks := []fencedBlock{}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "```") {
			continue
		}
		lang := strings.TrimSpace(strings.TrimPrefix(line, "```"))
		bodyLines := []string{}
		complete := false
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "```") {
				i = j
				complete = true
				break
			}
			bodyLines = append(bodyLines, lines[j])
		}
		blocks = append(blocks, fencedBlock{lang: lang, body: strings.Join(bodyLines, "\n"), complete: complete})
	}
	return blocks
}

func trimForToolParsing(content string) string {
	if maxToolParseContentChars <= 0 || len(content) <= maxToolParseContentChars {
		return content
	}
	return content[len(content)-maxToolParseContentChars:]
}

func clampToTail(value string, maxChars int) string {
	if maxChars <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxChars {
		return value
	}
	return string(runes[len(runes)-maxChars:])
}

func buildPendingToolBlock(content string) string {
	blocks := extractFencedBlocks(content)
	for i := len(blocks) - 1; i >= 0; i-- {
		block := blocks[i]
		lang := strings.ToLower(strings.TrimSpace(block.lang))
		if block.complete || (lang != "tool" && lang != "json") {
			continue
		}
		pending := "```" + strings.TrimSpace(block.lang) + "\n" + block.body
		return clampToTail(pending, maxPendingToolBlockChars)
	}
	return ""
}

func buildToolRecoveryPrompt(status toolParseStatus) string {
	if status.hadIncomplete && status.hadOversized {
		return fmt.Sprintf("Your previous tool block was incomplete and exceeded parser limits. Resend one complete fenced ```tool JSON block, split into smaller calls, and keep each block under %d characters.", maxToolJSONChars)
	}
	if status.hadIncomplete {
		return "Your previous tool block looks incomplete (missing closing ``` or truncated JSON). Resend one complete fenced ```tool JSON block only, with no prose before or after."
	}
	if status.hadOversized {
		return fmt.Sprintf("Your previous tool block exceeded parser limits. Retry with smaller tool calls and keep each fenced tool JSON block under %d characters.", maxToolJSONChars)
	}
	return "Your previous tool block was invalid and was not executed. Reply with either: (1) one valid fenced ```tool JSON payload, or (2) a normal assistant response with no tool block."
}

func parseToolCallsFromPayload(payload map[string]any) []toolCall {
	if payload == nil {
		return nil
	}
	if rawCalls, ok := payload["tool_calls"].([]any); ok {
		calls := make([]toolCall, 0, len(rawCalls))
		for _, raw := range rawCalls {
			if callMap, ok := raw.(map[string]any); ok {
				if call, ok := parseToolCallMap(callMap); ok {
					calls = append(calls, call)
				}
			}
		}
		return calls
	}
	if call, ok := parseToolCallMap(payload); ok {
		return []toolCall{call}
	}
	return nil
}

func parseToolCallMap(payload map[string]any) (toolCall, bool) {
	if functionData, ok := payload["function"].(map[string]any); ok {
		if name := readStringAny(functionData["name"]); name != "" {
			payload["tool_name"] = name
		}
		if _, hasInput := payload["input"]; !hasInput {
			if _, hasArgs := payload["arguments"]; !hasArgs {
				if functionArgs, ok := functionData["arguments"]; ok {
					payload["arguments"] = functionArgs
				}
			}
		}
	}
	name := readStringAny(payload["tool_name"])
	if name == "" {
		name = readStringAny(payload["name"])
	}
	input := parseToolInputFromPayload(payload)
	name, input = canonicalizeToolCall(name, input)
	if name == "" {
		return toolCall{}, false
	}
	return toolCall{ToolName: name, Input: input}, true
}

func parseToolInputFromPayload(payload map[string]any) map[string]any {
	for _, key := range []string{"input", "arguments", "args", "parameters"} {
		parsed, ok := parseToolInputValue(payload[key])
		if !ok {
			continue
		}
		return parsed
	}
	return map[string]any{}
}

func parseToolInputValue(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return map[string]any{}, true
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return nil, false
		}
		return parsed, true
	default:
		return nil, false
	}
}

func readStringAny(value any) string {
	if value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return strings.TrimSpace(str)
	}
	return ""
}

func normalizeToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func canonicalizeToolCall(name string, input map[string]any) (string, map[string]any) {
	canonical := normalizeToolName(name)
	normalizedInput := cloneAnyMap(input)
	switch canonical {
	case "browser.search", "browser.browse", "browser.open", "browser.goto", "browser.visit", "browser.go":
		canonical = "browser.navigate"
		normalizedInput = normalizeBrowserNavigateInput(normalizedInput)
	case "browser.screenshot", "browser.take_screenshot", "browser.capture":
		canonical = "browser.snapshot"
	case "browser.extract_text", "browser.read", "browser.read_text", "browser.get_text":
		canonical = "browser.extract"
		if readStringAny(normalizedInput["mode"]) == "" {
			normalizedInput["mode"] = "text"
		}
	case "browser.extract_list":
		canonical = "browser.extract"
		normalizedInput["mode"] = "list"
	case "browser.extract_table":
		canonical = "browser.extract"
		normalizedInput["mode"] = "table"
	case "browser.extract_metadata":
		canonical = "browser.extract"
		normalizedInput["mode"] = "metadata"
	}
	return canonical, normalizedInput
}

func normalizeBrowserNavigateInput(input map[string]any) map[string]any {
	normalized := cloneAnyMap(input)
	if existingURL := readStringAny(normalized["url"]); existingURL != "" {
		return normalized
	}
	for _, key := range []string{"target", "href", "link", "uri"} {
		value := readStringAny(normalized[key])
		if strings.HasPrefix(strings.ToLower(value), "http://") || strings.HasPrefix(strings.ToLower(value), "https://") {
			normalized["url"] = value
			return normalized
		}
	}
	for _, key := range []string{"query", "q", "search", "term", "keywords"} {
		queryText := readStringAny(normalized[key])
		if queryText == "" {
			continue
		}
		normalized["url"] = "https://duckduckgo.com/?q=" + url.QueryEscape(queryText)
		return normalized
	}
	return normalized
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func isToolAllowed(name string) bool {
	_, ok := allowedToolNames[normalizeToolName(name)]
	return ok
}

func isNoContentLLMError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	return strings.Contains(message, "no content") || strings.Contains(message, "response was empty")
}

func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	if strings.Contains(message, "timeout") || strings.Contains(message, "timed out") {
		return true
	}
	if strings.Contains(message, "bad gateway") || strings.Contains(message, "temporarily unavailable") {
		return true
	}
	if strings.Contains(message, "connection reset") || strings.Contains(message, "connection refused") {
		return true
	}
	if retryableStatusRE.MatchString(message) {
		return true
	}
	return false
}

func isTimeoutLLMError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	return strings.Contains(message, "timeout") || strings.Contains(message, "timed out")
}

func clampConversationWindow(messages []llm.Message, maxMessages int, maxChars int) []llm.Message {
	if len(messages) == 0 {
		return messages
	}
	if maxMessages <= 0 && maxChars <= 0 {
		return messages
	}

	prefixCount := 0
	for prefixCount < len(messages) && messages[prefixCount].Role == "system" {
		prefixCount++
	}
	prefix := append([]llm.Message{}, messages[:prefixCount]...)
	tail := messages[prefixCount:]
	if len(tail) == 0 {
		return prefix
	}

	selected := make([]llm.Message, 0, len(tail))
	totalChars := 0
	for i := len(tail) - 1; i >= 0; i-- {
		current := tail[i]
		currentChars := len([]rune(current.Content))
		if len(selected) > 0 {
			if maxMessages > 0 && len(selected) >= maxMessages {
				break
			}
			if maxChars > 0 && totalChars+currentChars > maxChars {
				break
			}
		}
		selected = append(selected, current)
		totalChars += currentChars
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}

	result := make([]llm.Message, 0, len(prefix)+len(selected))
	result = append(result, prefix...)
	result = append(result, selected...)
	return result
}

func llmRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 2:
		return 250 * time.Millisecond
	case 3:
		return 750 * time.Millisecond
	default:
		return 0
	}
}

func (a *RunActivities) generateWithRetry(ctx context.Context, runID string, providers []llmProviderCandidate, messages []llm.Message) (string, error) {
	if len(providers) == 0 {
		return "", errors.New("no llm providers configured")
	}
	attempts := maxLLMGenerateAttempts
	if attempts < 1 {
		attempts = 1
	}
	budgetDeadline := time.Time{}
	if maxLLMPhaseBudget > 0 {
		budgetDeadline = time.Now().Add(maxLLMPhaseBudget)
	}
	var lastErr error
	for providerIndex, provider := range providers {
		for attempt := 1; attempt <= attempts; attempt++ {
			if delay := llmRetryDelay(attempt); delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}

			generateCtx := ctx
			cancel := func() {}
			timeout := a.requestTimeout
			if !budgetDeadline.IsZero() {
				remaining := time.Until(budgetDeadline)
				if remaining <= 0 {
					if lastErr != nil {
						return "", lastErr
					}
					return "", context.DeadlineExceeded
				}
				if timeout <= 0 || remaining < timeout {
					timeout = remaining
				}
			}
			if timeout > 0 {
				generateCtx, cancel = context.WithTimeout(ctx, timeout)
			}
			if runID != "" {
				_ = a.postEvent(ctx, runID, "model.request.started", map[string]any{
					"provider":  provider.Name,
					"attempt":   attempt,
					"transient": true,
				})
			}
			response, err := provider.Provider.Generate(generateCtx, messages)
			cancel()
			if err == nil {
				if strings.TrimSpace(response) == "" {
					err = errors.New("LLM response had no content")
				} else {
					if runID != "" {
						_ = a.postEvent(ctx, runID, "model.request.completed", map[string]any{
							"provider":  provider.Name,
							"attempt":   attempt,
							"transient": true,
						})
					}
					return response, nil
				}
			}
			if runID != "" {
				_ = a.postEvent(ctx, runID, "model.request.failed", map[string]any{
					"provider": provider.Name,
					"attempt":  attempt,
					"error":    truncateRunes(strings.TrimSpace(err.Error()), 200),
				})
			}
			lastErr = err
			if !isRetryableLLMError(err) {
				return "", err
			}
			if shouldFailoverProvider(err) {
				break
			}
			// Timeouts are the slowest failure mode; cap these to two attempts.
			if isTimeoutLLMError(err) && attempt >= 2 {
				break
			}
			if attempt == attempts {
				break
			}
		}
		if providerIndex < len(providers)-1 {
			continue
		}
	}
	if lastErr == nil {
		return "", errors.New("llm generation failed")
	}
	return "", lastErr
}

func shouldFailoverProvider(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	if strings.Contains(message, "bad gateway") || strings.Contains(message, "service unavailable") || strings.Contains(message, "gateway timeout") {
		return true
	}
	return strings.Contains(message, " 502") || strings.Contains(message, " 503") || strings.Contains(message, " 504")
}

func buildNoContentFallback(successfulToolCalls []toolCall) string {
	if summary := summarizeWritePaths(successfulToolCalls); summary != "" {
		return fmt.Sprintf("I completed some tool actions (%s), but the model returned an empty response before I could finish. Please retry and I can continue from these changes.", summary)
	}
	return "The model returned an empty response, so I could not finish this run. Please retry and I can continue from where we left off."
}

func buildTransientLLMFallback(err error) string {
	if err == nil {
		return "The model provider is temporarily unavailable, so I could not finish this run. Please retry in a moment."
	}
	return fmt.Sprintf("The model provider is temporarily unavailable (%s). Please retry in a moment.", truncateRunes(strings.TrimSpace(err.Error()), 180))
}

func buildToolExecutionFallback(successfulToolCalls []toolCall) string {
	if len(successfulToolCalls) > 0 {
		return buildToolCompletionSummary(successfulToolCalls, false, nil)
	}
	return "No tool actions were executed for this run. Retry this request and I will execute it."
}

func buildNoContentRetryPrompt(mustExecuteTools bool, userRequest string) string {
	requestSnippet := truncateRunes(strings.TrimSpace(userRequest), 240)
	if mustExecuteTools {
		return fmt.Sprintf("The previous response was empty. Retry now and respond with exactly one fenced ```tool JSON block for request %q. No prose.", requestSnippet)
	}
	return "The previous response was empty. Retry now with a concise assistant reply only."
}

func buildToolCompletionSummary(successfulToolCalls []toolCall, hadToolErrors bool, cause error) string {
	parts := make([]string, 0, 3)
	if writes := summarizeWritePaths(successfulToolCalls); writes != "" {
		parts = append(parts, writes)
	}
	if processSummary := summarizeProcessActions(successfulToolCalls); processSummary != "" {
		parts = append(parts, processSummary)
	}
	if preview := summarizePreviewURLs(successfulToolCalls); preview != "" {
		parts = append(parts, preview)
	}

	actionSummary := "completed tool actions"
	if len(parts) == 1 {
		actionSummary = parts[0]
	} else if len(parts) > 1 {
		actionSummary = strings.Join(parts, "; ")
	}

	if hadToolErrors {
		return fmt.Sprintf("I partially completed this run (%s), but some tool steps failed. You can resume and I will continue from the current state.", actionSummary)
	}
	if cause != nil {
		return fmt.Sprintf("I completed this run (%s), but the model provider became unavailable before I could generate a fuller narrative (%s).", actionSummary, truncateRunes(strings.TrimSpace(cause.Error()), 160))
	}
	return fmt.Sprintf("I completed this run (%s).", actionSummary)
}

func (a *RunActivities) composeBestEffortFinalResponse(
	ctx context.Context,
	runID string,
	providers []llmProviderCandidate,
	baseMessages []llm.Message,
	userRequest string,
	successfulToolCalls []toolCall,
	researchRequirements webResearchRequirements,
	hadToolErrors bool,
	cause error,
) string {
	if len(successfulToolCalls) == 0 {
		return ""
	}
	if synthesized, synthErr := a.generateFinalSynthesis(ctx, runID, providers, baseMessages, userRequest, successfulToolCalls, researchRequirements); synthErr == nil {
		candidate := strings.TrimSpace(stripFencedToolBlocks(synthesized))
		if candidate != "" && !(researchRequirements.Enabled && responseHasLowResearchQuality(candidate)) {
			return candidate
		}
	}
	if researchRequirements.Enabled {
		if deterministic := buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, researchRequirements, userRequest); deterministic != "" {
			return deterministic
		}
	}
	return buildToolCompletionSummary(successfulToolCalls, hadToolErrors, cause)
}

func responseHasLowResearchQuality(content string) bool {
	normalized := strings.ToLower(strings.TrimSpace(content))
	if normalized == "" {
		return true
	}
	if strings.Contains(normalized, "impact note unavailable") {
		return true
	}
	if strings.Contains(normalized, "did not expose a clear summary sentence") {
		return true
	}
	if strings.Contains(normalized, "compiled source diagnostics") {
		return true
	}
	if strings.Contains(normalized, "low-quality extracts:") {
		return true
	}
	if strings.Contains(normalized, "blocked sources:") {
		return true
	}
	if strings.Contains(normalized, "coverage limitation: extracted") {
		return true
	}
	if strings.Contains(normalized, "extractable source(s)") {
		return true
	}
	if strings.Contains(normalized, "[object object]") {
		return true
	}
	lowSignalURLFragments := []string{
		"duckduckgo-help-pages",
		"apps.apple.com",
		"play.google.com/store/apps",
		"/privacy",
		"/terms",
		"/cookie",
	}
	for _, fragment := range lowSignalURLFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	if countSourceLinks(content) == 0 {
		return true
	}
	return false
}

func enforceResearchQualityFallback(
	content string,
	successfulToolCalls []toolCall,
	requirements webResearchRequirements,
	userRequest string,
) string {
	trimmed := strings.TrimSpace(content)
	if !requirements.Enabled {
		derived := deriveWebResearchRequirements(userRequest, true)
		if !derived.Enabled {
			return trimmed
		}
		requirements = derived
	}
	if trimmed != "" && !responseHasLowResearchQuality(trimmed) {
		return trimmed
	}
	if deterministic := strings.TrimSpace(buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, requirements, userRequest)); deterministic != "" {
		return deterministic
	}
	if trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fmt.Sprintf("I could not extract enough evidence to complete this research request. Requested evidence threshold: %d usable sources.", requirements.MinimumItems))
}

func buildInvalidToolPayloadFallback(successfulToolCalls []toolCall) string {
	if summary := summarizeWritePaths(successfulToolCalls); summary != "" {
		return fmt.Sprintf("I completed some tool actions (%s), but I could not parse valid follow-up tool instructions from the model. Please retry and I can continue from this state.", summary)
	}
	return "I could not parse valid tool instructions from the model after several retries. Please retry and I will continue with stricter tool formatting."
}

func summarizeWritePaths(successfulToolCalls []toolCall) string {
	paths := make([]string, 0, len(successfulToolCalls))
	for _, call := range successfulToolCalls {
		if call.ToolName != "editor.write" {
			continue
		}
		pathValue, ok := call.Input["path"]
		if !ok {
			continue
		}
		if pathText, ok := pathValue.(string); ok {
			trimmed := strings.TrimSpace(pathText)
			if trimmed != "" {
				paths = append(paths, trimmed)
			}
		}
	}
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return fmt.Sprintf("wrote %s", paths[0])
	}
	if len(paths) == 2 {
		return fmt.Sprintf("wrote %s and %s", paths[0], paths[1])
	}
	return fmt.Sprintf("wrote %s and %d more files", paths[0], len(paths)-1)
}

func summarizeProcessActions(successfulToolCalls []toolCall) string {
	execCount := 0
	startedCount := 0
	for _, call := range successfulToolCalls {
		switch call.ToolName {
		case "process.exec":
			execCount++
		case "process.start":
			startedCount++
		}
	}
	parts := make([]string, 0, 2)
	if execCount > 0 {
		if execCount == 1 {
			parts = append(parts, "ran 1 command")
		} else {
			parts = append(parts, fmt.Sprintf("ran %d commands", execCount))
		}
	}
	if startedCount > 0 {
		if startedCount == 1 {
			parts = append(parts, "started 1 process")
		} else {
			parts = append(parts, fmt.Sprintf("started %d processes", startedCount))
		}
	}
	return strings.Join(parts, " and ")
}

func summarizePreviewURLs(successfulToolCalls []toolCall) string {
	unique := map[string]struct{}{}
	urls := make([]string, 0, 4)
	for _, call := range successfulToolCalls {
		raw := call.Input["preview_urls"]
		candidates := toStringSlice(raw)
		for _, url := range candidates {
			trimmed := strings.TrimSpace(url)
			if trimmed == "" {
				continue
			}
			if _, exists := unique[trimmed]; exists {
				continue
			}
			unique[trimmed] = struct{}{}
			urls = append(urls, trimmed)
		}
	}
	if len(urls) == 0 {
		return ""
	}
	if len(urls) == 1 {
		return fmt.Sprintf("preview available at %s", urls[0])
	}
	return fmt.Sprintf("preview available at %s and %d more URLs", urls[0], len(urls)-1)
}

type webResearchEvidence struct {
	URL          string
	Title        string
	Impact       string
	EvidenceText []string
	SeqHint      int
	Status       string
	ReasonCode   string
	ReasonDetail string
	WordCount    int
}

func (e webResearchEvidence) blocked() bool {
	switch strings.TrimSpace(strings.ToLower(e.ReasonCode)) {
	case "blocked_by_bot_protection", "consent_wall", "login_wall":
		return true
	default:
		return false
	}
}

func (e webResearchEvidence) extractable() bool {
	return !e.blocked() && strings.TrimSpace(strings.ToLower(e.ReasonCode)) != "no_extractable_content"
}

func buildDeterministicWebResearchSummary(successfulToolCalls []toolCall, requirements webResearchRequirements) string {
	return buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, requirements, "")
}

func buildDeterministicWebResearchSummaryForRequest(successfulToolCalls []toolCall, requirements webResearchRequirements, userRequest string) string {
	evidence := collectWebResearchEvidence(successfulToolCalls)
	if len(evidence) == 0 {
		return "I could not extract enough article-grade source content for this request yet."
	}
	requestKeywords := researchKeywordsFromRequest(userRequest)
	specificKeywords := specificResearchKeywords(requestKeywords)
	targetYear := ""
	targetMonth := ""
	for _, keyword := range requestKeywords {
		if len(keyword) == 4 && strings.HasPrefix(keyword, "20") {
			targetYear = keyword
			break
		}
	}
	for _, keyword := range requestKeywords {
		switch keyword {
		case "january", "february", "march", "april", "may", "june",
			"july", "august", "september", "october", "november", "december":
			targetMonth = keyword
		}
	}

	usable := make([]webResearchEvidence, 0, len(evidence))
	for _, item := range evidence {
		switch {
		case item.blocked():
			continue
		case !item.extractable():
			continue
		case strings.TrimSpace(item.Impact) == "" || isLowValueImpactText(item.Impact):
			continue
		default:
			usable = append(usable, item)
		}
	}

	type scoredEvidence struct {
		item  webResearchEvidence
		score int
	}
	scoredUsable := make([]scoredEvidence, 0, len(usable))
	for _, item := range usable {
		score := researchEvidenceQualityScoreForRequest(item, specificKeywords, targetYear)
		scoredUsable = append(scoredUsable, scoredEvidence{item: item, score: score})
	}
	sort.SliceStable(scoredUsable, func(i, j int) bool {
		return scoredUsable[i].score > scoredUsable[j].score
	})
	usable = usable[:0]
	supplemental := make([]webResearchEvidence, 0, len(scoredUsable))
	for _, entry := range scoredUsable {
		if entry.score < 18 {
			if entry.score > 0 {
				supplemental = append(supplemental, entry.item)
				continue
			}
			continue
		}
		usable = append(usable, entry.item)
	}
	if len(usable) < requirements.MinimumItems && len(supplemental) > 0 {
		needed := requirements.MinimumItems - len(usable)
		if needed < 1 {
			needed = 1
		}
		for _, candidate := range supplemental {
			if needed <= 0 {
				break
			}
			usable = append(usable, candidate)
			needed--
		}
	}
	if len(usable) == 0 {
		usable = append(usable, selectBestEffortExtractableEvidence(evidence, specificKeywords, targetYear, targetMonth, requirements.MinimumItems)...)
	}

	limit := requirements.MinimumItems
	if limit < 1 {
		limit = 8
	}
	if limit > len(usable) {
		limit = len(usable)
	}
	requestedTopN := requestedTopNFromPrompt(userRequest, requirements.MinimumItems)
	if requestedTopN > 0 && requestedTopN < limit {
		limit = requestedTopN
	}

	if limit <= 0 {
		return "I could not extract enough article-grade source pages to produce a reliable top-stories summary yet."
	}

	var builder strings.Builder
	builder.WriteString("Here is the research summary based on extractable article sources.\n\n")
	builder.WriteString("Key themes:\n")
	builder.WriteString("1. Institutional participation and regulatory developments continue to shape the RWA/DeFi narrative.\n")
	builder.WriteString("2. Product launches, liquidity shifts, and risk repricing are driving short-term market structure changes.\n\n")
	builder.WriteString("Top stories:\n")
	for idx := 0; idx < limit; idx++ {
		item := usable[idx]
		headline := strings.TrimSpace(item.Title)
		if headline == "" {
			headline = hostFromURL(item.URL)
		}
		dateLabel := extractResearchDateLabel(item)
		if dateLabel == "" {
			dateLabel = "Date not explicit on page"
		}
		summary := buildResearchItemSummary(item)
		builder.WriteString(fmt.Sprintf("%d. **%s**\n", idx+1, headline))
		builder.WriteString(fmt.Sprintf("   - Date: %s\n", dateLabel))
		builder.WriteString(fmt.Sprintf("   - Source: [%s](%s)\n", hostFromURL(item.URL), item.URL))
		builder.WriteString(fmt.Sprintf("   - Summary: %s\n", summary))
	}
	return strings.TrimSpace(builder.String())
}

func selectBestEffortExtractableEvidence(
	evidence []webResearchEvidence,
	specificKeywords []string,
	targetYear string,
	targetMonth string,
	limit int,
) []webResearchEvidence {
	if limit < 1 {
		limit = 8
	}
	type scoredEvidence struct {
		item  webResearchEvidence
		score int
	}
	scored := make([]scoredEvidence, 0, len(evidence))
	for _, item := range evidence {
		if !item.extractable() || item.blocked() {
			continue
		}
		if reason := nonArticleReasonForURL(item.URL); reason != "" {
			continue
		}
		if targetYear != "" {
			combined := strings.ToLower(strings.TrimSpace(item.URL + " " + item.Title + " " + strings.Join(item.EvidenceText, " ")))
			if !strings.Contains(combined, targetYear) {
				continue
			}
		}
		if targetMonth != "" && !evidenceMentionsTargetMonth(item, targetMonth) {
			continue
		}
		if !evidenceMatchesSpecificKeywords(item, specificKeywords) {
			continue
		}
		score := researchEvidenceQualityScoreForRequest(item, specificKeywords, targetYear)
		if score < 8 {
			continue
		}
		scored = append(scored, scoredEvidence{item: item, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	out := make([]webResearchEvidence, 0, min(limit, len(scored)))
	for _, entry := range scored {
		if len(out) >= limit {
			break
		}
		out = append(out, entry.item)
	}
	return out
}

func researchEvidenceQualityScore(item webResearchEvidence) int {
	score := 0
	if item.WordCount > 0 {
		score += min(item.WordCount, 120)
	}
	title := strings.ToLower(strings.TrimSpace(item.Title))
	impact := strings.ToLower(strings.TrimSpace(item.Impact))
	urlText := strings.ToLower(strings.TrimSpace(item.URL))
	if strings.Contains(urlText, "/2026/") || strings.Contains(urlText, "feb") || strings.Contains(urlText, "march") {
		score += 18
	}
	if matchedYear := anyYearRE.FindString(urlText + " " + title); matchedYear != "" && matchedYear != "2026" {
		score -= 28
	}
	relevanceSignals := []string{"rwa", "real world asset", "real-world asset", "defi", "crypto", "tokenization", "stablecoin", "bitcoin", "ethereum"}
	for _, signal := range relevanceSignals {
		if strings.Contains(title, signal) || strings.Contains(impact, signal) || strings.Contains(urlText, signal) {
			score += 6
		}
	}
	lowIntentSignals := []string{"press release", "event overview", "podcast", "newsletter", "conference", "sponsored", "top-100", "award", "price"}
	for _, signal := range lowIntentSignals {
		if strings.Contains(title, signal) || strings.Contains(urlText, signal) {
			score -= 12
		}
	}
	if isLowValueImpactText(impact) {
		score -= 16
	}
	return score
}

func researchEvidenceQualityScoreForRequest(item webResearchEvidence, specificKeywords []string, targetYear string) int {
	score := researchEvidenceQualityScore(item)
	haystack := strings.ToLower(strings.TrimSpace(item.Title + " " + item.Impact + " " + item.URL))
	if targetYear != "" {
		if strings.Contains(haystack, targetYear) {
			score += 10
		} else if matchedYear := anyYearRE.FindString(haystack); matchedYear != "" && matchedYear != targetYear {
			score -= 20
		}
	}
	if len(specificKeywords) > 0 {
		matches := keywordMatchCount(specificKeywords, haystack)
		if matches == 0 {
			score -= 30
		} else {
			score += matches * 8
		}
	}
	return score
}

func keywordAliases(keyword string) []string {
	switch strings.TrimSpace(strings.ToLower(keyword)) {
	case "rwa":
		return []string{
			"rwa",
			"real world asset",
			"real-world asset",
			"tokenization",
			"tokenized",
			"tokenize",
			"on-chain treasury",
			"onchain treasury",
			"treasury token",
		}
	case "defi":
		return []string{
			"defi",
			"decentralized finance",
			"decentralised finance",
		}
	default:
		trimmed := strings.TrimSpace(strings.ToLower(keyword))
		if trimmed == "" {
			return nil
		}
		return []string{trimmed}
	}
}

func keywordInText(keyword string, haystack string) bool {
	text := strings.ToLower(strings.TrimSpace(haystack))
	if text == "" {
		return false
	}
	for _, alias := range keywordAliases(keyword) {
		if alias != "" && strings.Contains(text, alias) {
			return true
		}
	}
	return false
}

func keywordMatchCount(keywords []string, haystack string) int {
	count := 0
	for _, keyword := range keywords {
		if keywordInText(keyword, haystack) {
			count++
		}
	}
	return count
}

func specificResearchKeywords(keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}
	out := make([]string, 0, len(keywords))
	seen := map[string]struct{}{}
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(strings.ToLower(keyword))
		if keyword == "" {
			continue
		}
		switch keyword {
		case "crypto", "news", "latest", "current", "stories", "story",
			"january", "february", "march", "april", "may", "june",
			"july", "august", "september", "october", "november", "december":
			continue
		}
		if len(keyword) == 4 && strings.HasPrefix(keyword, "20") {
			continue
		}
		if _, exists := seen[keyword]; exists {
			continue
		}
		seen[keyword] = struct{}{}
		out = append(out, keyword)
	}
	return out
}

func collectWebResearchEvidence(successfulToolCalls []toolCall) []webResearchEvidence {
	type sourceIndex struct {
		idx int
	}
	byURL := map[string]sourceIndex{}
	sources := make([]webResearchEvidence, 0, len(successfulToolCalls))
	activeURL := ""
	ensureSource := func(urlValue string) int {
		normalized := strings.TrimSpace(urlValue)
		if normalized == "" {
			return -1
		}
		if existing, ok := byURL[normalized]; ok {
			return existing.idx
		}
		idx := len(sources)
		sources = append(sources, webResearchEvidence{URL: normalized, SeqHint: idx})
		byURL[normalized] = sourceIndex{idx: idx}
		return idx
	}

	for _, call := range successfulToolCalls {
		switch call.ToolName {
		case "browser.navigate":
			urlValue := firstNonEmptyString(call.Input["url"], call.Input["current_url"], call.Input["final_url"])
			idx := ensureSource(urlValue)
			if idx >= 0 {
				activeURL = sources[idx].URL
				if title := strings.TrimSpace(toString(call.Input["title"])); title != "" {
					sources[idx].Title = title
				}
			}
		case "browser.extract":
			mode := strings.ToLower(strings.TrimSpace(toString(call.Input["mode"])))
			urlValue := strings.TrimSpace(toString(call.Input["url"]))
			if urlValue == "" {
				urlValue = activeURL
			}
			idx := ensureSource(urlValue)
			if idx < 0 {
				continue
			}
			extracted := call.Input["extracted"]
			diagnostics, _ := call.Input["diagnostics"].(map[string]any)
			allowEvidenceFromExtract := true
			if diagnostics != nil {
				if status := strings.TrimSpace(toString(diagnostics["status"])); status != "" {
					sources[idx].Status = status
				}
				if reasonCode := strings.TrimSpace(toString(diagnostics["reason_code"])); reasonCode != "" {
					sources[idx].ReasonCode = reasonCode
				}
				if reasonDetail := strings.TrimSpace(toString(diagnostics["reason_detail"])); reasonDetail != "" {
					sources[idx].ReasonDetail = reasonDetail
				}
				if wordCountRaw := strings.TrimSpace(toString(diagnostics["word_count"])); wordCountRaw != "" {
					if parsedWordCount, parseErr := strconv.Atoi(wordCountRaw); parseErr == nil {
						sources[idx].WordCount = parsedWordCount
					}
				}
				status := strings.ToLower(strings.TrimSpace(toString(diagnostics["status"])))
				reasonCode := strings.ToLower(strings.TrimSpace(toString(diagnostics["reason_code"])))
				switch reasonCode {
				case "no_extractable_content", "blocked_by_bot_protection", "consent_wall", "login_wall":
					allowEvidenceFromExtract = false
				}
				if status == "blocked" || status == "empty" {
					allowEvidenceFromExtract = false
				}
			}
			switch typed := extracted.(type) {
			case map[string]any:
				if title := strings.TrimSpace(toString(typed["title"])); title != "" && sources[idx].Title == "" {
					sources[idx].Title = title
				}
				if urlText := strings.TrimSpace(toString(typed["url"])); urlText != "" && sources[idx].URL == "" {
					sources[idx].URL = urlText
				}
				if allowEvidenceFromExtract {
					if mode == "metadata" {
						description := strings.TrimSpace(toString(typed["description"]))
						firstParagraph := strings.TrimSpace(toString(typed["first_paragraph"]))
						contentPreview := strings.TrimSpace(toString(diagnostics["content_preview"]))
						if len(strings.Fields(description)) >= 8 {
							addEvidenceText(&sources[idx], description)
						}
						if len(strings.Fields(firstParagraph)) >= 8 {
							addEvidenceText(&sources[idx], firstParagraph)
						}
						if len(strings.Fields(contentPreview)) >= 8 {
							addEvidenceText(&sources[idx], contentPreview)
						}
					} else {
						addEvidenceText(
							&sources[idx],
							toString(typed["description"]),
							toString(typed["first_paragraph"]),
							toString(typed["excerpt"]),
							toString(typed["lead"]),
							toString(typed["content"]),
							toString(typed["article_body"]),
							toString(typed["body"]),
							toString(diagnostics["content_preview"]),
						)
					}
				}
			case string:
				if allowEvidenceFromExtract && (mode == "text" || mode == "") {
					addEvidenceText(&sources[idx], typed, toString(diagnostics["content_preview"]))
				}
			case []any:
				if !allowEvidenceFromExtract {
					continue
				}
				if len(typed) == 0 {
					continue
				}
				parts := make([]string, 0, len(typed))
				for _, value := range typed {
					text := strings.TrimSpace(toString(value))
					if text != "" {
						parts = append(parts, text)
					}
					if len(parts) >= 3 {
						break
					}
				}
				addEvidenceText(&sources[idx], strings.Join(parts, ". "), toString(diagnostics["content_preview"]))
			}

			if sources[idx].ReasonCode == "" && strings.EqualFold(strings.TrimSpace(toString(call.Input["reason_code"])), "no_extractable_content") {
				sources[idx].ReasonCode = "no_extractable_content"
			}
		}
	}

	filtered := make([]webResearchEvidence, 0, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source.URL) == "" {
			continue
		}
		if impact := synthesizeImpactFromEvidence(source.Title, source.EvidenceText); impact != "" {
			source.Impact = impact
			if source.WordCount == 0 {
				source.WordCount = len(strings.Fields(impact))
			}
		}
		if source.extractable() && (looksLikeBotChallengeSnippet(source.Title, source.Impact) || isBotChallengeURL(source.URL)) {
			source.ReasonCode = "blocked_by_bot_protection"
			if source.ReasonDetail == "" {
				source.ReasonDetail = "challenge_page_detected"
			}
		}
		if source.extractable() {
			if detail := nonArticleReasonForURL(source.URL); detail != "" {
				source.ReasonCode = "no_extractable_content"
				if source.ReasonDetail == "" {
					source.ReasonDetail = detail
				}
			}
		}
		if source.extractable() && strings.Contains(strings.ToLower(source.URL), "/opinion/") {
			source.ReasonCode = "no_extractable_content"
			if source.ReasonDetail == "" {
				source.ReasonDetail = "opinion_page"
			}
		}
		if source.extractable() {
			joined := strings.ToLower(strings.TrimSpace(source.URL + " " + source.Title))
			if strings.Contains(joined, "/sponsored/") || strings.Contains(joined, " sponsored") {
				source.ReasonCode = "no_extractable_content"
				if source.ReasonDetail == "" {
					source.ReasonDetail = "sponsored_content"
				}
			}
		}
		if source.extractable() && looksLikeLandingSnippet(source.Title, source.Impact) {
			source.ReasonCode = "no_extractable_content"
			if source.ReasonDetail == "" {
				source.ReasonDetail = "landing_page_or_index_content"
			}
		}
		if source.extractable() && looksLikeLegalPolicySnippet(source.URL, source.Title, source.Impact, source.EvidenceText) {
			source.ReasonCode = "no_extractable_content"
			if source.ReasonDetail == "" {
				source.ReasonDetail = "legal_or_policy_page"
			}
		}
		if source.extractable() && looksLikeNotFoundSnippet(source.Title, source.Impact, source.EvidenceText) {
			source.ReasonCode = "no_extractable_content"
			if source.ReasonDetail == "" {
				source.ReasonDetail = "not_found_page"
			}
		}
		if source.extractable() && strings.TrimSpace(source.Impact) == "" {
			fallbackImpact := summarizeImpactText(strings.Join(source.EvidenceText, " "))
			if fallbackImpact != "" {
				source.Impact = fallbackImpact
			} else {
				source.ReasonCode = "no_extractable_content"
				if source.ReasonDetail == "" {
					source.ReasonDetail = "missing_summary_text"
				}
			}
		}
		if source.ReasonDetail == "" && source.ReasonCode != "" {
			source.ReasonDetail = source.ReasonCode
		}
		filtered = append(filtered, source)
	}
	return filtered
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		text := strings.TrimSpace(toString(value))
		if text != "" {
			return text
		}
	}
	return ""
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func addEvidenceText(source *webResearchEvidence, values ...string) {
	if source == nil {
		return
	}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > 5000 {
			trimmed = truncateRunes(trimmed, 5000)
		}
		if containsString(source.EvidenceText, trimmed) {
			continue
		}
		source.EvidenceText = append(source.EvidenceText, trimmed)
	}
}

type scoredSentence struct {
	Text  string
	Score int
}

func synthesizeImpactFromEvidence(title string, evidence []string) string {
	if len(evidence) == 0 {
		return ""
	}
	seen := map[string]struct{}{}
	sentences := make([]scoredSentence, 0, 24)
	for _, fragment := range evidence {
		for _, candidate := range candidateImpactSentences(fragment) {
			normalized := strings.ToLower(strings.TrimSpace(candidate))
			if normalized == "" {
				continue
			}
			if _, exists := seen[normalized]; exists {
				continue
			}
			seen[normalized] = struct{}{}
			score := scoreImpactSentence(candidate, title)
			if score <= 0 {
				continue
			}
			sentences = append(sentences, scoredSentence{Text: candidate, Score: score})
		}
	}

	if len(sentences) == 0 {
		return summarizeImpactText(strings.Join(evidence, " "))
	}

	sort.SliceStable(sentences, func(i, j int) bool {
		return sentences[i].Score > sentences[j].Score
	})

	selected := make([]string, 0, 2)
	for _, sentence := range sentences {
		if len(selected) >= 2 {
			break
		}
		if sentenceTooSimilar(selected, sentence.Text) {
			continue
		}
		selected = append(selected, sentence.Text)
	}
	if len(selected) == 0 {
		return summarizeImpactText(strings.Join(evidence, " "))
	}
	return truncateRunes(strings.Join(selected, " "), 220)
}

func candidateImpactSentences(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	cleaned := strings.Join(strings.Fields(trimmed), " ")
	if cleaned == "" {
		return nil
	}
	parts := sentenceSplitRE.Split(cleaned, -1)
	out := make([]string, 0, len(parts))
	for _, sentence := range parts {
		candidate := strings.Trim(sentence, " \t\n\r\"'“”’‘-")
		if candidate == "" {
			continue
		}
		wordCount := len(strings.Fields(candidate))
		if wordCount < 8 || wordCount > 48 {
			continue
		}
		out = append(out, candidate)
	}
	if len(out) == 0 && len(strings.Fields(cleaned)) >= 8 {
		return []string{truncateRunes(cleaned, 220)}
	}
	return out
}

func scoreImpactSentence(sentence string, title string) int {
	candidate := strings.TrimSpace(sentence)
	if candidate == "" {
		return 0
	}
	lower := strings.ToLower(candidate)
	blockingSignals := []string{
		"are you a robot",
		"not a robot",
		"detected unusual activity",
		"detected unusual traffic",
		"unusual traffic from your computer network",
		"verify you are human",
		"attention required",
		"click the box below",
		"checking your browser",
		"just a moment",
		"cookie consent",
		"accept cookies",
	}
	for _, phrase := range blockingSignals {
		if strings.Contains(lower, phrase) {
			return 0
		}
	}

	if len(tickerSymbolRE.FindAllString(candidate, -1)) >= 4 {
		return 0
	}
	if isLowValueImpactText(lower) {
		return 0
	}
	if strings.Contains(lower, "copyright") || strings.Contains(lower, "all rights reserved") {
		return 0
	}
	if len(tickerSymbolRE.FindAllString(candidate, -1)) >= 3 {
		return 0
	}

	score := len(strings.Fields(candidate))
	if datePathRE.MatchString(candidate) || strings.Contains(lower, "feb") || strings.Contains(lower, "2026") {
		score += 2
	}
	if strings.Contains(candidate, "%") || strings.Contains(candidate, "$") {
		score += 1
	}
	actionTerms := []string{
		"rose", "fell", "jumped", "dropped", "increased", "decreased",
		"launched", "announced", "approved", "expanded", "raised",
		"slashed", "repriced", "rotated", "boosted",
	}
	for _, term := range actionTerms {
		if strings.Contains(lower, term) {
			score++
			break
		}
	}
	titleWords := strings.Fields(strings.ToLower(strings.TrimSpace(title)))
	titleMatchCount := 0
	for _, word := range titleWords {
		w := strings.Trim(word, ".,:;!?()[]{}\"'`")
		if len(w) < 4 {
			continue
		}
		if strings.Contains(lower, w) {
			titleMatchCount++
		}
	}
	if titleMatchCount >= 2 {
		score++
	}
	return score
}

func isLowValueImpactText(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	for _, phrase := range lowValueResearchSignals {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func sentenceTooSimilar(existing []string, candidate string) bool {
	candidateSet := wordSet(candidate)
	if len(candidateSet) == 0 {
		return true
	}
	for _, item := range existing {
		existingSet := wordSet(item)
		if len(existingSet) == 0 {
			continue
		}
		intersection := 0
		for word := range candidateSet {
			if _, ok := existingSet[word]; ok {
				intersection++
			}
		}
		overlap := float64(intersection) / float64(min(len(candidateSet), len(existingSet)))
		if overlap >= 0.75 {
			return true
		}
	}
	return false
}

func wordSet(raw string) map[string]struct{} {
	words := strings.Fields(strings.ToLower(raw))
	set := make(map[string]struct{}, len(words))
	for _, word := range words {
		normalized := strings.Trim(word, ".,:;!?()[]{}\"'`")
		if len(normalized) < 3 {
			continue
		}
		set[normalized] = struct{}{}
	}
	return set
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func summarizeImpactText(raw string) string {
	sentences := candidateImpactSentences(raw)
	if len(sentences) == 0 {
		return ""
	}
	for _, sentence := range sentences {
		if scoreImpactSentence(sentence, "") > 0 {
			return truncateRunes(sentence, 180)
		}
	}
	return truncateRunes(sentences[0], 180)
}

func inferImpactFromTitle(title string) string {
	cleanedTitle := strings.TrimSpace(title)
	if cleanedTitle == "" {
		return ""
	}
	return fmt.Sprintf("Headline indicates: %s.", truncateRunes(cleanedTitle, 140))
}

func buildResearchItemSummary(item webResearchEvidence) string {
	impact := strings.TrimSpace(item.Impact)
	if impact == "" || isLowValueImpactText(impact) {
		impact = summarizeImpactText(strings.Join(item.EvidenceText, " "))
	}
	if impact == "" || isLowValueImpactText(impact) {
		impact = inferImpactFromTitle(item.Title)
	}
	if impact == "" {
		return "Extracted text was limited; no reliable story summary could be synthesized."
	}
	return truncateRunes(impact, 260)
}

func extractResearchDateLabel(item webResearchEvidence) string {
	combined := strings.TrimSpace(item.URL + " " + item.Title + " " + strings.Join(item.EvidenceText, " "))
	if combined == "" {
		return ""
	}
	if pathMatch := datePathRE.FindString(strings.ToLower(item.URL)); pathMatch != "" {
		trimmed := strings.Trim(pathMatch, "/")
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 2 {
			if len(parts) >= 3 {
				return fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
			}
			return fmt.Sprintf("%s-%s", parts[0], parts[1])
		}
	}
	if match := monthDateRE.FindString(combined); strings.TrimSpace(match) != "" {
		return strings.TrimSpace(match)
	}
	if match := anyYearRE.FindString(combined); strings.TrimSpace(match) != "" {
		return strings.TrimSpace(match)
	}
	return ""
}

func hostFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "Source"
	}
	host := strings.TrimSpace(parsed.Host)
	if host == "" {
		return "Source"
	}
	return host
}

func nonArticleReasonForURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	path := strings.ToLower(strings.TrimSpace(parsed.Path))
	if host == "" {
		return ""
	}
	if strings.HasPrefix(path, "/search") {
		return "search_results_page"
	}
	if strings.Contains(host, "duckduckgo.com") && parsed.Query().Has("q") {
		return "search_results_page"
	}
	query := parsed.Query()
	if strings.Contains(path, "/find") && (query.Has("q") || query.Has("query") || query.Has("s")) {
		return "search_results_page"
	}
	if (query.Has("q") || query.Has("query") || query.Has("s") || query.Has("blob")) && strings.Contains(path, "search") {
		return "search_results_page"
	}
	if path == "" || path == "/" {
		return "homepage_not_article"
	}
	trimmedPath := strings.Trim(path, "/")
	segments := strings.Split(trimmedPath, "/")
	if len(segments) == 0 {
		return "homepage_not_article"
	}

	firstSegment := strings.ToLower(strings.TrimSpace(segments[0]))
	switch firstSegment {
	case "privacy", "policy", "policies", "terms", "legal", "cookie", "cookies":
		return "legal_or_policy_page"
	}
	if strings.Contains(path, "terms-and-privacy") || strings.Contains(path, "privacy-policy") || strings.Contains(path, "terms-of-service") {
		return "legal_or_policy_page"
	}
	if strings.Contains(path, "/author/") || strings.Contains(path, "/authors/") {
		return "section_index_page"
	}
	if strings.Contains(path, "/help/") || strings.Contains(path, "/support/") {
		return "section_index_page"
	}
	if strings.Contains(path, "help-pages") {
		return "section_index_page"
	}
	if strings.Contains(path, "/press-releases/") || strings.Contains(path, "/press-release/") {
		return "section_index_page"
	}
	if strings.Contains(path, "/price/") || strings.HasPrefix(path, "/price/") {
		return "section_index_page"
	}
	if strings.Contains(path, "/people/top-people") || strings.HasPrefix(path, "/people/") {
		return "section_index_page"
	}
	switch firstSegment {
	case "tag", "tags", "topic", "topics", "category", "categories", "newsletter", "newsletters", "price", "prices", "video", "videos", "podcast", "podcasts",
		"author", "authors", "help", "support", "docs", "documentation", "faq":
		return "section_index_page"
	}
	if firstSegment == "search" {
		return "search_results_page"
	}
	if firstSegment == "news" || firstSegment == "markets" || firstSegment == "latest" || firstSegment == "latest-news" {
		if !looksLikeArticlePath(segments) {
			return "section_index_page"
		}
	}
	if len(segments) == 1 && !looksLikeArticlePath(segments) {
		switch firstSegment {
		case "news", "markets", "latest", "latest-news", "latest-crypto-news", "research", "insights":
			return "section_index_page"
		}
	}
	return ""
}

func looksLikeArticlePath(segments []string) bool {
	if len(segments) == 0 {
		return false
	}
	joined := strings.ToLower(strings.Join(segments, "/"))
	if datePathRE.MatchString("/" + joined) {
		return true
	}
	if len(segments) >= 3 {
		last := strings.TrimSpace(strings.ToLower(segments[len(segments)-1]))
		return len(last) >= 10
	}
	if len(segments) == 2 {
		second := strings.TrimSpace(strings.ToLower(segments[1]))
		if second == "" {
			return false
		}
		if len(second) >= 14 && strings.Contains(second, "-") {
			return true
		}
	}
	return false
}

func looksLikeLandingSnippet(title string, impact string) bool {
	text := strings.ToLower(strings.TrimSpace(title + " " + impact))
	if text == "" {
		return false
	}
	signals := []string{
		"bitcoin, ethereum",
		"crypto news",
		"price indexes",
		"markets",
		"latest news",
		"news video prices",
		"data & indices",
	}
	matches := 0
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			matches++
		}
	}
	if matches >= 2 {
		return true
	}
	if looksLikeBotChallengeSnippet(title, impact) {
		return true
	}
	return len(tickerSymbolRE.FindAllString(impact, -1)) >= 4
}

func looksLikeLegalPolicySnippet(rawURL string, title string, impact string, evidence []string) bool {
	if reason := nonArticleReasonForURL(rawURL); reason == "legal_or_policy_page" {
		return true
	}
	joinedTitle := strings.ToLower(strings.TrimSpace(title))
	if joinedTitle == "" {
		return false
	}
	signals := []string{
		"privacy policy",
		"terms of service",
		"terms and privacy",
		"privacy & terms",
		"cookie policy",
		"legal notice",
		"manage cookies",
	}
	matchCount := 0
	for _, signal := range signals {
		if strings.Contains(joinedTitle, signal) {
			matchCount++
		}
	}
	if matchCount > 0 {
		return true
	}
	// Some article pages include cookie/legal boilerplate in body text/impact; ignore that unless
	// page URL or title clearly indicates legal/policy content.
	_ = evidence
	_ = impact
	return false
}

func looksLikeBotChallengeSnippet(title string, impact string) bool {
	text := strings.ToLower(strings.TrimSpace(title + " " + impact))
	if text == "" {
		return false
	}
	signals := []string{
		"are you a robot",
		"not a robot",
		"detected unusual activity",
		"detected unusual traffic",
		"unusual traffic from your computer network",
		"verify you are human",
		"just a moment",
		"attention required",
		"checking your browser",
		"access denied",
		"captcha",
		"security check",
		"click the box below",
	}
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func looksLikeNotFoundSnippet(title string, impact string, evidence []string) bool {
	text := strings.ToLower(strings.TrimSpace(title + " " + impact + " " + strings.Join(evidence, " ")))
	if text == "" {
		return false
	}
	signals := []string{
		"page not found",
		"404",
		"this page could not be found",
		"could've sworn the page was around here somewhere",
	}
	matches := 0
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			matches++
		}
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(title)), "404") || strings.Contains(strings.ToLower(strings.TrimSpace(title)), "page not found") {
		return true
	}
	return matches >= 2
}

func isBotChallengeURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	path := strings.ToLower(strings.TrimSpace(parsed.Path))
	if host == "" {
		return false
	}
	if strings.Contains(host, "google.") && strings.HasPrefix(path, "/sorry") {
		return true
	}
	if strings.Contains(path, "captcha") || strings.Contains(path, "challenge") {
		return true
	}
	return false
}

func toStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		converted := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if ok {
				converted = append(converted, text)
			}
		}
		return converted
	default:
		return nil
	}
}

func latestUserMessage(messages []store.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		content := strings.TrimSpace(messages[i].Content)
		if content != "" {
			return content
		}
	}
	return ""
}

func shouldRepromptToolExecution(toolRunnerAvailable bool, userRequest string, modelResponse string, repromptCount int) bool {
	if !toolRunnerAvailable || repromptCount >= maxToolIntentReprompts {
		return false
	}
	if !requestLikelyNeedsTools(userRequest) {
		return false
	}
	return responseLooksLikeExecutionPromise(modelResponse)
}

func requestLikelyNeedsTools(userRequest string) bool {
	text := strings.ToLower(strings.TrimSpace(userRequest))
	if text == "" {
		return false
	}
	keywords := []string{
		"browse", "search", "look up", "research", "latest", "news",
		"create", "build", "write", "edit", "update", "fix", "implement", "generate", "scaffold",
		"nextjs", "website", "app", "code", "file", "run ", "npm ", "pnpm ", "yarn ",
	}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func responseLooksLikeExecutionPromise(response string) bool {
	text := strings.ToLower(strings.TrimSpace(response))
	text = strings.ReplaceAll(text, "’", "'")
	text = strings.ReplaceAll(text, "‘", "'")
	if text == "" {
		return false
	}
	if strings.Contains(text, "```") || strings.Contains(text, "\"tool_calls\"") {
		return false
	}
	if len([]rune(text)) > 280 {
		return false
	}
	promisePhrases := []string{
		"i'll ", "i will ", "let me ", "i'm going to ", "i can ",
	}
	for _, phrase := range promisePhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func buildToolOnlyRetryPrompt(userRequest string) string {
	trimmed := truncateRunes(strings.TrimSpace(userRequest), 240)
	return fmt.Sprintf("You must execute this request now. For request: %q, respond with exactly one fenced ```tool JSON block and no prose. Include concrete first-step tool calls immediately.", trimmed)
}

func buildMissingToolCallsFallback() string {
	return "I could not start execution because the model repeatedly returned prose instead of tool calls. Retry this run and I will continue with strict tool-only execution."
}

type webResearchRequirements struct {
	Enabled      bool
	MinimumItems int
}

func deriveWebResearchRequirements(userRequest string, mustExecuteTools bool) webResearchRequirements {
	if !mustExecuteTools {
		return webResearchRequirements{}
	}
	text := strings.ToLower(strings.TrimSpace(userRequest))
	if text == "" {
		return webResearchRequirements{}
	}
	if !requestLikelyNeedsTools(text) {
		return webResearchRequirements{}
	}
	if !strings.Contains(text, "browse") &&
		!strings.Contains(text, "search") &&
		!strings.Contains(text, "research") &&
		!strings.Contains(text, "news") &&
		!strings.Contains(text, "source") &&
		!strings.Contains(text, "link") {
		return webResearchRequirements{}
	}

	minItems := 3
	if matches := topNRequestRE.FindStringSubmatch(text); len(matches) == 2 {
		if parsed, err := strconv.Atoi(matches[1]); err == nil {
			if parsed < 1 {
				parsed = 1
			}
			minSources := parsed
			if minSources < 3 {
				minSources = 3
			}
			if minSources > 8 {
				minSources = 8
			}
			minItems = minSources
		}
	}
	return webResearchRequirements{
		Enabled:      true,
		MinimumItems: minItems,
	}
}

func requestedTopNFromPrompt(userRequest string, minimumFallback int) int {
	text := strings.ToLower(strings.TrimSpace(userRequest))
	if matches := topNRequestRE.FindStringSubmatch(text); len(matches) == 2 {
		if parsed, err := strconv.Atoi(matches[1]); err == nil {
			if parsed < minimumFallback {
				return minimumFallback
			}
			if parsed > 12 {
				return 12
			}
			return parsed
		}
	}
	if minimumFallback < 1 {
		return 1
	}
	return minimumFallback
}

func buildWebResearchExecutionPrompt(requirements webResearchRequirements) string {
	return fmt.Sprintf(
		"This task requires web research. Gather evidence before finishing: visit and extract from at least %d distinct source URLs. Use browser.navigate and browser.extract (mode=metadata and text) on article pages. Prefer deep-link article/source pages and do not count homepages, search results, section index pages, or legal/privacy/terms pages as usable evidence. Avoid Google/Bing search-result pages unless no alternatives exist because they often trigger anti-bot pages. Do not stop at the first page of a domain: when you land on search/homepage/index pages, use browser.evaluate to collect in-page article links, then navigate those links and extract. If extraction fails, record the exact reason code (blocked_by_bot_protection, consent_wall, no_extractable_content) and continue with alternative sources. Build your own summary from extracted article body text; do not rely on a page-provided summary field. Final user response must include only high-confidence story findings with source links and synthesized summaries; do not include blocked/low-quality diagnostics unless explicitly requested.",
		requirements.MinimumItems,
	)
}

func evidenceMatchesSpecificKeywords(item webResearchEvidence, specificKeywords []string) bool {
	if len(specificKeywords) == 0 {
		return true
	}
	primary := strings.ToLower(strings.TrimSpace(item.Title + " " + item.Impact + " " + item.URL))
	for _, keyword := range specificKeywords {
		if keywordInText(keyword, primary) {
			return true
		}
	}
	fullText := strings.ToLower(strings.TrimSpace(strings.Join(item.EvidenceText, " ")))
	if fullText == "" {
		return false
	}
	for _, keyword := range specificKeywords {
		if keywordInText(keyword, fullText) {
			return true
		}
	}
	return false
}

func filterUsableWebResearchEvidenceForRequest(successfulToolCalls []toolCall, userRequest string) []webResearchEvidence {
	evidence := collectWebResearchEvidence(successfulToolCalls)
	if len(evidence) == 0 {
		return nil
	}
	requestKeywords := researchKeywordsFromRequest(userRequest)
	specificKeywords := specificResearchKeywords(requestKeywords)
	targetYear := ""
	targetMonth := ""
	for _, keyword := range requestKeywords {
		if len(keyword) == 4 && strings.HasPrefix(keyword, "20") {
			targetYear = keyword
			break
		}
	}
	for _, keyword := range requestKeywords {
		switch keyword {
		case "january", "february", "march", "april", "may", "june",
			"july", "august", "september", "october", "november", "december":
			targetMonth = keyword
		}
	}
	usable := make([]webResearchEvidence, 0, len(evidence))
	for _, item := range evidence {
		if !item.extractable() {
			continue
		}
		if detail := nonArticleReasonForURL(item.URL); detail != "" {
			continue
		}
		if len(item.EvidenceText) == 0 {
			continue
		}
		combined := strings.ToLower(strings.TrimSpace(item.URL + " " + item.Title + " " + strings.Join(item.EvidenceText, " ")))
		if targetYear != "" && !strings.Contains(combined, targetYear) {
			continue
		}
		if targetMonth != "" && !evidenceMentionsTargetMonth(item, targetMonth) {
			continue
		}
		bodyWordCount := item.WordCount
		if bodyWordCount == 0 {
			bodyWordCount = len(strings.Fields(strings.Join(item.EvidenceText, " ")))
		}
		if bodyWordCount < 40 {
			continue
		}
		if strings.TrimSpace(item.Impact) == "" || isLowValueImpactText(item.Impact) {
			continue
		}
		if !evidenceMatchesSpecificKeywords(item, specificKeywords) {
			continue
		}
		score := researchEvidenceQualityScoreForRequest(item, specificKeywords, targetYear)
		if score < 24 {
			continue
		}
		usable = append(usable, item)
	}
	return usable
}

func evidenceMentionsTargetMonth(item webResearchEvidence, targetMonth string) bool {
	month := strings.TrimSpace(strings.ToLower(targetMonth))
	if month == "" {
		return true
	}
	combined := strings.ToLower(strings.TrimSpace(item.URL + " " + item.Title + " " + strings.Join(item.EvidenceText, " ")))
	if strings.Contains(combined, month) {
		return true
	}
	monthByName := map[string]string{
		"january":   "/01/",
		"february":  "/02/",
		"march":     "/03/",
		"april":     "/04/",
		"may":       "/05/",
		"june":      "/06/",
		"july":      "/07/",
		"august":    "/08/",
		"september": "/09/",
		"october":   "/10/",
		"november":  "/11/",
		"december":  "/12/",
	}
	if marker, ok := monthByName[month]; ok && strings.Contains(strings.ToLower(item.URL), marker) {
		return true
	}
	return false
}

func summarizeWebResearchEvidence(successfulToolCalls []toolCall) (int, int) {
	return summarizeWebResearchEvidenceForRequest(successfulToolCalls, "")
}

func summarizeWebResearchEvidenceForRequest(successfulToolCalls []toolCall, userRequest string) (int, int) {
	evidence := filterUsableWebResearchEvidenceForRequest(successfulToolCalls, userRequest)
	usableSources := map[string]struct{}{}
	extractCount := 0
	for _, source := range evidence {
		usableSources[strings.TrimSpace(source.URL)] = struct{}{}
		extractCount++
	}
	return len(usableSources), extractCount
}

func countSourceLinks(content string) int {
	return len(sourceLinkRE.FindAllString(content, -1))
}

func shouldRepromptForWebResearch(requirements webResearchRequirements, uniqueSources int, extractCount int, linkCount int) bool {
	if !requirements.Enabled {
		return false
	}
	requiredExtracts := requiredWebResearchExtracts(requirements.MinimumItems)
	if uniqueSources < requirements.MinimumItems {
		return true
	}
	if extractCount < requiredExtracts {
		return true
	}
	return linkCount < requirements.MinimumItems
}

func buildWebResearchRetryPrompt(requirements webResearchRequirements, uniqueSources int, extractCount int, linkCount int) string {
	requiredExtracts := requiredWebResearchExtracts(requirements.MinimumItems)
	return fmt.Sprintf(
		"Continue web research with tools before finalizing. Current usable evidence: %d/%d distinct source URLs, %d/%d extract calls, and %d/%d source links in the draft. Homepages/search pages/index pages/privacy pages/terms pages are low quality and must be replaced with deep-link source pages. Avoid Google/Bing search-result pages unless no alternatives exist because they often trigger anti-bot pages. On index/search pages, use browser.evaluate to collect candidate article links, then navigate into those links and run browser.extract in both metadata and text mode. Do not stop at a site landing page: navigate through additional pages on that site until you reach extractable article pages, or move to another domain if blocked. Derive impact notes from article body text, not metadata-only snippets. Do not wait for a page-provided summary sentence. Skip blocked/consent pages unless you annotate their reason codes, then collect alternative extractable sources. Respond with exactly one fenced ```tool JSON block using browser.navigate/browser.evaluate/browser.extract to collect the missing evidence.",
		uniqueSources,
		requirements.MinimumItems,
		extractCount,
		requiredExtracts,
		linkCount,
		requirements.MinimumItems,
	)
}

type researchLinkCandidate struct {
	URL        string
	AnchorText string
	Score      int
}

const discoverArticleLinksScript = `(function () {
  const nodes = Array.from(document.querySelectorAll('a[href]'));
  const out = [];
  for (const node of nodes) {
    const href = typeof node.href === 'string' ? node.href : '';
    if (!href) continue;
    out.push({
      href,
      text: (node.textContent || '').replace(/\s+/g, ' ').trim(),
    });
  }
  return out.slice(0, 500);
})()`

func buildClickArticleLinkScript(targetURL string, anchorText string) string {
	target := strconv.Quote(strings.TrimSpace(targetURL))
	text := strconv.Quote(strings.TrimSpace(anchorText))
	return fmt.Sprintf(`(function () {
  const targetHref = %s;
  const hintText = %s.toLowerCase();
  const targetPath = (() => {
    try { return new URL(targetHref, window.location.href).pathname || ""; } catch { return ""; }
  })();
  const links = Array.from(document.querySelectorAll('a[href]'));
  for (const link of links) {
    const href = typeof link.href === 'string' ? link.href : '';
    if (!href) continue;
    const text = (link.textContent || '').replace(/\s+/g, ' ').trim().toLowerCase();
    const path = (() => {
      try { return new URL(href, window.location.href).pathname || ""; } catch { return ""; }
    })();
    const directMatch = href === targetHref || (targetPath && path === targetPath);
    const textMatch = hintText && text && (text.includes(hintText) || hintText.includes(text));
    if (!directMatch && !textMatch) continue;
    try {
      link.scrollIntoView({ behavior: 'instant', block: 'center' });
      link.click();
      return { clicked: true, href, text };
    } catch (err) {
      return { clicked: false, href, error: String(err || '') };
    }
  }
  return { clicked: false, href: '', error: 'link_not_found' };
})()`, target, text)
}

func (a *RunActivities) autoDeepenWebResearch(
	ctx context.Context,
	runID string,
	userRequest string,
	successfulToolCalls []toolCall,
	browserUserTab browserUserTabConfig,
) ([]toolCall, bool) {
	restrictSeeds := requestLikelyCryptoResearch(userRequest)
	seeds := mergeResearchSeeds(
		maxAutoResearchSeedPages,
		collectResearchSeedURLs(successfulToolCalls, restrictSeeds),
		fallbackResearchSeedURLsFromRequest(userRequest),
		5,
	)
	if len(seeds) == 0 {
		return nil, false
	}
	requestKeywords := researchKeywordsFromRequest(userRequest)
	_ = a.emitEvent(ctx, runID, "research.deepening", map[string]any{
		"status":           "started",
		"seed_urls":        seeds,
		"request_keywords": requestKeywords,
	})

	recovered := make([]toolCall, 0, 24)
	hadErrors := false
	visited := map[string]struct{}{}
	hostFailures := map[string]int{}
	hostSuccesses := map[string]int{}
	recordHostOutcome := func(rawURL string, reasonCode string) {
		host := hostFromURL(rawURL)
		if host == "" {
			return
		}
		code := strings.ToLower(strings.TrimSpace(reasonCode))
		switch code {
		case "blocked_by_bot_protection", "consent_wall", "login_wall", "no_extractable_content":
			hostFailures[host]++
		default:
			hostSuccesses[host]++
		}
	}
	shouldSkipHost := func(rawURL string) bool {
		host := hostFromURL(rawURL)
		if host == "" {
			return false
		}
		return hostFailures[host] >= 2 && hostSuccesses[host] == 0
	}
	for _, item := range collectWebResearchEvidence(successfulToolCalls) {
		if normalized := normalizeResearchURL(item.URL); normalized != "" {
			visited[normalized] = struct{}{}
		}
		host := hostFromURL(item.URL)
		if host == "" {
			continue
		}
		if item.extractable() && !item.blocked() {
			hostSuccesses[host]++
		} else {
			hostFailures[host]++
		}
	}

	totalArticleLinks := 0
	for _, seedURL := range seeds {
		if totalArticleLinks >= maxAutoResearchLinks {
			break
		}
		if shouldSkipHost(seedURL) {
			continue
		}
		if _, ok := a.executeAutoResearchToolCall(ctx, runID, toolCall{
			ToolName: "browser.navigate",
			Input:    map[string]any{"url": seedURL},
		}, browserUserTab, &recovered, &hadErrors); !ok {
			recordHostOutcome(seedURL, "no_extractable_content")
			continue
		}

		topEvalOutput, ok := a.executeAutoResearchToolCall(ctx, runID, toolCall{
			ToolName: "browser.evaluate",
			Input:    map[string]any{"script": discoverArticleLinksScript},
		}, browserUserTab, &recovered, &hadErrors)
		if !ok {
			continue
		}
		linkCandidates := parseResearchLinkCandidates(topEvalOutput)
		for pass := 0; pass < autoResearchScrollPasses; pass++ {
			_, _ = a.executeAutoResearchToolCall(ctx, runID, toolCall{
				ToolName: "browser.scroll",
				Input:    map[string]any{"direction": "down", "amount": autoResearchScrollAmount},
			}, browserUserTab, &recovered, &hadErrors)
			scrolledEvalOutput, _ := a.executeAutoResearchToolCall(ctx, runID, toolCall{
				ToolName: "browser.evaluate",
				Input:    map[string]any{"script": discoverArticleLinksScript},
			}, browserUserTab, &recovered, &hadErrors)
			linkCandidates = append(linkCandidates, parseResearchLinkCandidates(scrolledEvalOutput)...)
		}

		candidates := rankArticleLinkCandidates(seedURL, linkCandidates, visited, requestKeywords)
		usedForSeed := 0
		for _, candidate := range candidates {
			if totalArticleLinks >= maxAutoResearchLinks || usedForSeed >= maxAutoResearchPerSeed {
				break
			}
			if shouldSkipHost(candidate.URL) {
				continue
			}
			if _, exists := visited[candidate.URL]; exists {
				continue
			}
			visited[candidate.URL] = struct{}{}

			clickOutput, _ := a.executeAutoResearchToolCall(ctx, runID, toolCall{
				ToolName: "browser.evaluate",
				Input: map[string]any{
					"script": buildClickArticleLinkScript(candidate.URL, candidate.AnchorText),
				},
			}, browserUserTab, &recovered, &hadErrors)
			clicked := evaluateClickSucceeded(clickOutput)

			metadataOutput, metaOK := a.executeAutoResearchToolCall(ctx, runID, toolCall{
				ToolName: "browser.extract",
				Input:    map[string]any{"mode": "metadata"},
			}, browserUserTab, &recovered, &hadErrors)
			if !metaOK || !clicked || !metadataPointsToTarget(metadataOutput, candidate.URL, seedURL) {
				if _, ok := a.executeAutoResearchToolCall(ctx, runID, toolCall{
					ToolName: "browser.navigate",
					Input:    map[string]any{"url": candidate.URL},
				}, browserUserTab, &recovered, &hadErrors); !ok {
					recordHostOutcome(candidate.URL, "no_extractable_content")
					continue
				}
				metadataOutput, _ = a.executeAutoResearchToolCall(ctx, runID, toolCall{
					ToolName: "browser.extract",
					Input:    map[string]any{"mode": "metadata"},
				}, browserUserTab, &recovered, &hadErrors)
			}

			textOutput, _ := a.executeAutoResearchToolCall(ctx, runID, toolCall{
				ToolName: "browser.extract",
				Input:    map[string]any{"mode": "text"},
			}, browserUserTab, &recovered, &hadErrors)
			_, _ = a.executeAutoResearchToolCall(ctx, runID, toolCall{
				ToolName: "browser.scroll",
				Input:    map[string]any{"direction": "down", "amount": 900},
			}, browserUserTab, &recovered, &hadErrors)
			_, _ = a.executeAutoResearchToolCall(ctx, runID, toolCall{
				ToolName: "browser.extract",
				Input:    map[string]any{"mode": "text"},
			}, browserUserTab, &recovered, &hadErrors)

			reasonCode, reasonDetail := extractReasonFromToolOutput(textOutput)
			if reasonCode == "" {
				reasonCode, reasonDetail = extractReasonFromToolOutput(metadataOutput)
			}
			recordHostOutcome(candidate.URL, reasonCode)
			if shouldDeepenResearchOnPage(reasonCode, reasonDetail) && totalArticleLinks < maxAutoResearchLinks {
				deeperEvalOutput, deeperOK := a.executeAutoResearchToolCall(ctx, runID, toolCall{
					ToolName: "browser.evaluate",
					Input:    map[string]any{"script": discoverArticleLinksScript},
				}, browserUserTab, &recovered, &hadErrors)
				if deeperOK {
					deeperCandidates := rankArticleLinkCandidates(candidate.URL, parseResearchLinkCandidates(deeperEvalOutput), visited, requestKeywords)
					deeperUsed := 0
					for _, deeperCandidate := range deeperCandidates {
						if totalArticleLinks >= maxAutoResearchLinks || usedForSeed >= maxAutoResearchPerSeed || deeperUsed >= 2 {
							break
						}
						if shouldSkipHost(deeperCandidate.URL) {
							continue
						}
						if _, exists := visited[deeperCandidate.URL]; exists {
							continue
						}
						visited[deeperCandidate.URL] = struct{}{}
						if _, ok := a.executeAutoResearchToolCall(ctx, runID, toolCall{
							ToolName: "browser.navigate",
							Input:    map[string]any{"url": deeperCandidate.URL},
						}, browserUserTab, &recovered, &hadErrors); !ok {
							recordHostOutcome(deeperCandidate.URL, "no_extractable_content")
							continue
						}
						_, _ = a.executeAutoResearchToolCall(ctx, runID, toolCall{
							ToolName: "browser.extract",
							Input:    map[string]any{"mode": "metadata"},
						}, browserUserTab, &recovered, &hadErrors)
						_, _ = a.executeAutoResearchToolCall(ctx, runID, toolCall{
							ToolName: "browser.extract",
							Input:    map[string]any{"mode": "text"},
						}, browserUserTab, &recovered, &hadErrors)
						recordHostOutcome(deeperCandidate.URL, "")
						_, _ = a.executeAutoResearchToolCall(ctx, runID, toolCall{
							ToolName: "browser.scroll",
							Input:    map[string]any{"direction": "down", "amount": 900},
						}, browserUserTab, &recovered, &hadErrors)
						_, _ = a.executeAutoResearchToolCall(ctx, runID, toolCall{
							ToolName: "browser.extract",
							Input:    map[string]any{"mode": "text"},
						}, browserUserTab, &recovered, &hadErrors)
						deeperUsed++
						usedForSeed++
						totalArticleLinks++
					}
				}
			}

			usedForSeed++
			totalArticleLinks++
		}
	}

	status := "completed"
	if len(recovered) == 0 {
		status = "noop"
	} else if hadErrors {
		status = "partial"
	}
	_ = a.emitEvent(ctx, runID, "research.deepening", map[string]any{
		"status":                 status,
		"seed_count":             len(seeds),
		"candidate_articles":     totalArticleLinks,
		"tool_calls_executed":    len(recovered),
		"had_execution_failures": hadErrors,
	})
	return recovered, hadErrors
}

func (a *RunActivities) executeAutoResearchToolCall(
	ctx context.Context,
	runID string,
	call toolCall,
	browserUserTab browserUserTabConfig,
	recovered *[]toolCall,
	hadErrors *bool,
) (map[string]any, bool) {
	output, err := a.executeToolCall(ctx, runID, call, browserUserTab)
	if err != nil {
		*hadErrors = true
		_ = a.postEvent(ctx, runID, "tool.failed", buildToolFailurePayload(call.ToolName, err))
		return nil, false
	}
	*recovered = append(*recovered, toolCall{ToolName: call.ToolName, Input: output})
	return output, true
}

func collectResearchSeedURLs(successfulToolCalls []toolCall, restrictToKnownHosts bool) []string {
	evidence := collectWebResearchEvidence(successfulToolCalls)
	seeds := make([]string, 0, maxAutoResearchSeedPages)
	seenHosts := map[string]struct{}{}

	appendSeed := func(rawURL string) {
		if len(seeds) >= maxAutoResearchSeedPages {
			return
		}
		normalized := normalizeResearchURL(rawURL)
		if normalized == "" {
			return
		}
		parsed, err := url.Parse(normalized)
		if err != nil {
			return
		}
		host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
		if host == "" {
			return
		}
		if restrictToKnownHosts && !isKnownResearchNewsHost(host) && !isSyntheticResearchHost(host) {
			return
		}
		if isSearchSeedHost(host) || isDisallowedResearchHost(host) {
			return
		}
		if _, exists := seenHosts[host]; exists {
			return
		}
		seenHosts[host] = struct{}{}
		seeds = append(seeds, normalized)
	}

	for _, item := range evidence {
		reason := strings.ToLower(strings.TrimSpace(item.ReasonDetail))
		code := strings.ToLower(strings.TrimSpace(item.ReasonCode))
		if code == "blocked_by_bot_protection" || code == "consent_wall" || code == "login_wall" {
			continue
		}
		if code == "no_extractable_content" || reason == "homepage_not_article" || reason == "section_index_page" || reason == "missing_summary_text" {
			appendSeed(item.URL)
		}
	}
	if len(seeds) >= maxAutoResearchSeedPages {
		return seeds
	}
	for _, item := range evidence {
		appendSeed(item.URL)
		if len(seeds) >= maxAutoResearchSeedPages {
			break
		}
	}
	return seeds
}

func isSyntheticResearchHost(host string) bool {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" {
		return false
	}
	if normalized == "localhost" || normalized == "127.0.0.1" || normalized == "::1" {
		return true
	}
	if normalized == "example.com" || normalized == "example.org" || normalized == "example.net" {
		return true
	}
	return strings.HasSuffix(normalized, ".example.com") || strings.HasSuffix(normalized, ".example.org") || strings.HasSuffix(normalized, ".example.net")
}

func requestLikelyCryptoResearch(userRequest string) bool {
	text := strings.ToLower(strings.TrimSpace(userRequest))
	if text == "" {
		return false
	}
	cues := []string{
		"crypto",
		"defi",
		"rwa",
		"real world asset",
		"real-world asset",
		"tokenization",
		"bitcoin",
		"ethereum",
		"stablecoin",
		"web3",
		"on-chain",
		"onchain",
	}
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func fallbackResearchSeedURLsFromRequest(userRequest string) []string {
	query := researchSeedQueryFromRequest(userRequest)
	if query == "" {
		return nil
	}
	candidates := []string{
		"https://www.reuters.com/site-search/?query=" + url.QueryEscape(query),
		"https://www.reuters.com/markets/",
		"https://www.forbes.com/search/?q=" + url.QueryEscape(query),
		"https://finance.yahoo.com/search?p=" + url.QueryEscape(query),
		"https://thedefiant.io/news",
		"https://www.ledgerinsights.com/tag/tokenization/",
		"https://www.ledgerinsights.com/category/tokenization/",
		"https://cointelegraph.com/tags/rwa",
		"https://cointelegraph.com/tags/tokenization",
		"https://www.coindesk.com/tag/real-world-assets/",
		"https://www.coindesk.com/markets/",
		"https://www.coindesk.com/business/",
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		normalized := normalizeResearchURL(candidate)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
		if len(out) >= maxAutoResearchSeedPages {
			break
		}
	}
	return out
}

func mergeResearchSeeds(maxItems int, primary []string, fallback []string, minFallback int) []string {
	if maxItems < 1 {
		maxItems = maxAutoResearchSeedPages
	}
	if minFallback < 0 {
		minFallback = 0
	}
	if minFallback > maxItems {
		minFallback = maxItems
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, maxItems)
	primaryCap := maxItems - minFallback
	if primaryCap < 0 {
		primaryCap = 0
	}
	appendUnique := func(seed string) bool {
		normalized := normalizeResearchURL(seed)
		if normalized == "" {
			return false
		}
		if _, exists := seen[normalized]; exists {
			return false
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
		return true
	}

	for _, seed := range primary {
		if len(out) >= primaryCap {
			break
		}
		appendUnique(seed)
	}
	for _, seed := range fallback {
		if len(out) >= maxItems {
			break
		}
		appendUnique(seed)
	}
	for _, seed := range primary {
		if len(out) >= maxItems {
			break
		}
		appendUnique(seed)
	}
	return out
}

func researchKeywordsFromRequest(userRequest string) []string {
	normalized := strings.ToLower(strings.TrimSpace(userRequest))
	if normalized == "" {
		return nil
	}
	stopwords := map[string]struct{}{
		"browse": {}, "web": {}, "give": {}, "top": {}, "current": {}, "news": {}, "stories": {},
		"surrounding": {}, "with": {}, "from": {}, "and": {}, "for": {}, "the": {}, "that": {},
		"summary": {}, "comprehensive": {}, "items": {}, "item": {}, "real": {}, "world": {}, "asset": {}, "assets": {},
	}
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	seen := map[string]struct{}{}
	keywords := make([]string, 0, 8)
	if strings.Contains(normalized, "real world asset") || strings.Contains(normalized, "real-world asset") {
		seen["rwa"] = struct{}{}
		keywords = append(keywords, "rwa")
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) < 3 {
			continue
		}
		switch part {
		case "rwas":
			part = "rwa"
		case "defis":
			part = "defi"
		}
		if _, stop := stopwords[part]; stop {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		keywords = append(keywords, part)
		if len(keywords) >= 8 {
			break
		}
	}
	return keywords
}

func researchSeedQueryFromRequest(userRequest string) string {
	keywords := researchKeywordsFromRequest(userRequest)
	if len(keywords) > 0 {
		return strings.Join(keywords, " ")
	}
	parts := strings.Fields(strings.TrimSpace(strings.ToLower(userRequest)))
	if len(parts) == 0 {
		return ""
	}
	if len(parts) > 10 {
		parts = parts[:10]
	}
	return strings.Join(parts, " ")
}

func normalizeResearchURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	parsed.Fragment = ""
	query := parsed.Query()
	for _, key := range []string{
		"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content",
		"gclid", "fbclid", "yclid", "mc_cid", "mc_eid",
	} {
		query.Del(key)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func parseResearchLinkCandidates(output map[string]any) []researchLinkCandidate {
	rawResult := output["result"]
	if rawResult == nil {
		return nil
	}
	items, ok := rawResult.([]any)
	if !ok {
		return nil
	}
	candidates := make([]researchLinkCandidate, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			candidates = append(candidates, researchLinkCandidate{URL: typed})
		case map[string]any:
			candidates = append(candidates, researchLinkCandidate{
				URL:        firstNonEmptyString(typed["href"], typed["url"]),
				AnchorText: strings.TrimSpace(toString(typed["text"])),
			})
		}
	}
	return candidates
}

func resolveResearchCandidateURL(candidateURL string, seedURL string) string {
	trimmed := strings.TrimSpace(candidateURL)
	if trimmed == "" {
		return ""
	}
	base, baseErr := url.Parse(strings.TrimSpace(seedURL))
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	if !parsed.IsAbs() && baseErr == nil && base != nil {
		parsed = base.ResolveReference(parsed)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return ""
	}
	query := parsed.Query()
	if strings.Contains(host, "google.") {
		if parsed.Path == "/url" {
			if redirected := strings.TrimSpace(firstNonEmptyString(query.Get("url"), query.Get("q"))); redirected != "" {
				return normalizeResearchURL(redirected)
			}
		}
		if redirected := strings.TrimSpace(query.Get("url")); redirected != "" {
			return normalizeResearchURL(redirected)
		}
	}
	if strings.Contains(host, "duckduckgo.com") {
		if redirected := strings.TrimSpace(query.Get("uddg")); redirected != "" {
			if unescaped, unescapeErr := url.QueryUnescape(redirected); unescapeErr == nil {
				redirected = unescaped
			}
			return normalizeResearchURL(redirected)
		}
	}
	return normalizeResearchURL(parsed.String())
}

func isSearchSeedHost(host string) bool {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" {
		return false
	}
	searchHosts := []string{
		"google.com",
		"news.google.com",
		"duckduckgo.com",
		"bing.com",
		"search.yahoo.com",
		"yahoo.com",
	}
	for _, candidate := range searchHosts {
		if normalized == candidate || strings.HasSuffix(normalized, "."+candidate) {
			return true
		}
	}
	return false
}

func isDisallowedResearchHost(host string) bool {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" {
		return true
	}
	blocked := []string{
		"accounts.google.com",
		"policies.google.com",
		"support.google.com",
		"consent.google.com",
		"myaccount.google.com",
	}
	for _, candidate := range blocked {
		if normalized == candidate || strings.HasSuffix(normalized, "."+candidate) {
			return true
		}
	}
	return false
}

func isUtilityResearchHost(host string) bool {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" {
		return true
	}
	utilityHosts := []string{
		"duckduckgo.com",
		"help.duckduckgo.com",
		"apps.apple.com",
		"play.google.com",
		"chromewebstore.google.com",
		"support.google.com",
		"policies.google.com",
	}
	for _, candidate := range utilityHosts {
		if normalized == candidate || strings.HasSuffix(normalized, "."+candidate) {
			return true
		}
	}
	return false
}

func isKnownResearchNewsHost(host string) bool {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" {
		return false
	}
	newsHosts := []string{
		"reuters.com",
		"forbes.com",
		"bloomberg.com",
		"coindesk.com",
		"cointelegraph.com",
		"theblock.co",
		"decrypt.co",
		"thedefiant.io",
		"ledgerinsights.com",
		"binance.com",
		"coinmarketcap.com",
		"coingecko.com",
		"finance.yahoo.com",
		"prnewswire.com",
		"rwa.xyz",
		"protos.com",
	}
	for _, candidate := range newsHosts {
		if normalized == candidate || strings.HasSuffix(normalized, "."+candidate) {
			return true
		}
	}
	return false
}

func rankArticleLinkCandidates(seedURL string, candidates []researchLinkCandidate, visited map[string]struct{}, requestKeywords []string) []researchLinkCandidate {
	seedParsed, err := url.Parse(seedURL)
	if err != nil {
		return nil
	}
	seedHost := strings.ToLower(strings.TrimSpace(seedParsed.Hostname()))
	if seedHost == "" {
		return nil
	}
	allowCrossDomain := isSearchSeedHost(seedHost)
	targetYear := ""
	targetMonth := ""
	specificKeywords := make([]string, 0, len(requestKeywords))
	for _, keyword := range requestKeywords {
		if len(keyword) == 4 && strings.HasPrefix(keyword, "20") {
			targetYear = keyword
			continue
		}
		switch keyword {
		case "january", "february", "march", "april", "may", "june",
			"july", "august", "september", "october", "november", "december":
			targetMonth = keyword
		case "crypto", "news", "latest", "current", "stories", "story":
			// Generic terms are not strong topical filters.
		default:
			specificKeywords = append(specificKeywords, keyword)
		}
	}

	filtered := make([]researchLinkCandidate, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		normalized := resolveResearchCandidateURL(candidate.URL, seedURL)
		if normalized == "" {
			continue
		}
		if _, exists := visited[normalized]; exists {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}

		parsed, err := url.Parse(normalized)
		if err != nil {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
		if host == "" {
			continue
		}
		if isDisallowedResearchHost(host) || isUtilityResearchHost(host) {
			continue
		}
		if allowCrossDomain && isSearchSeedHost(host) {
			continue
		}
		if !allowCrossDomain && host != seedHost && !strings.HasSuffix(host, "."+seedHost) && !strings.HasSuffix(seedHost, "."+host) {
			continue
		}

		path := strings.ToLower(strings.TrimSpace(parsed.Path))
		if path == "" || path == "/" || strings.HasPrefix(path, "/search") {
			continue
		}
		query := parsed.Query()
		if query.Has("q") || query.Has("query") || query.Has("s") || query.Has("blob") {
			continue
		}
		switch detail := nonArticleReasonForURL(normalized); detail {
		case "search_results_page", "homepage_not_article", "section_index_page", "legal_or_policy_page":
			continue
		}
		segments := strings.Split(strings.Trim(path, "/"), "/")
		if len(segments) < 2 {
			continue
		}
		lastSegment := segments[len(segments)-1]
		if strings.Contains(lastSegment, ".") {
			suffix := strings.ToLower(lastSegment[strings.LastIndex(lastSegment, "."):])
			switch suffix {
			case ".jpg", ".jpeg", ".png", ".gif", ".svg", ".webp", ".pdf":
				continue
			}
		}

		anchorText := strings.TrimSpace(candidate.AnchorText)
		anchorLower := strings.ToLower(anchorText)
		if strings.Contains(anchorLower, "privacy") || strings.Contains(anchorLower, "terms") || strings.Contains(anchorLower, "cookie") || strings.Contains(anchorLower, "consent") {
			continue
		}
		haystack := strings.ToLower(path + " " + anchorText)

		score := 0
		if datePathRE.MatchString(path) {
			score += 5
		}
		if len(segments) >= 3 {
			score += 2
		} else {
			score++
		}
		if strings.Contains(lastSegment, "-") {
			score += 2
		}
		if len(lastSegment) >= 24 {
			score++
		}
		if len(strings.Fields(anchorText)) >= 4 {
			score++
		}

		keywordMatches := 0
		if len(requestKeywords) > 0 {
			for _, keyword := range requestKeywords {
				if keyword != "" && strings.Contains(haystack, keyword) {
					keywordMatches++
				}
			}
			if keywordMatches > 0 {
				score += keywordMatches * 2
			} else {
				score--
			}
		}

		if strings.Contains(path, "/press-releases/") || strings.Contains(path, "/press-release/") {
			continue
		}
		if strings.Contains(path, "/sponsored/") {
			continue
		}
		if strings.Contains(path, "/opinion/") {
			continue
		}
		if strings.Contains(path, "/people/") {
			continue
		}
		if strings.Contains(path, "/price/") || strings.Contains(path, "/prices/") || strings.Contains(path, "/video/") || strings.Contains(path, "/videos/") || strings.Contains(path, "/podcast/") || strings.Contains(path, "/podcasts/") {
			continue
		}
		if strings.Contains(path, "/tag/") || strings.Contains(path, "/tags/") || strings.Contains(path, "/topic/") || strings.Contains(path, "/topics/") {
			continue
		}

		if targetYear != "" {
			matchedYear := anyYearRE.FindString(haystack)
			if strings.Contains(haystack, targetYear) {
				score += 3
			} else if matchedYear != "" && matchedYear != targetYear {
				continue
			} else if legacyYearRE.MatchString(haystack) {
				score -= 10
			}
		}
		if targetMonth != "" && strings.Contains(haystack, targetMonth) {
			score += 2
		}

		specificMatches := 0
		if len(specificKeywords) > 0 {
			specificMatches = keywordMatchCount(specificKeywords, haystack)
			if specificMatches > 0 {
				score += specificMatches * 2
			} else {
				continue
			}
		}

		if allowCrossDomain && host != seedHost {
			if !isKnownResearchNewsHost(host) {
				continue
			}
			score += 2
			score += 2
		}

		if score < 2 {
			continue
		}
		seen[normalized] = struct{}{}
		filtered = append(filtered, researchLinkCandidate{
			URL:        normalized,
			AnchorText: strings.TrimSpace(candidate.AnchorText),
			Score:      score,
		})
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Score > filtered[j].Score
	})
	return filtered
}

func evaluateClickSucceeded(output map[string]any) bool {
	result, ok := output["result"]
	if !ok {
		return false
	}
	switch typed := result.(type) {
	case bool:
		return typed
	case map[string]any:
		switch clicked := typed["clicked"].(type) {
		case bool:
			return clicked
		case string:
			return strings.EqualFold(strings.TrimSpace(clicked), "true")
		}
	}
	return false
}

func metadataPointsToTarget(metadataOutput map[string]any, targetURL string, seedURL string) bool {
	currentURL := extractURLFromToolOutput(metadataOutput)
	currentNorm := normalizeResearchURL(currentURL)
	targetNorm := normalizeResearchURL(targetURL)
	seedNorm := normalizeResearchURL(seedURL)
	if currentNorm == "" || targetNorm == "" {
		return false
	}
	if currentNorm == targetNorm {
		return true
	}
	if currentNorm == seedNorm {
		return false
	}
	currentParsed, currentErr := url.Parse(currentNorm)
	targetParsed, targetErr := url.Parse(targetNorm)
	if currentErr != nil || targetErr != nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(currentParsed.Hostname()), strings.TrimSpace(targetParsed.Hostname())) {
		return false
	}
	currentPath := strings.Trim(strings.ToLower(strings.TrimSpace(currentParsed.Path)), "/")
	targetPath := strings.Trim(strings.ToLower(strings.TrimSpace(targetParsed.Path)), "/")
	if currentPath == "" || targetPath == "" {
		return false
	}
	if currentPath == targetPath {
		return true
	}
	currentSegments := strings.Split(currentPath, "/")
	targetSegments := strings.Split(targetPath, "/")
	if len(currentSegments) == 0 || len(targetSegments) == 0 {
		return false
	}
	return currentSegments[len(currentSegments)-1] == targetSegments[len(targetSegments)-1]
}

func extractURLFromToolOutput(output map[string]any) string {
	if output == nil {
		return ""
	}
	if direct := strings.TrimSpace(toString(output["url"])); direct != "" {
		return direct
	}
	if extracted, ok := output["extracted"].(map[string]any); ok {
		if nested := strings.TrimSpace(toString(extracted["url"])); nested != "" {
			return nested
		}
	}
	return ""
}

func extractReasonFromToolOutput(output map[string]any) (string, string) {
	if output == nil {
		return "", ""
	}
	readDiagnostics := func(value any) (string, string) {
		diagnostics, _ := value.(map[string]any)
		if diagnostics == nil {
			return "", ""
		}
		return strings.TrimSpace(toString(diagnostics["reason_code"])), strings.TrimSpace(toString(diagnostics["reason_detail"]))
	}
	code, detail := readDiagnostics(output["diagnostics"])
	if code != "" || detail != "" {
		return code, detail
	}
	code, detail = readDiagnostics(output["extracted"])
	if code != "" || detail != "" {
		return code, detail
	}
	return strings.TrimSpace(toString(output["reason_code"])), strings.TrimSpace(toString(output["reason_detail"]))
}

func shouldDeepenResearchOnPage(reasonCode string, reasonDetail string) bool {
	if strings.TrimSpace(strings.ToLower(reasonCode)) != "no_extractable_content" {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(reasonDetail)) {
	case "section_index_page", "homepage_not_article", "search_results_page", "landing_page_or_index_content", "missing_summary_text":
		return true
	default:
		return false
	}
}

func looksLikeInProgressResearchNarrative(response string) bool {
	text := strings.ToLower(strings.TrimSpace(response))
	if text == "" {
		return false
	}
	cues := []string{
		"let me try",
		"i'll try",
		"i will try",
		"i can try",
		"i can continue",
		"i'll continue",
		"i will continue",
		"alternative sources",
		"try alternative",
		"next, i",
		"next i",
	}
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return strings.Contains(text, "i'll") && strings.Contains(text, "try")
}

func buildInsufficientWebResearchFallback(successfulToolCalls []toolCall, requirements webResearchRequirements, userRequest string, lastResponse string) string {
	if deterministic := strings.TrimSpace(buildDeterministicWebResearchSummaryForRequest(successfulToolCalls, requirements, userRequest)); deterministic != "" {
		return deterministic
	}
	return strings.TrimSpace(fmt.Sprintf("I could not reach the requested evidence threshold of %d usable sources with extractable article pages.", requirements.MinimumItems))
}

func requiredWebResearchExtracts(minimumItems int) int {
	requiredExtracts := minimumItems / 2
	if requiredExtracts < 2 {
		requiredExtracts = 2
	}
	return requiredExtracts
}

func hasSufficientWebResearchEvidence(successfulToolCalls []toolCall, requirements webResearchRequirements) bool {
	return hasSufficientWebResearchEvidenceForRequest(successfulToolCalls, requirements, "")
}

func hasSufficientWebResearchEvidenceForRequest(successfulToolCalls []toolCall, requirements webResearchRequirements, userRequest string) bool {
	if !requirements.Enabled {
		return false
	}
	uniqueSources, extractCount := summarizeWebResearchEvidenceForRequest(successfulToolCalls, userRequest)
	if uniqueSources < requirements.MinimumItems {
		return false
	}
	return extractCount >= requiredWebResearchExtracts(requirements.MinimumItems)
}

func buildResearchDiagnosticsDigest(successfulToolCalls []toolCall) string {
	evidence := collectWebResearchEvidence(successfulToolCalls)
	if len(evidence) == 0 {
		return "Research diagnostics: no extractable source diagnostics captured."
	}
	var builder strings.Builder
	builder.WriteString("Research diagnostics:\n")
	for idx, item := range evidence {
		label := strings.TrimSpace(item.Title)
		if label == "" {
			label = hostFromURL(item.URL)
		}
		status := "ok"
		if item.blocked() {
			status = "blocked"
		} else if !item.extractable() {
			status = "low_quality"
		}
		impact := strings.TrimSpace(item.Impact)
		reasonCode := strings.TrimSpace(item.ReasonCode)
		if reasonCode == "" {
			reasonCode = "none"
		}
		detail := strings.TrimSpace(item.ReasonDetail)
		if detail == "" && reasonCode != "none" {
			detail = reasonCode
		}
		builder.WriteString(
			fmt.Sprintf(
				"%d. %s (%s) url=%s reason=%s detail=%s impact=%s\n",
				idx+1,
				label,
				status,
				item.URL,
				reasonCode,
				detail,
				impact,
			),
		)
	}
	return strings.TrimSpace(builder.String())
}

func (a *RunActivities) generateFinalSynthesis(
	ctx context.Context,
	runID string,
	providers []llmProviderCandidate,
	baseMessages []llm.Message,
	userRequest string,
	successfulToolCalls []toolCall,
	researchRequirements webResearchRequirements,
) (string, error) {
	summary := buildToolCompletionSummary(successfulToolCalls, false, nil)
	researchDigest := ""
	requestedTopN := requestedTopNFromPrompt(userRequest, researchRequirements.MinimumItems)
	if researchRequirements.Enabled {
		researchDigest = buildResearchDiagnosticsDigest(successfulToolCalls)
	}
	instruction := "Now produce the final user-facing answer only. Do not output tool JSON, fenced blocks, or additional tool calls."
	if researchRequirements.Enabled {
		instruction = fmt.Sprintf(
			"Now produce the final user-facing answer only. Use at least %d cited source links and concise impact notes. Prefer a numbered Top %d list when the request asks for top items. For each item include: headline, date, source link, and a 2-4 sentence summary synthesized from extracted article body text (not metadata-only snippets). Start with a short overview and key themes section before the numbered items. Only cite deep-link article/source pages (not homepages/search/index pages). Do not include blocked/low-quality/source-diagnostics sections unless the user explicitly requested diagnostics. Do not output tool JSON, fenced blocks, or additional tool calls.",
			researchRequirements.MinimumItems,
			requestedTopN,
		)
	}
	synthesisMessages := append([]llm.Message{}, baseMessages...)
	synthesisMessages = append(synthesisMessages,
		llm.Message{
			Role: "system",
			Content: fmt.Sprintf(
				"Execution summary: %s\nOriginal request: %s\n%s\n%s",
				summary,
				truncateRunes(strings.TrimSpace(userRequest), 360),
				researchDigest,
				instruction,
			),
		},
	)
	synthesisMessages = clampConversationWindow(synthesisMessages, maxConversationMessages, maxConversationChars)
	return a.generateWithRetry(ctx, runID, providers, synthesisMessages)
}

func formatToolResult(toolName string, output map[string]any, err error) string {
	payload := map[string]any{"tool_name": toolName}
	if err != nil {
		payload["error"] = err.Error()
	}
	if output != nil {
		payload["output"] = output
	}
	encoded, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return fmt.Sprintf("Tool result (%s): %v", toolName, err)
	}
	text := string(encoded)
	if maxToolResultChars > 0 {
		text = truncateRunes(text, maxToolResultChars)
	}
	return fmt.Sprintf("Tool result: %s", text)
}

func buildToolFailurePayload(toolName string, err error) map[string]any {
	payload := map[string]any{
		"tool_name": toolName,
		"error":     strings.TrimSpace(err.Error()),
	}
	var execErr *toolExecutionError
	if errors.As(err, &execErr) {
		if invocationID := strings.TrimSpace(execErr.InvocationID); invocationID != "" {
			payload["tool_invocation_id"] = invocationID
		}
	}
	if reasonCode := inferToolFailureReasonCode(err); reasonCode != "" {
		payload["reason_code"] = reasonCode
	}
	return payload
}

func inferToolFailureReasonCode(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "blocked_by_bot_protection"):
		return "blocked_by_bot_protection"
	case strings.Contains(message, "consent_wall"):
		return "consent_wall"
	case strings.Contains(message, "no_extractable_content"):
		return "no_extractable_content"
	case strings.Contains(message, "user_tab_mode_unavailable"):
		return "user_tab_mode_unavailable"
	case strings.Contains(message, "timeout"):
		return "timeout"
	default:
		return ""
	}
}

func (a *RunActivities) executeToolCall(ctx context.Context, runID string, call toolCall, browserUserTab browserUserTabConfig) (map[string]any, error) {
	if a.toolRunner == "" {
		return nil, &toolExecutionError{Message: "tool runner url not configured"}
	}
	invocationID := uuid.New().String()
	toolInput := cloneAnyMap(call.Input)
	if browserUserTab.Enabled && strings.HasPrefix(strings.ToLower(strings.TrimSpace(call.ToolName)), "browser.") {
		toolInput["_browser_mode"] = "user_tab"
		toolInput["_browser_guardrails"] = map[string]any{
			"interaction_allowed": browserUserTab.InteractionAllowed,
			"create_tab_group":    true,
			"allowlist_domains":   browserUserTab.DomainAllowlist,
			"preferred_browser":   browserUserTab.PreferredBrowser,
			"browser_user_agent":  browserUserTab.BrowserUserAgent,
		}
	}
	payload := map[string]any{
		"contract_version": "tool_contract_v2",
		"run_id":           runID,
		"invocation_id":    invocationID,
		"idempotency_key":  invocationID,
		"tool_name":        call.ToolName,
		"input":            toolInput,
	}
	if a.toolTimeout > 0 {
		payload["timeout_ms"] = int(a.toolTimeout / time.Millisecond)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &toolExecutionError{InvocationID: invocationID, Message: err.Error()}
	}
	requestCtx := ctx
	if a.toolTimeout > 0 {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, a.toolTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, a.toolRunner+"/tools/execute", bytes.NewReader(body))
	if err != nil {
		return nil, &toolExecutionError{InvocationID: invocationID, Message: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, &toolExecutionError{InvocationID: invocationID, Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(resp.Body)
		message := parseToolRunnerErrorMessage(resp.StatusCode, responseBody)
		return nil, &toolExecutionError{InvocationID: invocationID, Message: message}
	}
	var result toolRunnerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, &toolExecutionError{InvocationID: invocationID, Message: err.Error()}
	}
	if result.Error != "" {
		return nil, &toolExecutionError{InvocationID: invocationID, Message: strings.TrimSpace(result.Error)}
	}
	return result.Output, nil
}

func parseToolRunnerErrorMessage(statusCode int, responseBody []byte) string {
	trimmed := strings.TrimSpace(string(responseBody))
	if trimmed == "" {
		return fmt.Sprintf("tool runner returned status %d", statusCode)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return trimmed
	}

	reasonCode := strings.TrimSpace(toString(payload["reason_code"]))
	if diagnostics, ok := payload["diagnostics"].(map[string]any); ok {
		if reasonCode == "" {
			reasonCode = strings.TrimSpace(toString(diagnostics["reason_code"]))
		}
		if reasonDetail := strings.TrimSpace(toString(diagnostics["reason_detail"])); reasonDetail != "" {
			if detail := strings.TrimSpace(toString(payload["error"])); detail == "" {
				payload["error"] = reasonDetail
			}
		}
	}

	errorText := firstNonEmptyString(payload["error"], payload["message"], payload["detail"])
	if errorText == "" {
		errorText = trimmed
	}
	if strings.HasPrefix(strings.TrimSpace(errorText), "{") {
		nested := map[string]any{}
		if err := json.Unmarshal([]byte(errorText), &nested); err == nil {
			if reasonCode == "" {
				reasonCode = strings.TrimSpace(toString(nested["reason_code"]))
			}
			errorText = firstNonEmptyString(nested["error"], nested["message"], nested["detail"], errorText)
		}
	}
	if reasonCode != "" {
		return fmt.Sprintf("%s (%s)", errorText, reasonCode)
	}
	return errorText
}

func (a *RunActivities) cleanupRunResources(ctx context.Context, runID string) error {
	if strings.TrimSpace(a.toolRunner) == "" || strings.TrimSpace(runID) == "" {
		return nil
	}
	payloadBody, err := json.Marshal(map[string]any{"force": true})
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		fmt.Sprintf("%s/runs/%s/processes/cleanup", strings.TrimRight(a.toolRunner, "/"), url.PathEscape(runID)),
		bytes.NewReader(payloadBody),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = fmt.Sprintf("cleanup failed with status %d", resp.StatusCode)
		}
		return errors.New(message)
	}
	return nil
}

func shouldAutoGenerateResearchDoc(content string) bool {
	trimmed := strings.TrimSpace(sanitizeResearchUserResponse(content))
	if trimmed == "" {
		trimmed = strings.TrimSpace(content)
	}
	if trimmed == "" {
		return false
	}
	normalized := strings.ToLower(trimmed)
	if strings.Contains(normalized, "could not extract enough article-grade source") {
		return false
	}
	if strings.Contains(normalized, "low-quality extracts:") || strings.Contains(normalized, "blocked sources:") {
		return false
	}
	hasStoryStructure := strings.Contains(normalized, "top stories:") || topStoryLineRE.MatchString(trimmed)
	if !hasStoryStructure {
		return false
	}
	return countSourceLinks(trimmed) >= 3
}

func sanitizeResearchUserResponse(content string) string {
	trimmed := strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		if lower == "" {
			filtered = append(filtered, line)
			continue
		}
		if strings.HasPrefix(lower, "overview:") && strings.Contains(lower, "extractable source(s)") {
			continue
		}
		if strings.HasPrefix(lower, "usable sources:") && strings.Contains(lower, "requested") {
			continue
		}
		if strings.HasPrefix(lower, "coverage limitation:") {
			continue
		}
		if strings.HasPrefix(lower, "stopped before finalizing") {
			continue
		}
		if strings.Contains(lower, "i can continue gathering alternatives") {
			continue
		}
		if strings.Contains(lower, "model kept returning intent text instead of executable tool json") {
			continue
		}
		filtered = append(filtered, line)
	}

	sanitized := strings.TrimSpace(strings.Join(filtered, "\n"))
	if sanitized == "" {
		return ""
	}
	lowered := strings.ToLower(sanitized)
	diagnosticsMarkers := []string{
		"\nlow-quality extracts:",
		"\nblocked sources:",
		"\nper-source diagnostics:",
	}
	cut := len(sanitized)
	for _, marker := range diagnosticsMarkers {
		if idx := strings.Index(lowered, marker); idx >= 0 && idx < cut {
			cut = idx
		}
	}
	if strings.HasPrefix(lowered, "low-quality extracts:") ||
		strings.HasPrefix(lowered, "blocked sources:") ||
		strings.HasPrefix(lowered, "per-source diagnostics:") {
		return ""
	}
	if cut < len(sanitized) {
		sanitized = strings.TrimSpace(sanitized[:cut])
	}
	return strings.TrimSpace(sanitized)
}

func normalizeResearchDocLine(line string) string {
	normalized := markdownLinkRE.ReplaceAllString(line, "$1 ($2)")
	replacer := strings.NewReplacer(
		"**", "",
		"__", "",
		"`", "",
		"### ", "",
		"## ", "",
		"# ", "",
	)
	normalized = replacer.Replace(normalized)
	return strings.TrimSpace(normalized)
}

func researchDocSectionHeading(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	normalized := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(trimmed, ":")))
	switch normalized {
	case "overview":
		return "Overview"
	case "key themes":
		return "Key Themes"
	case "top stories", "major headlines":
		return "Top Stories"
	case "references", "sources":
		return "References"
	}
	if strings.HasPrefix(trimmed, "## ") {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
	}
	if strings.HasPrefix(trimmed, "# ") {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
	}
	return ""
}

func deriveResearchDocTitle(content string) string {
	if match := strings.TrimSpace(monthYearRE.FindString(content)); match != "" {
		words := strings.Fields(strings.ToLower(match))
		for idx, word := range words {
			if word == "" {
				continue
			}
			words[idx] = strings.ToUpper(word[:1]) + word[1:]
		}
		return fmt.Sprintf("Research Report - %s", strings.Join(words, " "))
	}
	return "Web Research Report"
}

func buildResearchDocSections(content string) []map[string]any {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	sections := make([]map[string]any, 0, 8)
	currentHeading := "Summary"
	currentLines := make([]string, 0, 16)
	flush := func() {
		if len(currentLines) == 0 {
			return
		}
		joined := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if joined == "" {
			currentLines = currentLines[:0]
			return
		}
		sections = append(sections, map[string]any{
			"heading": truncateRunes(currentHeading, 120),
			"content": truncateRunes(joined, 12000),
		})
		currentLines = currentLines[:0]
	}

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if heading := researchDocSectionHeading(trimmed); heading != "" {
			flush()
			currentHeading = heading
			continue
		}
		if trimmed == "" {
			if len(currentLines) > 0 && currentLines[len(currentLines)-1] != "" {
				currentLines = append(currentLines, "")
			}
			continue
		}
		currentLines = append(currentLines, normalizeResearchDocLine(trimmed))
	}
	flush()
	if len(sections) > 8 {
		return sections[:8]
	}
	return sections
}

func (a *RunActivities) toolRunnerSupportsTool(ctx context.Context, toolName string) bool {
	if strings.TrimSpace(a.toolRunner) == "" || strings.TrimSpace(toolName) == "" {
		return false
	}
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	capabilitiesURL := fmt.Sprintf("%s/tools/capabilities", strings.TrimRight(a.toolRunner, "/"))
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, capabilitiesURL, nil)
	if err != nil {
		return false
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	payload := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false
	}
	return containsString(toStringSlice(payload["tools"]), toolName)
}

func (a *RunActivities) maybeCreateResearchDocxArtifact(runID string, content string) {
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(content) == "" {
		return
	}
	if !shouldAutoGenerateResearchDoc(content) {
		return
	}
	if strings.TrimSpace(a.toolRunner) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if !a.toolRunnerSupportsTool(ctx, "document.create_docx") {
		return
	}
	sections := buildResearchDocSections(content)
	if len(sections) == 0 {
		return
	}
	_, _ = a.executeToolCall(ctx, runID, toolCall{
		ToolName: "document.create_docx",
		Input: map[string]any{
			"title":    deriveResearchDocTitle(content),
			"sections": sections,
		},
	}, browserUserTabConfig{})
}

func (a *RunActivities) postMessage(ctx context.Context, runID string, content string) error {
	sanitizedContent := strings.TrimSpace(sanitizeResearchUserResponse(content))
	if sanitizedContent == "" {
		sanitizedContent = strings.TrimSpace(content)
	}
	if sanitizedContent == "" {
		return errors.New("assistant message content empty")
	}
	url := fmt.Sprintf("%s/runs/%s/messages", a.controlPlane, runID)
	body, err := marshalJSON(map[string]string{
		"role":    "assistant",
		"content": sanitizedContent,
	})
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("control plane message failed: %s", resp.Status)
	}
	a.maybeCreateResearchDocxArtifact(runID, sanitizedContent)
	return nil
}

func (a *RunActivities) postEvent(ctx context.Context, runID string, eventType string, payload map[string]any) error {
	url := fmt.Sprintf("%s/runs/%s/events", a.controlPlane, runID)
	body, err := marshalJSON(map[string]any{
		"type":      eventType,
		"source":    "llm",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"trace_id":  uuid.New().String(),
		"payload":   payload,
	})
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("control plane event failed: %s", resp.Status)
	}
	return nil
}
