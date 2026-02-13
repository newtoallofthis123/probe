package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func loadTestGitIgnore(t *testing.T, gitignoreContent string) *GitIgnore {
	t.Helper()
	dir := t.TempDir()
	if gitignoreContent != "" {
		os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignoreContent), 0644)
	}
	return LoadGitIgnore(dir)
}

func TestGitIgnoreBasic(t *testing.T) {
	gi := loadTestGitIgnore(t, "node_modules\n")

	if !gi.IsIgnored("node_modules/foo.js") {
		t.Error("expected node_modules/foo.js to be ignored")
	}
	if gi.IsIgnored("src/main.go") {
		t.Error("expected src/main.go to NOT be ignored")
	}
}

func TestGitIgnoreWildcard(t *testing.T) {
	gi := loadTestGitIgnore(t, "*.log\n")

	if !gi.IsIgnored("app.log") {
		t.Error("expected app.log to be ignored")
	}
	if gi.IsIgnored("app.txt") {
		t.Error("expected app.txt to NOT be ignored")
	}
}

func TestGitIgnoreNegation(t *testing.T) {
	gi := loadTestGitIgnore(t, "*.log\n!keep.log\n")

	if !gi.IsIgnored("app.log") {
		t.Error("expected app.log to be ignored")
	}
	if gi.IsIgnored("keep.log") {
		t.Error("expected keep.log to NOT be ignored (negation)")
	}
}

func TestGitIgnoreDirectory(t *testing.T) {
	gi := loadTestGitIgnore(t, "build/\n")

	if !gi.IsIgnored("build/output.js") {
		t.Error("expected build/output.js to be ignored")
	}
	if gi.IsIgnored("builder.go") {
		t.Error("expected builder.go to NOT be ignored")
	}
}

func TestGitIgnoreUnconditional(t *testing.T) {
	// Even with no .gitignore, .git and .DS_Store are ignored
	gi := loadTestGitIgnore(t, "")

	if !gi.IsIgnored(".git/HEAD") {
		t.Error("expected .git/HEAD to be ignored unconditionally")
	}
	if !gi.IsIgnored(".DS_Store") {
		t.Error("expected .DS_Store to be ignored unconditionally")
	}
}

func TestSafePathTraversal(t *testing.T) {
	dir := t.TempDir()
	_, err := SafePath(dir, "../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestSafePathValid(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hi"), 0644)
	resolved, err := SafePath(dir, "test.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Resolve symlinks on expected path too (macOS /var → /private/var)
	expected, _ := filepath.EvalSymlinks(filepath.Join(dir, "test.txt"))
	if resolved != expected {
		t.Errorf("expected %s, got %s", expected, resolved)
	}
}
