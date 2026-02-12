package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupIgnoreDir(t *testing.T, gitignoreContent string, files []string) ToolContext {
	t.Helper()
	dir := t.TempDir()

	// Write .gitignore
	if gitignoreContent != "" {
		os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignoreContent), 0644)
	}

	// Create files and directories
	for _, f := range files {
		path := filepath.Join(dir, f)
		if strings.HasSuffix(f, "/") {
			os.MkdirAll(path, 0755)
		} else {
			os.MkdirAll(filepath.Dir(path), 0755)
			os.WriteFile(path, []byte("content"), 0644)
		}
	}

	return ToolContext{
		ProjectDir: dir,
		GitIgnore:  LoadGitIgnore(dir),
	}
}

func TestGitIgnoreBasic(t *testing.T) {
	gi := setupIgnoreDir(t, "node_modules\n", nil).GitIgnore

	if !gi.IsIgnored("node_modules/foo.js") {
		t.Error("expected node_modules/foo.js to be ignored")
	}
	if gi.IsIgnored("src/main.go") {
		t.Error("expected src/main.go to NOT be ignored")
	}
}

func TestGitIgnoreWildcard(t *testing.T) {
	gi := setupIgnoreDir(t, "*.log\n", nil).GitIgnore

	if !gi.IsIgnored("app.log") {
		t.Error("expected app.log to be ignored")
	}
	if gi.IsIgnored("app.txt") {
		t.Error("expected app.txt to NOT be ignored")
	}
}

func TestGitIgnoreNegation(t *testing.T) {
	gi := setupIgnoreDir(t, "*.log\n!keep.log\n", nil).GitIgnore

	if !gi.IsIgnored("app.log") {
		t.Error("expected app.log to be ignored")
	}
	if gi.IsIgnored("keep.log") {
		t.Error("expected keep.log to NOT be ignored (negation)")
	}
}

func TestGitIgnoreDirectory(t *testing.T) {
	gi := setupIgnoreDir(t, "build/\n", nil).GitIgnore

	if !gi.IsIgnored("build/output.js") {
		t.Error("expected build/output.js to be ignored")
	}
	if gi.IsIgnored("builder.go") {
		t.Error("expected builder.go to NOT be ignored")
	}
}

func TestGitIgnoreUnconditional(t *testing.T) {
	// Even with no .gitignore, .git and .DS_Store are ignored
	gi := setupIgnoreDir(t, "", nil).GitIgnore

	if !gi.IsIgnored(".git/HEAD") {
		t.Error("expected .git/HEAD to be ignored unconditionally")
	}
	if !gi.IsIgnored(".DS_Store") {
		t.Error("expected .DS_Store to be ignored unconditionally")
	}
}

func TestFindFilesIgnored(t *testing.T) {
	tc := setupIgnoreDir(t, "node_modules\n*.log\n", []string{
		"src/main.go",
		"node_modules/dep/index.js",
		"app.log",
		"readme.txt",
	})

	args := mustJSON(t, map[string]any{"pattern": "**"})
	result, err := ExecuteTool(context.Background(), "find_files", args, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result, "node_modules") {
		t.Errorf("find_files should not return node_modules, got:\n%s", result)
	}
	if strings.Contains(result, "app.log") {
		t.Errorf("find_files should not return app.log, got:\n%s", result)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("find_files should return main.go, got:\n%s", result)
	}
}

func TestReadFileIgnored(t *testing.T) {
	tc := setupIgnoreDir(t, "secret.env\n", []string{"secret.env"})

	args := mustJSON(t, map[string]any{"path": "secret.env"})
	result, err := ExecuteTool(context.Background(), "read_file", args, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "in .gitignore and excluded") {
		t.Errorf("expected gitignore error, got:\n%s", result)
	}
}
