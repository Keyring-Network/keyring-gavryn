package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Keyring-Network/keyring-gavryn/control-plane/internal/store"
)

func TestRootDir(t *testing.T) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	root, err := RootDir()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := filepath.Join(configDir, "opencode", "skills")
	if root != expected {
		t.Fatalf("expected %q, got %q", expected, root)
	}
}

func TestRootDir_NoHome(t *testing.T) {
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", "")
	case "darwin", "ios":
		t.Setenv("HOME", "")
	default:
		t.Setenv("XDG_CONFIG_HOME", "relative/path")
	}
	_, err := RootDir()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCleanRelativePath_Valid(t *testing.T) {
	paths := map[string]string{
		"file.txt":        "file.txt",
		"dir/file.txt":    "dir/file.txt",
		"dir/../file.txt": "file.txt",
	}
	for input, expected := range paths {
		result, err := cleanRelativePath(input)
		if err != nil {
			t.Fatalf("expected no error for %q, got %v", input, err)
		}
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	}
}

func TestCleanRelativePath_Absolute(t *testing.T) {
	absPath := filepath.Join(string(filepath.Separator), "tmp", "file.txt")
	_, err := cleanRelativePath(absPath)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCleanRelativePath_Traversal(t *testing.T) {
	_, err := cleanRelativePath("../secret")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCleanRelativePath_Empty(t *testing.T) {
	_, err := cleanRelativePath("")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCleanRelativePath_Dot(t *testing.T) {
	_, err := cleanRelativePath(".")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaterializeSkill_Success(t *testing.T) {
	rootDir := t.TempDir()
	files := []store.SkillFile{{Path: "tool.txt", Content: []byte("hello")}}
	if err := MaterializeSkill(rootDir, "skill", files); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	path := filepath.Join(rootDir, "skill", "tool.txt")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(content))
	}
}

func TestMaterializeSkill_EmptyRoot(t *testing.T) {
	err := MaterializeSkill("", "skill", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaterializeSkill_EmptyName(t *testing.T) {
	err := MaterializeSkill(t.TempDir(), " ", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaterializeSkill_Overwrite(t *testing.T) {
	rootDir := t.TempDir()
	files := []store.SkillFile{{Path: "tool.txt", Content: []byte("one")}}
	if err := MaterializeSkill(rootDir, "skill", files); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	files = []store.SkillFile{{Path: "tool.txt", Content: []byte("two")}}
	if err := MaterializeSkill(rootDir, "skill", files); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	path := filepath.Join(rootDir, "skill", "tool.txt")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if string(content) != "two" {
		t.Fatalf("expected %q, got %q", "two", string(content))
	}
}

func TestMaterializeSkill_NestedPaths(t *testing.T) {
	rootDir := t.TempDir()
	files := []store.SkillFile{{Path: "dir/sub/file.txt", Content: []byte("nested")}}
	if err := MaterializeSkill(rootDir, "skill", files); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	path := filepath.Join(rootDir, "skill", "dir", "sub", "file.txt")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if string(content) != "nested" {
		t.Fatalf("expected %q, got %q", "nested", string(content))
	}
}

func TestMaterializeSkill_InvalidPath(t *testing.T) {
	rootDir := t.TempDir()
	files := []store.SkillFile{{Path: "../secret", Content: []byte("nope")}}
	err := MaterializeSkill(rootDir, "skill", files)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaterializeSkill_RemoveAllError(t *testing.T) {
	baseDir := t.TempDir()
	rootFile := filepath.Join(baseDir, "rootfile")
	if err := os.WriteFile(rootFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	err := MaterializeSkill(rootFile, "skill", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaterializeSkill_MkdirAllRootError(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Chmod(rootDir, 0o555); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(rootDir, 0o755)
	})
	err := MaterializeSkill(rootDir, "skill", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaterializeSkill_MkdirAllFileDirError(t *testing.T) {
	rootDir := t.TempDir()
	files := []store.SkillFile{
		{Path: "parent", Content: []byte("one")},
		{Path: "parent/child.txt", Content: []byte("two")},
	}
	err := MaterializeSkill(rootDir, "skill", files)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaterializeSkill_WriteFileError(t *testing.T) {
	rootDir := t.TempDir()
	files := []store.SkillFile{
		{Path: "dir/file.txt", Content: []byte("one")},
		{Path: "dir", Content: []byte("two")},
	}
	err := MaterializeSkill(rootDir, "skill", files)
	if err == nil {
		t.Fatal("expected error")
	}
}
