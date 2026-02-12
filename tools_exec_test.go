package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func projectDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGrepExecutor(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not on PATH")
	}
	dir := projectDir(t)
	args := mustJSON(t, map[string]any{"pattern": "func main"})
	result, err := ExecuteTool(context.Background(), "grep", args, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected result to contain main.go, got:\n%s", result)
	}
}

func TestFindFilesExecutor(t *testing.T) {
	dir := projectDir(t)
	args := mustJSON(t, map[string]any{"pattern": "*.go"})
	result, err := ExecuteTool(context.Background(), "find_files", args, dir)
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
	dir := projectDir(t)
	args := mustJSON(t, map[string]any{"path": "main.go", "start_line": 1, "end_line": 10})
	result, err := ExecuteTool(context.Background(), "read_file", args, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "package main") {
		t.Errorf("expected 'package main' in output, got:\n%s", result)
	}
	// Verify line numbers present
	if !strings.Contains(result, " | ") {
		t.Errorf("expected line number formatting, got:\n%s", result)
	}
}

func TestReadFileSandbox(t *testing.T) {
	dir := projectDir(t)
	args := mustJSON(t, map[string]any{"path": "../../etc/passwd"})
	result, err := ExecuteTool(context.Background(), "read_file", args, dir)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if !strings.Contains(result, "outside the project directory") {
		t.Errorf("expected sandbox error, got:\n%s", result)
	}
}

func TestReadFileNotFound(t *testing.T) {
	dir := projectDir(t)
	args := mustJSON(t, map[string]any{"path": "nonexistent.go"})
	result, err := ExecuteTool(context.Background(), "read_file", args, dir)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found' in error, got:\n%s", result)
	}
	// Should list sibling files
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected sibling files listed, got:\n%s", result)
	}
}

func TestReadFileTruncation(t *testing.T) {
	// Create a temp file with >200 lines
	dir := t.TempDir()
	var content strings.Builder
	for i := 0; i < 250; i++ {
		content.WriteString("line content\n")
	}
	os.WriteFile(filepath.Join(dir, "big.txt"), []byte(content.String()), 0644)

	args := mustJSON(t, map[string]any{"path": "big.txt"})
	result, err := ExecuteTool(context.Background(), "read_file", args, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "showing first 200 of 250 lines") {
		t.Errorf("expected truncation notice, got:\n%s", result)
	}
}

func TestListDirExecutor(t *testing.T) {
	dir := projectDir(t)
	args := mustJSON(t, map[string]any{"depth": 1})
	result, err := ExecuteTool(context.Background(), "list_dir", args, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected main.go in listing, got:\n%s", result)
	}
}

func TestExecuteToolUnknown(t *testing.T) {
	result, err := ExecuteTool(context.Background(), "bogus", nil, "/tmp")
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
	_, err := ExecuteTool(context.Background(), "submit_answer", args, "/tmp")
	var submit *SubmitAnswerResult
	if !errors.As(err, &submit) {
		t.Fatalf("expected SubmitAnswerResult error, got: %v", err)
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
