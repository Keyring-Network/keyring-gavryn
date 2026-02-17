package skills

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

//go:embed builtin/*/SKILL.md
var builtinSkillFiles embed.FS

type BuiltinSpec struct {
	Name         string
	Description  string
	MarkdownPath string
}

var builtinSpecs = []BuiltinSpec{
	{
		Name:         "gavryn-tool-contracts",
		Description:  "Canonical browser/editor/process tool schemas with valid field names.",
		MarkdownPath: "builtin/gavryn-tool-contracts/SKILL.md",
	},
	{
		Name:         "gavryn-dev-recipes",
		Description:  "Project scaffolding and dev server/preview command recipes for Gavryn.",
		MarkdownPath: "builtin/gavryn-dev-recipes/SKILL.md",
	},
	{
		Name:         "gavryn-browser-research",
		Description:  "Reliable browser research workflow using supported browser tools.",
		MarkdownPath: "builtin/gavryn-browser-research/SKILL.md",
	},
}

func BuiltinSkills() []BuiltinSpec {
	copyOf := make([]BuiltinSpec, len(builtinSpecs))
	copy(copyOf, builtinSpecs)
	return copyOf
}

func EnsureBuiltins(ctx context.Context, st store.Store) error {
	existing, err := st.ListSkills(ctx)
	if err != nil {
		return err
	}
	existingNames := make(map[string]struct{}, len(existing))
	for _, skill := range existing {
		existingNames[strings.ToLower(strings.TrimSpace(skill.Name))] = struct{}{}
	}

	rootDir, rootErr := RootDir()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, spec := range builtinSpecs {
		if _, ok := existingNames[strings.ToLower(spec.Name)]; ok {
			continue
		}
		content, err := builtinSkillFiles.ReadFile(spec.MarkdownPath)
		if err != nil {
			return fmt.Errorf("read builtin %s: %w", spec.Name, err)
		}

		skillID := uuid.NewString()
		skill := store.Skill{
			ID:          skillID,
			Name:        spec.Name,
			Description: spec.Description,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := st.CreateSkill(ctx, skill); err != nil {
			return err
		}

		file := store.SkillFile{
			ID:          uuid.NewString(),
			SkillID:     skillID,
			Path:        "SKILL.md",
			Content:     content,
			ContentType: "text/markdown",
			SizeBytes:   int64(len(content)),
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := st.UpsertSkillFile(ctx, file); err != nil {
			return err
		}

		if rootErr == nil {
			if err := MaterializeSkill(rootDir, spec.Name, []store.SkillFile{file}); err != nil {
				return err
			}
		}
	}
	return nil
}
