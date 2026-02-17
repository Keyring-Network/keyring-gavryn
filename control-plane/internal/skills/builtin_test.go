package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store/memory"
)

func TestBuiltinSkillsList(t *testing.T) {
	specs := BuiltinSkills()
	if len(specs) == 0 {
		t.Fatal("expected builtin skills")
	}
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) == "" {
			t.Fatal("expected builtin name")
		}
		if strings.TrimSpace(spec.Description) == "" {
			t.Fatal("expected builtin description")
		}
		content, err := builtinSkillFiles.ReadFile(spec.MarkdownPath)
		if err != nil {
			t.Fatalf("read markdown for %s: %v", spec.Name, err)
		}
		if len(content) == 0 {
			t.Fatalf("expected markdown content for %s", spec.Name)
		}
	}
}

func TestEnsureBuiltinsSeedsMissingSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	st := memory.New()

	if err := EnsureBuiltins(ctx, st); err != nil {
		t.Fatalf("ensure builtins: %v", err)
	}

	all, err := st.ListSkills(ctx)
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(all) != len(BuiltinSkills()) {
		t.Fatalf("expected %d skills, got %d", len(BuiltinSkills()), len(all))
	}

	rootDir, err := RootDir()
	if err != nil {
		t.Fatalf("root dir: %v", err)
	}
	for _, spec := range BuiltinSkills() {
		skill, ok := findSkillByName(all, spec.Name)
		if !ok {
			t.Fatalf("missing skill %s", spec.Name)
		}
		files, err := st.ListSkillFiles(ctx, skill.ID)
		if err != nil {
			t.Fatalf("list files for %s: %v", spec.Name, err)
		}
		if len(files) != 1 || files[0].Path != "SKILL.md" {
			t.Fatalf("expected SKILL.md for %s", spec.Name)
		}
		if _, err := os.Stat(filepath.Join(rootDir, spec.Name, "SKILL.md")); err != nil {
			t.Fatalf("expected materialized file for %s: %v", spec.Name, err)
		}
	}
}

func TestEnsureBuiltinsDoesNotOverwriteExistingSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	st := memory.New()

	spec := BuiltinSkills()[0]
	now := time.Now().UTC().Format(time.RFC3339Nano)
	customSkillID := uuid.NewString()
	if err := st.CreateSkill(ctx, store.Skill{
		ID:          customSkillID,
		Name:        spec.Name,
		Description: "custom",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("create custom skill: %v", err)
	}
	if err := st.UpsertSkillFile(ctx, store.SkillFile{
		ID:          uuid.NewString(),
		SkillID:     customSkillID,
		Path:        "SKILL.md",
		Content:     []byte("# custom"),
		ContentType: "text/markdown",
		SizeBytes:   int64(len("# custom")),
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("create custom skill file: %v", err)
	}

	if err := EnsureBuiltins(ctx, st); err != nil {
		t.Fatalf("ensure builtins: %v", err)
	}

	files, err := st.ListSkillFiles(ctx, customSkillID)
	if err != nil {
		t.Fatalf("list custom files: %v", err)
	}
	if len(files) != 1 || string(files[0].Content) != "# custom" {
		t.Fatal("expected existing skill content to remain unchanged")
	}
}

func findSkillByName(skills []store.Skill, name string) (store.Skill, bool) {
	for _, skill := range skills {
		if skill.Name == name {
			return skill, true
		}
	}
	return store.Skill{}, false
}
