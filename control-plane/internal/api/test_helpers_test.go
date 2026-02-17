package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/config"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/events"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/llm"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

type MockStore struct {
	mock.Mock
}

func (m *MockStore) ListRuns(ctx context.Context) ([]store.RunSummary, error) {
	args := m.Called(ctx)
	var result []store.RunSummary
	if value := args.Get(0); value != nil {
		result = value.([]store.RunSummary)
	}
	return result, args.Error(1)
}

func (m *MockStore) DeleteRun(ctx context.Context, runID string) error {
	args := m.Called(ctx, runID)
	return args.Error(0)
}

func (m *MockStore) CreateRun(ctx context.Context, run store.Run) error {
	args := m.Called(ctx, run)
	return args.Error(0)
}

func (m *MockStore) AddMessage(ctx context.Context, msg store.Message) error {
	args := m.Called(ctx, msg)
	return args.Error(0)
}

func (m *MockStore) ListMessages(ctx context.Context, runID string) ([]store.Message, error) {
	args := m.Called(ctx, runID)
	var result []store.Message
	if value := args.Get(0); value != nil {
		result = value.([]store.Message)
	}
	return result, args.Error(1)
}

func (m *MockStore) GetLLMSettings(ctx context.Context) (*store.LLMSettings, error) {
	args := m.Called(ctx)
	if value := args.Get(0); value != nil {
		return value.(*store.LLMSettings), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockStore) UpsertLLMSettings(ctx context.Context, settings store.LLMSettings) error {
	args := m.Called(ctx, settings)
	return args.Error(0)
}

func (m *MockStore) ListSkills(ctx context.Context) ([]store.Skill, error) {
	args := m.Called(ctx)
	var result []store.Skill
	if value := args.Get(0); value != nil {
		result = value.([]store.Skill)
	}
	return result, args.Error(1)
}

func (m *MockStore) GetSkill(ctx context.Context, skillID string) (*store.Skill, error) {
	args := m.Called(ctx, skillID)
	if value := args.Get(0); value != nil {
		return value.(*store.Skill), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockStore) CreateSkill(ctx context.Context, skill store.Skill) error {
	args := m.Called(ctx, skill)
	return args.Error(0)
}

func (m *MockStore) UpdateSkill(ctx context.Context, skill store.Skill) error {
	args := m.Called(ctx, skill)
	return args.Error(0)
}

func (m *MockStore) DeleteSkill(ctx context.Context, skillID string) error {
	args := m.Called(ctx, skillID)
	return args.Error(0)
}

func (m *MockStore) ListSkillFiles(ctx context.Context, skillID string) ([]store.SkillFile, error) {
	args := m.Called(ctx, skillID)
	var result []store.SkillFile
	if value := args.Get(0); value != nil {
		result = value.([]store.SkillFile)
	}
	return result, args.Error(1)
}

func (m *MockStore) UpsertSkillFile(ctx context.Context, file store.SkillFile) error {
	args := m.Called(ctx, file)
	return args.Error(0)
}

func (m *MockStore) DeleteSkillFile(ctx context.Context, skillID string, path string) error {
	args := m.Called(ctx, skillID, path)
	return args.Error(0)
}

func (m *MockStore) ListContextNodes(ctx context.Context) ([]store.ContextNode, error) {
	args := m.Called(ctx)
	var result []store.ContextNode
	if value := args.Get(0); value != nil {
		result = value.([]store.ContextNode)
	}
	return result, args.Error(1)
}

func (m *MockStore) GetContextFile(ctx context.Context, nodeID string) (*store.ContextNode, error) {
	args := m.Called(ctx, nodeID)
	if value := args.Get(0); value != nil {
		return value.(*store.ContextNode), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockStore) CreateContextFolder(ctx context.Context, node store.ContextNode) error {
	args := m.Called(ctx, node)
	return args.Error(0)
}

func (m *MockStore) CreateContextFile(ctx context.Context, node store.ContextNode) error {
	args := m.Called(ctx, node)
	return args.Error(0)
}

func (m *MockStore) DeleteContextNode(ctx context.Context, nodeID string) error {
	args := m.Called(ctx, nodeID)
	return args.Error(0)
}

func (m *MockStore) GetMemorySettings(ctx context.Context) (*store.MemorySettings, error) {
	args := m.Called(ctx)
	if value := args.Get(0); value != nil {
		return value.(*store.MemorySettings), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockStore) UpsertMemorySettings(ctx context.Context, settings store.MemorySettings) error {
	args := m.Called(ctx, settings)
	return args.Error(0)
}

func (m *MockStore) UpsertMemoryEntry(ctx context.Context, entry store.MemoryEntry) (bool, error) {
	args := m.Called(ctx, entry)
	return args.Bool(0), args.Error(1)
}

func (m *MockStore) GetPersonalitySettings(ctx context.Context) (*store.PersonalitySettings, error) {
	args := m.Called(ctx)
	if value := args.Get(0); value != nil {
		return value.(*store.PersonalitySettings), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockStore) UpsertPersonalitySettings(ctx context.Context, settings store.PersonalitySettings) error {
	args := m.Called(ctx, settings)
	return args.Error(0)
}

func (m *MockStore) SearchMemory(ctx context.Context, query string, limit int) ([]store.MemoryEntry, error) {
	args := m.Called(ctx, query, limit)
	var result []store.MemoryEntry
	if value := args.Get(0); value != nil {
		result = value.([]store.MemoryEntry)
	}
	return result, args.Error(1)
}

func (m *MockStore) SearchMemoryWithEmbedding(ctx context.Context, query string, embedding []float32, limit int) ([]store.MemoryEntry, error) {
	args := m.Called(ctx, query, embedding, limit)
	var result []store.MemoryEntry
	if value := args.Get(0); value != nil {
		result = value.([]store.MemoryEntry)
	}
	return result, args.Error(1)
}

func (m *MockStore) AppendEvent(ctx context.Context, event store.RunEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockStore) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]store.RunEvent, error) {
	args := m.Called(ctx, runID, afterSeq)
	var result []store.RunEvent
	if value := args.Get(0); value != nil {
		result = value.([]store.RunEvent)
	}
	return result, args.Error(1)
}

func (m *MockStore) ListRunSteps(ctx context.Context, runID string) ([]store.RunStep, error) {
	args := m.Called(ctx, runID)
	var result []store.RunStep
	if value := args.Get(0); value != nil {
		result = value.([]store.RunStep)
	}
	return result, args.Error(1)
}

func (m *MockStore) UpsertRunProcess(ctx context.Context, process store.RunProcess) error {
	args := m.Called(ctx, process)
	return args.Error(0)
}

func (m *MockStore) GetRunProcess(ctx context.Context, runID string, processID string) (*store.RunProcess, error) {
	args := m.Called(ctx, runID, processID)
	if value := args.Get(0); value != nil {
		return value.(*store.RunProcess), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockStore) ListRunProcesses(ctx context.Context, runID string) ([]store.RunProcess, error) {
	args := m.Called(ctx, runID)
	var result []store.RunProcess
	if value := args.Get(0); value != nil {
		result = value.([]store.RunProcess)
	}
	return result, args.Error(1)
}

func (m *MockStore) NextSeq(ctx context.Context, runID string) (int64, error) {
	args := m.Called(ctx, runID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockStore) UpsertArtifact(ctx context.Context, artifact store.Artifact) error {
	args := m.Called(ctx, artifact)
	return args.Error(0)
}

func (m *MockStore) ListArtifacts(ctx context.Context, runID string) ([]store.Artifact, error) {
	args := m.Called(ctx, runID)
	var result []store.Artifact
	if value := args.Get(0); value != nil {
		result = value.([]store.Artifact)
	}
	return result, args.Error(1)
}

func (m *MockStore) ListAutomations(ctx context.Context) ([]store.Automation, error) {
	args := m.Called(ctx)
	var result []store.Automation
	if value := args.Get(0); value != nil {
		result = value.([]store.Automation)
	}
	return result, args.Error(1)
}

func (m *MockStore) GetAutomation(ctx context.Context, automationID string) (*store.Automation, error) {
	args := m.Called(ctx, automationID)
	if value := args.Get(0); value != nil {
		return value.(*store.Automation), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockStore) CreateAutomation(ctx context.Context, automation store.Automation) error {
	args := m.Called(ctx, automation)
	return args.Error(0)
}

func (m *MockStore) UpdateAutomation(ctx context.Context, automation store.Automation) error {
	args := m.Called(ctx, automation)
	return args.Error(0)
}

func (m *MockStore) DeleteAutomation(ctx context.Context, automationID string) error {
	args := m.Called(ctx, automationID)
	return args.Error(0)
}

func (m *MockStore) ListAutomationInbox(ctx context.Context, automationID string) ([]store.AutomationInboxEntry, error) {
	args := m.Called(ctx, automationID)
	var result []store.AutomationInboxEntry
	if value := args.Get(0); value != nil {
		result = value.([]store.AutomationInboxEntry)
	}
	return result, args.Error(1)
}

func (m *MockStore) CreateAutomationInboxEntry(ctx context.Context, entry store.AutomationInboxEntry) error {
	args := m.Called(ctx, entry)
	return args.Error(0)
}

func (m *MockStore) UpdateAutomationInboxEntry(ctx context.Context, entry store.AutomationInboxEntry) error {
	args := m.Called(ctx, entry)
	return args.Error(0)
}

func (m *MockStore) MarkAutomationInboxEntryRead(ctx context.Context, automationID string, entryID string) error {
	args := m.Called(ctx, automationID, entryID)
	return args.Error(0)
}

func (m *MockStore) MarkAutomationInboxReadAll(ctx context.Context, automationID string) error {
	args := m.Called(ctx, automationID)
	return args.Error(0)
}

type MockBroker struct {
	mock.Mock
}

func (m *MockBroker) Publish(event events.RunEvent) {
	m.Called(event)
}

func (m *MockBroker) Subscribe(ctx context.Context, runID string) <-chan events.RunEvent {
	args := m.Called(ctx, runID)
	if value := args.Get(0); value != nil {
		if ch, ok := value.(chan events.RunEvent); ok {
			return ch
		}
		if ch, ok := value.(<-chan events.RunEvent); ok {
			return ch
		}
	}
	return nil
}

type MockWorkflowService struct {
	mock.Mock
}

func (m *MockWorkflowService) StartRun(ctx context.Context, runID string) error {
	args := m.Called(ctx, runID)
	return args.Error(0)
}

func (m *MockWorkflowService) SignalMessage(ctx context.Context, runID string, message string) error {
	args := m.Called(ctx, runID, message)
	return args.Error(0)
}

func (m *MockWorkflowService) ResumeRun(ctx context.Context, runID string, message string) error {
	args := m.Called(ctx, runID, message)
	return args.Error(0)
}

func (m *MockWorkflowService) CancelRun(ctx context.Context, runID string) error {
	args := m.Called(ctx, runID)
	return args.Error(0)
}

type MockProvider struct {
	mock.Mock
}

func (m *MockProvider) Generate(ctx context.Context, messages []llm.Message) (string, error) {
	args := m.Called(ctx, messages)
	return args.String(0), args.Error(1)
}

func newTestServer(t *testing.T, store store.Store, broker Broker, workflows WorkflowService, cfg config.Config) *httptest.Server {
	t.Helper()
	server := NewServer(store, broker, workflows, cfg)
	return httptest.NewServer(server.Router())
}
