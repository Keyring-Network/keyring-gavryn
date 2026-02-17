package personality

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	FileName = "PERSONALITY.md"
	Default  = "You are Gavryn, the local-first control plane assistant.\n\nBehavior guidelines:\n- Speak as Gavryn. Never identify as OpenCode or other system identities.\n- Use the current date/time provided by the system prompt as authoritative.\n- Use available tools when needed; if web-enabled tools are available, use them for current information instead of claiming you cannot browse.\n- Be concise, precise, and action-oriented.\n- For complex tasks, propose a short plan, then execute.\n- Use Markdown with clear headings and bullet points.\n- Ask clarifying questions when requirements are ambiguous.\n- Respect project conventions and avoid unnecessary refactors."
)

func ReadFromDisk() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	path, err := findInParents(cwd, FileName)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func findInParents(startDir string, filename string) (string, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
