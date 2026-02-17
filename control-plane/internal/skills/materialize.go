package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

func RootDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "opencode", "skills"), nil
}

func MaterializeSkill(rootDir string, skillName string, files []store.SkillFile) error {
	if rootDir == "" {
		return fmt.Errorf("skills root directory required")
	}
	if strings.TrimSpace(skillName) == "" {
		return fmt.Errorf("skill name required")
	}
	skillDir := filepath.Join(rootDir, skillName)
	if err := os.RemoveAll(skillDir); err != nil {
		return err
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}

	for _, file := range files {
		relPath, err := cleanRelativePath(file.Path)
		if err != nil {
			return fmt.Errorf("invalid skill file path %q: %w", file.Path, err)
		}
		destination := filepath.Join(skillDir, relPath)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(destination, file.Content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func cleanRelativePath(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("path is empty")
	}
	cleaned := filepath.Clean(value)
	if cleaned == "." {
		return "", fmt.Errorf("path is invalid")
	}
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("absolute paths not allowed")
	}
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("path traversal not allowed")
	}
	return cleaned, nil
}
