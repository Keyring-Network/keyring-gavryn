package memory

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCreateRun(t *testing.T) {
	ctx := context.Background()
	mem := New()
	run := store.Run{ID: "run-1", Status: "running", CreatedAt: "now", UpdatedAt: "now"}

	if err := mem.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	mem.mu.RLock()
	defer mem.mu.RUnlock()
	stored, ok := mem.runs[run.ID]
	if !ok {
		t.Fatalf("expected run to be stored")
	}
	if stored.Status != run.Status {
		t.Fatalf("expected status %q, got %q", run.Status, stored.Status)
	}
}

func TestAddMessage(t *testing.T) {
	ctx := context.Background()
	mem := New()
	msg := store.Message{ID: "msg-1", RunID: "run-1", Role: "user", Content: "hi", Sequence: 1}

	if err := mem.AddMessage(ctx, msg); err != nil {
		t.Fatalf("add message: %v", err)
	}

	mem.mu.RLock()
	defer mem.mu.RUnlock()
	if len(mem.messages[msg.RunID]) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mem.messages[msg.RunID]))
	}
}

func TestAppendEvent_UpsertsRunSteps(t *testing.T) {
	ctx := context.Background()
	mem := New()
	runID := "run-1"
	require.NoError(t, mem.CreateRun(ctx, store.Run{ID: runID, Status: "running", CreatedAt: "now", UpdatedAt: "now"}))

	require.NoError(t, mem.AppendEvent(ctx, store.RunEvent{
		RunID:   runID,
		Seq:     1,
		Type:    "step.started",
		Source:  "llm",
		Payload: map[string]any{"step_id": "assistant_reply", "name": "Generate assistant reply"},
	}))
	require.NoError(t, mem.AppendEvent(ctx, store.RunEvent{
		RunID:   runID,
		Seq:     2,
		Type:    "step.failed",
		Source:  "llm",
		Payload: map[string]any{"step_id": "assistant_reply", "error": "provider timeout"},
	}))

	steps, err := mem.ListRunSteps(ctx, runID)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	require.Equal(t, "assistant_reply", steps[0].ID)
	require.Equal(t, "failed", steps[0].Status)
	require.Equal(t, "provider timeout", steps[0].Error)
}

func TestListRuns_UsesGeneratedTitleEvent(t *testing.T) {
	ctx := context.Background()
	mem := New()
	runID := "run-1"

	require.NoError(t, mem.CreateRun(ctx, store.Run{ID: runID, Status: "running", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}))
	require.NoError(t, mem.AddMessage(ctx, store.Message{ID: "msg-1", RunID: runID, Role: "user", Content: "first user message", Sequence: 1}))
	require.NoError(t, mem.AppendEvent(ctx, store.RunEvent{RunID: runID, Seq: 1, Type: "run.title.updated", Payload: map[string]any{"title": "AI Generated Title"}}))

	runs, err := mem.ListRuns(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, "AI Generated Title", runs[0].Title)
}

func TestListMessages(t *testing.T) {
	ctx := context.Background()
	mem := New()
	msg1 := store.Message{ID: "msg-1", RunID: "run-1", Role: "user", Content: "first", Sequence: 1}
	msg2 := store.Message{ID: "msg-2", RunID: "run-1", Role: "assistant", Content: "second", Sequence: 2}

	_ = mem.AddMessage(ctx, msg1)
	_ = mem.AddMessage(ctx, msg2)

	msgs, err := mem.ListMessages(ctx, "run-1")
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	msgs[0].Content = "mutated"
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	if mem.messages["run-1"][0].Content != "first" {
		t.Fatalf("expected stored message to remain unchanged")
	}
}

func TestGetLLMSettings(t *testing.T) {
	ctx := context.Background()
	mem := New()

	settings, err := mem.GetLLMSettings(ctx)
	if err != nil {
		t.Fatalf("get llm settings: %v", err)
	}
	if settings != nil {
		t.Fatalf("expected nil settings when empty")
	}

	_ = mem.UpsertLLMSettings(ctx, store.LLMSettings{Mode: "live", Provider: "openai"})
	settings, err = mem.GetLLMSettings(ctx)
	if err != nil {
		t.Fatalf("get llm settings: %v", err)
	}
	if settings == nil || settings.Provider != "openai" {
		t.Fatalf("expected settings to be stored")
	}
}

func TestUpsertLLMSettings(t *testing.T) {
	ctx := context.Background()
	mem := New()

	first := store.LLMSettings{Mode: "live", Provider: "openai"}
	if err := mem.UpsertLLMSettings(ctx, first); err != nil {
		t.Fatalf("upsert llm settings: %v", err)
	}

	second := store.LLMSettings{Mode: "dev", Provider: "local"}
	if err := mem.UpsertLLMSettings(ctx, second); err != nil {
		t.Fatalf("upsert llm settings: %v", err)
	}

	settings, _ := mem.GetLLMSettings(ctx)
	if settings.Provider != "local" || settings.Mode != "dev" {
		t.Fatalf("expected settings to be updated")
	}
}

func TestListSkills(t *testing.T) {
	ctx := context.Background()
	mem := New()

	_ = mem.CreateSkill(ctx, store.Skill{ID: "s-1", Name: "alpha"})
	_ = mem.CreateSkill(ctx, store.Skill{ID: "s-2", Name: "beta"})

	skills, err := mem.ListSkills(ctx)
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
}

func TestGetSkill(t *testing.T) {
	ctx := context.Background()
	mem := New()

	missing, err := mem.GetSkill(ctx, "missing")
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing skill")
	}

	_ = mem.CreateSkill(ctx, store.Skill{ID: "s-1", Name: "alpha"})
	found, err := mem.GetSkill(ctx, "s-1")
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if found == nil || found.Name != "alpha" {
		t.Fatalf("expected skill to be returned")
	}
}

func TestCreateSkill(t *testing.T) {
	ctx := context.Background()
	mem := New()
	skill := store.Skill{ID: "s-1", Name: "alpha"}

	if err := mem.CreateSkill(ctx, skill); err != nil {
		t.Fatalf("create skill: %v", err)
	}
	stored, _ := mem.GetSkill(ctx, "s-1")
	if stored == nil || stored.Name != "alpha" {
		t.Fatalf("expected skill to be stored")
	}
}

func TestUpdateSkill(t *testing.T) {
	ctx := context.Background()
	mem := New()

	_ = mem.CreateSkill(ctx, store.Skill{ID: "s-1", Name: "alpha"})
	if err := mem.UpdateSkill(ctx, store.Skill{ID: "s-1", Name: "beta"}); err != nil {
		t.Fatalf("update skill: %v", err)
	}

	skill, _ := mem.GetSkill(ctx, "s-1")
	if skill == nil || skill.Name != "beta" {
		t.Fatalf("expected skill to be updated")
	}
}

func TestDeleteSkill(t *testing.T) {
	ctx := context.Background()
	mem := New()

	_ = mem.CreateSkill(ctx, store.Skill{ID: "s-1", Name: "alpha"})
	_ = mem.UpsertSkillFile(ctx, store.SkillFile{ID: "f-1", SkillID: "s-1", Path: "README.md"})

	if err := mem.DeleteSkill(ctx, "s-1"); err != nil {
		t.Fatalf("delete skill: %v", err)
	}

	if skill, _ := mem.GetSkill(ctx, "s-1"); skill != nil {
		t.Fatalf("expected skill to be deleted")
	}
	if files := mem.files["s-1"]; files != nil {
		t.Fatalf("expected skill files to be deleted")
	}
}

func TestListSkillFiles(t *testing.T) {
	ctx := context.Background()
	mem := New()

	files, err := mem.ListSkillFiles(ctx, "s-1")
	if err != nil {
		t.Fatalf("list skill files: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no files")
	}

	_ = mem.UpsertSkillFile(ctx, store.SkillFile{ID: "f-1", SkillID: "s-1", Path: "README.md", Content: []byte("a")})
	files, err = mem.ListSkillFiles(ctx, "s-1")
	if err != nil {
		t.Fatalf("list skill files: %v", err)
	}
	if len(files) != 1 || files[0].Path != "README.md" {
		t.Fatalf("expected skill file to be listed")
	}
}

func TestUpsertSkillFile(t *testing.T) {
	ctx := context.Background()
	mem := New()

	file := store.SkillFile{ID: "f-1", SkillID: "s-1", Path: "README.md", Content: []byte("a")}
	if err := mem.UpsertSkillFile(ctx, file); err != nil {
		t.Fatalf("upsert skill file: %v", err)
	}

	file.Content = []byte("b")
	if err := mem.UpsertSkillFile(ctx, file); err != nil {
		t.Fatalf("upsert skill file: %v", err)
	}

	files, _ := mem.ListSkillFiles(ctx, "s-1")
	if len(files) != 1 || string(files[0].Content) != "b" {
		t.Fatalf("expected skill file to be updated")
	}
}

func TestDeleteSkillFile(t *testing.T) {
	ctx := context.Background()
	mem := New()

	if err := mem.DeleteSkillFile(ctx, "missing", "none"); err != nil {
		t.Fatalf("delete missing skill file: %v", err)
	}

	_ = mem.UpsertSkillFile(ctx, store.SkillFile{ID: "f-1", SkillID: "s-1", Path: "README.md"})
	if err := mem.DeleteSkillFile(ctx, "s-1", "README.md"); err != nil {
		t.Fatalf("delete skill file: %v", err)
	}

	files, _ := mem.ListSkillFiles(ctx, "s-1")
	if len(files) != 0 {
		t.Fatalf("expected no files after delete")
	}
}

func TestListContextNodes(t *testing.T) {
	ctx := context.Background()
	mem := New()

	_ = mem.CreateContextFolder(ctx, store.ContextNode{ID: "n-1", Name: "root", NodeType: "folder"})
	_ = mem.CreateContextFile(ctx, store.ContextNode{ID: "n-2", ParentID: "n-1", Name: "file.txt", NodeType: "file"})

	nodes, err := mem.ListContextNodes(ctx)
	if err != nil {
		t.Fatalf("list context nodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestGetContextFile(t *testing.T) {
	ctx := context.Background()
	mem := New()

	missing, err := mem.GetContextFile(ctx, "missing")
	if err != nil {
		t.Fatalf("get context file: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing context file")
	}

	_ = mem.CreateContextFile(ctx, store.ContextNode{ID: "n-1", Name: "file.txt", NodeType: "file", Content: []byte("ok")})
	file, err := mem.GetContextFile(ctx, "n-1")
	if err != nil {
		t.Fatalf("get context file: %v", err)
	}
	if file == nil || string(file.Content) != "ok" {
		t.Fatalf("expected context file content to be returned")
	}
}

func TestCreateContextFolder(t *testing.T) {
	ctx := context.Background()
	mem := New()

	node := store.ContextNode{ID: "n-1", Name: "root", NodeType: "folder"}
	if err := mem.CreateContextFolder(ctx, node); err != nil {
		t.Fatalf("create context folder: %v", err)
	}

	mem.mu.RLock()
	defer mem.mu.RUnlock()
	if _, ok := mem.context[node.ID]; !ok {
		t.Fatalf("expected context folder to be stored")
	}
}

func TestCreateContextFile(t *testing.T) {
	ctx := context.Background()
	mem := New()

	node := store.ContextNode{ID: "n-1", Name: "file.txt", NodeType: "file", Content: []byte("data")}
	if err := mem.CreateContextFile(ctx, node); err != nil {
		t.Fatalf("create context file: %v", err)
	}

	if got, _ := mem.GetContextFile(ctx, "n-1"); got == nil {
		t.Fatalf("expected context file to be stored")
	}
}

func TestDeleteContextNode(t *testing.T) {
	ctx := context.Background()
	mem := New()

	_ = mem.CreateContextFolder(ctx, store.ContextNode{ID: "root", Name: "root", NodeType: "folder"})
	_ = mem.CreateContextFolder(ctx, store.ContextNode{ID: "child", ParentID: "root", Name: "child", NodeType: "folder"})
	_ = mem.CreateContextFile(ctx, store.ContextNode{ID: "file", ParentID: "child", Name: "file.txt", NodeType: "file"})

	if err := mem.DeleteContextNode(ctx, "root"); err != nil {
		t.Fatalf("delete context node: %v", err)
	}

	mem.mu.RLock()
	defer mem.mu.RUnlock()
	if len(mem.context) != 0 {
		t.Fatalf("expected context subtree to be deleted")
	}
}

func TestGetMemorySettings(t *testing.T) {
	ctx := context.Background()
	mem := New()

	settings, err := mem.GetMemorySettings(ctx)
	if err != nil {
		t.Fatalf("get memory settings: %v", err)
	}
	if settings != nil {
		t.Fatalf("expected nil memory settings when empty")
	}

	_ = mem.UpsertMemorySettings(ctx, store.MemorySettings{Enabled: true})
	settings, err = mem.GetMemorySettings(ctx)
	if err != nil {
		t.Fatalf("get memory settings: %v", err)
	}
	if settings == nil || !settings.Enabled {
		t.Fatalf("expected memory settings to be stored")
	}
}

func TestUpsertMemorySettings(t *testing.T) {
	ctx := context.Background()
	mem := New()

	if err := mem.UpsertMemorySettings(ctx, store.MemorySettings{Enabled: true}); err != nil {
		t.Fatalf("upsert memory settings: %v", err)
	}

	if err := mem.UpsertMemorySettings(ctx, store.MemorySettings{Enabled: false}); err != nil {
		t.Fatalf("upsert memory settings: %v", err)
	}

	settings, _ := mem.GetMemorySettings(ctx)
	if settings == nil || settings.Enabled {
		t.Fatalf("expected memory settings to be updated")
	}
}

func TestGetPersonalitySettings(t *testing.T) {
	ctx := context.Background()
	mem := New()

	settings, err := mem.GetPersonalitySettings(ctx)
	if err != nil {
		t.Fatalf("get personality settings: %v", err)
	}
	if settings != nil {
		t.Fatalf("expected nil personality settings when empty")
	}

	_ = mem.UpsertPersonalitySettings(ctx, store.PersonalitySettings{Content: "Be Gavryn."})
	settings, err = mem.GetPersonalitySettings(ctx)
	if err != nil {
		t.Fatalf("get personality settings: %v", err)
	}
	if settings == nil || settings.Content != "Be Gavryn." {
		t.Fatalf("expected personality settings to be stored")
	}
}

func TestUpsertPersonalitySettings(t *testing.T) {
	ctx := context.Background()
	mem := New()

	if err := mem.UpsertPersonalitySettings(ctx, store.PersonalitySettings{Content: "First"}); err != nil {
		t.Fatalf("upsert personality settings: %v", err)
	}

	if err := mem.UpsertPersonalitySettings(ctx, store.PersonalitySettings{Content: "Second"}); err != nil {
		t.Fatalf("upsert personality settings: %v", err)
	}

	settings, _ := mem.GetPersonalitySettings(ctx)
	if settings == nil || settings.Content != "Second" {
		t.Fatalf("expected personality settings to be updated")
	}
}

func TestSearchMemory(t *testing.T) {
	ctx := context.Background()
	mem := New()

	results, err := mem.SearchMemory(ctx, "", 5)
	if err != nil {
		t.Fatalf("search memory: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results for empty query")
	}

	mem.entries = []store.MemoryEntry{
		{ID: "m-1", Content: "Hello World"},
		{ID: "m-2", Content: "Another note"},
	}

	results, err = mem.SearchMemory(ctx, "hello", 1)
	if err != nil {
		t.Fatalf("search memory: %v", err)
	}
	if len(results) != 1 || results[0].ID != "m-1" {
		t.Fatalf("expected search to return matching entry")
	}
}

func TestUpsertMemoryEntry_DedupeByFingerprint(t *testing.T) {
	ctx := context.Background()
	mem := New()

	entry := store.MemoryEntry{
		ID:      "m-1",
		Content: "Hello",
		Metadata: map[string]any{
			"fingerprint": "abc123",
		},
	}
	inserted, err := mem.UpsertMemoryEntry(ctx, entry)
	if err != nil {
		t.Fatalf("upsert memory entry: %v", err)
	}
	if !inserted {
		t.Fatalf("expected entry to be inserted")
	}
	inserted, err = mem.UpsertMemoryEntry(ctx, entry)
	if err != nil {
		t.Fatalf("upsert memory entry: %v", err)
	}
	if inserted {
		t.Fatalf("expected entry to be deduped")
	}
}

func TestSearchMemoryWithEmbedding_Fallback(t *testing.T) {
	ctx := context.Background()
	mem := New()
	mem.entries = []store.MemoryEntry{{ID: "m-1", Content: "Hello World"}}

	results, err := mem.SearchMemoryWithEmbedding(ctx, "hello", []float32{0.1}, 1)
	if err != nil {
		t.Fatalf("search memory with embedding: %v", err)
	}
	if len(results) != 1 || results[0].ID != "m-1" {
		t.Fatalf("expected search to return matching entry")
	}
}

func TestMemoryStoreSequencingAndEvents(t *testing.T) {
	ctx := context.Background()
	mem := New()
	runID := "run-1"

	seq1, err := mem.NextSeq(ctx, runID)
	if err != nil {
		t.Fatalf("next seq: %v", err)
	}
	if seq1 != 1 {
		t.Fatalf("expected seq 1, got %d", seq1)
	}

	seq2, err := mem.NextSeq(ctx, runID)
	if err != nil {
		t.Fatalf("next seq: %v", err)
	}
	if seq2 != 2 {
		t.Fatalf("expected seq 2, got %d", seq2)
	}

	if err := mem.AppendEvent(ctx, store.RunEvent{RunID: runID, Seq: seq1, Type: "run.started"}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := mem.AppendEvent(ctx, store.RunEvent{RunID: runID, Seq: seq2, Type: "message.added"}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	all, err := mem.ListEvents(ctx, runID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 events, got %d", len(all))
	}

	filtered, err := mem.ListEvents(ctx, runID, 1)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Seq != 2 {
		t.Fatalf("expected 1 event after seq 1, got %+v", filtered)
	}
}

func TestArtifacts(t *testing.T) {
	ctx := context.Background()
	mem := New()

	artifacts, err := mem.ListArtifacts(ctx, "run-1")
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("expected empty artifacts")
	}

	artifact := store.Artifact{
		ID:        "artifact-1",
		RunID:     "run-1",
		Type:      "file",
		URI:       "http://example.com/file.txt",
		CreatedAt: "2024-01-01T00:00:00Z",
	}
	if err := mem.UpsertArtifact(ctx, artifact); err != nil {
		t.Fatalf("upsert artifact: %v", err)
	}

	artifact.Type = "image"
	if err := mem.UpsertArtifact(ctx, artifact); err != nil {
		t.Fatalf("upsert artifact: %v", err)
	}

	artifacts, err = mem.ListArtifacts(ctx, "run-1")
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].Type != "image" {
		t.Fatalf("expected updated artifact type")
	}
}

func TestRunProcesses(t *testing.T) {
	ctx := context.Background()
	mem := New()

	process := store.RunProcess{
		RunID:       "run-1",
		ProcessID:   "proc-1",
		Command:     "npm",
		Args:        []string{"run", "dev"},
		Status:      "running",
		PID:         1234,
		StartedAt:   "2026-02-07T00:00:00Z",
		PreviewURLs: []string{"http://localhost:3000"},
	}
	require.NoError(t, mem.UpsertRunProcess(ctx, process))

	code := 0
	process.Status = "exited"
	process.ExitCode = &code
	process.EndedAt = "2026-02-07T00:01:00Z"
	require.NoError(t, mem.UpsertRunProcess(ctx, process))

	found, err := mem.GetRunProcess(ctx, "run-1", "proc-1")
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, "exited", found.Status)
	require.NotNil(t, found.ExitCode)
	require.Equal(t, 0, *found.ExitCode)

	listed, err := mem.ListRunProcesses(ctx, "run-1")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "proc-1", listed[0].ProcessID)
}

func TestConcurrentNextSeq(t *testing.T) {
	ctx := context.Background()
	mem := New()
	runID := "run-1"
	count := 25

	results := make([]int64, 0, count)
	mu := sync.Mutex{}
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seq, err := mem.NextSeq(ctx, runID)
			if err != nil {
				t.Errorf("next seq: %v", err)
				return
			}
			mu.Lock()
			results = append(results, seq)
			mu.Unlock()
		}()
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i] < results[j] })
	if len(results) != count {
		t.Fatalf("expected %d results, got %d", count, len(results))
	}
	for i, seq := range results {
		if seq != int64(i+1) {
			t.Fatalf("expected seq %d, got %d", i+1, seq)
		}
	}
}
