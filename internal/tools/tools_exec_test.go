package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/newtoallofthis/probe/internal/sandbox"
)

// toolContext creates a temp dir with fixture files for testing.
func toolContext(t *testing.T) ToolContext {
	t.Helper()
	dir := t.TempDir()

	// Create fixture files
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "config.go"), []byte("package main\n\ntype Config struct{}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "tools_exec.go"), []byte("package main\n"), 0644)

	return ToolContext{
		ProjectDir: dir,
		GitIgnore:  sandbox.LoadGitIgnore(dir),
	}
}

func TestGrepExecutor(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not on PATH")
	}
	tc := toolContext(t)
	args := mustJSON(t, map[string]any{"pattern": "func main"})
	result, err := ExecuteTool(context.Background(), "grep", args, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected result to contain main.go, got:\n%s", result)
	}
}

func TestFindFilesExecutor(t *testing.T) {
	tc := toolContext(t)
	args := mustJSON(t, map[string]any{"pattern": "*.go"})
	result, err := ExecuteTool(context.Background(), "find_files", args, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected main.go in results, got:\n%s", result)
	}
	if !strings.Contains(result, "config.go") {
		t.Errorf("expected config.go in results, got:\n%s", result)
	}
}

func TestReadFileExecutor(t *testing.T) {
	tc := toolContext(t)
	args := mustJSON(t, map[string]any{"path": "main.go", "start_line": 1, "end_line": 3})
	result, err := ExecuteTool(context.Background(), "read_file", args, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "package main") {
		t.Errorf("expected 'package main' in output, got:\n%s", result)
	}
	if !strings.Contains(result, " | ") {
		t.Errorf("expected line number formatting, got:\n%s", result)
	}
}

func TestReadFileSandbox(t *testing.T) {
	tc := toolContext(t)
	args := mustJSON(t, map[string]any{"path": "../../etc/passwd"})
	result, err := ExecuteTool(context.Background(), "read_file", args, tc)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if !strings.Contains(result, "outside the project directory") {
		t.Errorf("expected sandbox error, got:\n%s", result)
	}
}

func TestReadFileNotFound(t *testing.T) {
	tc := toolContext(t)
	args := mustJSON(t, map[string]any{"path": "nonexistent.go"})
	result, err := ExecuteTool(context.Background(), "read_file", args, tc)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found' in error, got:\n%s", result)
	}
	// Should list siblings from the fixture dir
	if !strings.Contains(result, "config.go") {
		t.Errorf("expected sibling files listed, got:\n%s", result)
	}
}

func TestReadFileTruncation(t *testing.T) {
	dir := t.TempDir()
	var content strings.Builder
	for i := 0; i < 250; i++ {
		content.WriteString("line content\n")
	}
	os.WriteFile(filepath.Join(dir, "big.txt"), []byte(content.String()), 0644)

	tc := ToolContext{ProjectDir: dir, GitIgnore: sandbox.LoadGitIgnore(dir)}
	args := mustJSON(t, map[string]any{"path": "big.txt"})
	result, err := ExecuteTool(context.Background(), "read_file", args, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "showing first 200 of 250 lines") {
		t.Errorf("expected truncation notice, got:\n%s", result)
	}
}

func TestListDirExecutor(t *testing.T) {
	tc := toolContext(t)
	args := mustJSON(t, map[string]any{"depth": 1})
	result, err := ExecuteTool(context.Background(), "list_dir", args, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected main.go in listing, got:\n%s", result)
	}
}

func TestExecuteToolUnknown(t *testing.T) {
	tc := ToolContext{ProjectDir: "/tmp", GitIgnore: sandbox.LoadGitIgnore("/tmp")}
	result, err := ExecuteTool(context.Background(), "bogus", nil, tc)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if !strings.Contains(result, "unknown tool 'bogus'") {
		t.Errorf("expected unknown tool error, got: %s", result)
	}
}

func TestSubmitAnswer(t *testing.T) {
	args := mustJSON(t, map[string]any{
		"results": []map[string]any{{"file": "main.go", "start_line": 1, "end_line": 10, "reason": "test"}},
		"summary": "test summary",
	})
	tc := ToolContext{ProjectDir: "/tmp", GitIgnore: sandbox.LoadGitIgnore("/tmp")}
	_, err := ExecuteTool(context.Background(), "submit_answer", args, tc)
	var submit *SubmitAnswerResult
	if !errors.As(err, &submit) {
		t.Fatalf("expected SubmitAnswerResult error, got: %v", err)
	}
}

// --- AllowList (--stdin) tests ---

func TestGrepRespectsAllowList(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not on PATH")
	}
	tc := toolContext(t)
	mainResolved, _ := filepath.EvalSymlinks(filepath.Join(tc.ProjectDir, "main.go"))
	configResolved, _ := filepath.EvalSymlinks(filepath.Join(tc.ProjectDir, "config.go"))
	tc.AllowList = []string{mainResolved, configResolved}

	args := mustJSON(t, map[string]any{"pattern": "package main"})
	result, err := ExecuteTool(context.Background(), "grep", args, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected main.go in results, got:\n%s", result)
	}
	if strings.Contains(result, "tools_exec.go") {
		t.Errorf("grep should be scoped to allowList, but found tools_exec.go")
	}
}

func TestFindFilesRespectsAllowList(t *testing.T) {
	tc := toolContext(t)
	// Use unresolved path to match WalkDir output
	tc.AllowList = []string{filepath.Join(tc.ProjectDir, "main.go")}

	args := mustJSON(t, map[string]any{"pattern": "*.go"})
	result, err := ExecuteTool(context.Background(), "find_files", args, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected main.go, got:\n%s", result)
	}
	if strings.Contains(result, "config.go") {
		t.Errorf("find_files should be scoped to allowList, but found config.go")
	}
}

func TestReadFileRespectsAllowList(t *testing.T) {
	tc := toolContext(t)
	// Resolve through symlinks (macOS /var → /private/var) to match SafePath
	mainResolved, _ := filepath.EvalSymlinks(filepath.Join(tc.ProjectDir, "main.go"))
	tc.AllowList = []string{mainResolved}

	args := mustJSON(t, map[string]any{"path": "main.go", "start_line": 1, "end_line": 3})
	result, err := ExecuteTool(context.Background(), "read_file", args, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "package main") {
		t.Errorf("expected file content, got:\n%s", result)
	}

	args2 := mustJSON(t, map[string]any{"path": "config.go"})
	result2, err := ExecuteTool(context.Background(), "read_file", args2, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result2, "not in the provided file list") {
		t.Errorf("expected allowList rejection, got:\n%s", result2)
	}
}

// --- Gitignore integration tests ---

func setupIgnoreDir(t *testing.T, gitignoreContent string, files []string) ToolContext {
	t.Helper()
	dir := t.TempDir()

	if gitignoreContent != "" {
		os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignoreContent), 0644)
	}

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
		GitIgnore:  sandbox.LoadGitIgnore(dir),
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

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
