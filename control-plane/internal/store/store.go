package store

import "context"

type Run struct {
	ID               string
	Status           string
	Phase            string
	CompletionReason string
	ResumedFrom      string
	CheckpointSeq    int64
	PolicyProfile    string
	ModelRoute       string
	Tags             []string
	CreatedAt        string
	UpdatedAt        string
}

type RunSummary struct {
	ID               string
	Status           string
	Phase            string
	CompletionReason string
	ResumedFrom      string
	CheckpointSeq    int64
	PolicyProfile    string
	ModelRoute       string
	Tags             []string
	Title            string
	CreatedAt        string
	UpdatedAt        string
	MessageCount     int64
}

type Message struct {
	ID        string
	RunID     string
	Role      string
	Content   string
	Sequence  int64
	CreatedAt string
	Metadata  map[string]any
}

type LLMSettings struct {
	Mode          string
	Provider      string
	Model         string
	BaseURL       string
	APIKeyEnc     string
	CodexAuthPath string
	CodexHome     string
	CreatedAt     string
	UpdatedAt     string
}

type RunEvent struct {
	RunID     string
	Seq       int64
	Type      string
	Timestamp string
	Source    string
	TraceID   string
	Payload   map[string]any
}

type RunStep struct {
	RunID             string
	ID                string
	ParentStepID      string
	Name              string
	Kind              string
	Status            string
	PlanID            string
	Attempt           int
	PolicyDecision    string
	Source            string
	Seq               int64
	StartedAt         string
	CompletedAt       string
	Error             string
	Dependencies      []string
	ExpectedArtifacts []string
	Diagnostics       map[string]any
}

type RunProcess struct {
	RunID       string
	ProcessID   string
	Command     string
	Args        []string
	Cwd         string
	Status      string
	PID         int
	StartedAt   string
	EndedAt     string
	ExitCode    *int
	Signal      string
	PreviewURLs []string
	Metadata    map[string]any
}

type Artifact struct {
	ID             string
	RunID          string
	Type           string
	Category       string
	URI            string
	ContentType    string
	SizeBytes      int64
	Checksum       string
	Labels         []string
	SearchableText string
	RetentionClass string
	CreatedAt      string
	Metadata       map[string]any
}

type Automation struct {
	ID         string
	Name       string
	Prompt     string
	Model      string
	Days       []string
	TimeOfDay  string
	Timezone   string
	Enabled    bool
	NextRunAt  string
	LastRunAt  string
	InProgress bool
	CreatedAt  string
	UpdatedAt  string
}

type AutomationInboxEntry struct {
	ID               string
	AutomationID     string
	RunID            string
	Status           string
	Phase            string
	CompletionReason string
	FinalResponse    string
	TimedOut         bool
	Error            string
	Unread           bool
	Trigger          string
	StartedAt        string
	CompletedAt      string
	Diagnostics      map[string]any
	CreatedAt        string
	UpdatedAt        string
}

type Skill struct {
	ID          string
	Name        string
	Description string
	CreatedAt   string
	UpdatedAt   string
}

type SkillFile struct {
	ID          string
	SkillID     string
	Path        string
	Content     []byte
	ContentType string
	SizeBytes   int64
	CreatedAt   string
	UpdatedAt   string
}

type ContextNode struct {
	ID          string
	ParentID    string
	Name        string
	NodeType    string
	Content     []byte
	ContentType string
	SizeBytes   int64
	CreatedAt   string
	UpdatedAt   string
}

type MemorySettings struct {
	Enabled   bool
	CreatedAt string
	UpdatedAt string
}

type PersonalitySettings struct {
	Content   string
	CreatedAt string
	UpdatedAt string
}

type MemoryEntry struct {
	ID        string
	Content   string
	Metadata  map[string]any
	Embedding []float32
	CreatedAt string
	UpdatedAt string
}

type Store interface {
	DeleteRun(ctx context.Context, runID string) error
	ListRuns(ctx context.Context) ([]RunSummary, error)
	CreateRun(ctx context.Context, run Run) error
	AddMessage(ctx context.Context, msg Message) error
	ListMessages(ctx context.Context, runID string) ([]Message, error)
	GetLLMSettings(ctx context.Context) (*LLMSettings, error)
	UpsertLLMSettings(ctx context.Context, settings LLMSettings) error
	ListSkills(ctx context.Context) ([]Skill, error)
	GetSkill(ctx context.Context, skillID string) (*Skill, error)
	CreateSkill(ctx context.Context, skill Skill) error
	UpdateSkill(ctx context.Context, skill Skill) error
	DeleteSkill(ctx context.Context, skillID string) error
	ListSkillFiles(ctx context.Context, skillID string) ([]SkillFile, error)
	UpsertSkillFile(ctx context.Context, file SkillFile) error
	DeleteSkillFile(ctx context.Context, skillID string, path string) error
	ListContextNodes(ctx context.Context) ([]ContextNode, error)
	GetContextFile(ctx context.Context, nodeID string) (*ContextNode, error)
	CreateContextFolder(ctx context.Context, node ContextNode) error
	CreateContextFile(ctx context.Context, node ContextNode) error
	DeleteContextNode(ctx context.Context, nodeID string) error
	GetMemorySettings(ctx context.Context) (*MemorySettings, error)
	UpsertMemorySettings(ctx context.Context, settings MemorySettings) error
	UpsertMemoryEntry(ctx context.Context, entry MemoryEntry) (bool, error)
	SearchMemoryWithEmbedding(ctx context.Context, query string, embedding []float32, limit int) ([]MemoryEntry, error)
	GetPersonalitySettings(ctx context.Context) (*PersonalitySettings, error)
	UpsertPersonalitySettings(ctx context.Context, settings PersonalitySettings) error
	SearchMemory(ctx context.Context, query string, limit int) ([]MemoryEntry, error)
	AppendEvent(ctx context.Context, event RunEvent) error
	ListEvents(ctx context.Context, runID string, afterSeq int64) ([]RunEvent, error)
	ListRunSteps(ctx context.Context, runID string) ([]RunStep, error)
	UpsertRunProcess(ctx context.Context, process RunProcess) error
	GetRunProcess(ctx context.Context, runID string, processID string) (*RunProcess, error)
	ListRunProcesses(ctx context.Context, runID string) ([]RunProcess, error)
	NextSeq(ctx context.Context, runID string) (int64, error)
	UpsertArtifact(ctx context.Context, artifact Artifact) error
	ListArtifacts(ctx context.Context, runID string) ([]Artifact, error)
	ListAutomations(ctx context.Context) ([]Automation, error)
	GetAutomation(ctx context.Context, automationID string) (*Automation, error)
	CreateAutomation(ctx context.Context, automation Automation) error
	UpdateAutomation(ctx context.Context, automation Automation) error
	DeleteAutomation(ctx context.Context, automationID string) error
	ListAutomationInbox(ctx context.Context, automationID string) ([]AutomationInboxEntry, error)
	CreateAutomationInboxEntry(ctx context.Context, entry AutomationInboxEntry) error
	UpdateAutomationInboxEntry(ctx context.Context, entry AutomationInboxEntry) error
	MarkAutomationInboxEntryRead(ctx context.Context, automationID string, entryID string) error
	MarkAutomationInboxReadAll(ctx context.Context, automationID string) error
}
