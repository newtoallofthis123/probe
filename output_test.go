package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSummarizeToolCall(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args string
		want string
	}{
		{"grep", "grep", `{"pattern":"func main","glob":"**/*.go"}`, `grep func main`},
		{"find", "find_files", `{"pattern":"*.go"}`, `find *.go`},
		{"read full", "read_file", `{"path":"main.go"}`, `read main.go`},
		{"read range", "read_file", `{"path":"main.go","start_line":"1","end_line":"30"}`, `read main.go:1-30`},
		{"list_dir", "list_dir", `{"path":"src"}`, `ls src`},
		{"submit_answer", "submit_answer", `{}`, `submit_answer`},
		{"unknown", "some_tool", `{}`, `some_tool`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeToolCall(tt.tool, tt.args)
			if got != tt.want {
				t.Errorf("summarizeToolCall(%q, %q) = %q, want %q", tt.tool, tt.args, got, tt.want)
			}
		})
	}
}

func TestSummarizeToolResult(t *testing.T) {
	tests := []struct {
		name   string
		tool   string
		result string
		want   string
	}{
		{"empty", "grep", "", "empty"},
		{"grep matches", "grep", "main.go:1:func main()\nmain.go:5:func foo()", "2 matches"},
		{"find files", "find_files", "a.go\nb.go\nc.go", "3 files"},
		{"read lines", "read_file", "line1\nline2\nline3\nline4", "4 lines"},
		{"list entries", "list_dir", "a/\nb/\nc.go", "3 entries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeToolResult(tt.tool, tt.result)
			if got != tt.want {
				t.Errorf("summarizeToolResult(%q, ...) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}

func TestExtractJSONField(t *testing.T) {
	tests := []struct {
		name  string
		json  string
		field string
		want  string
	}{
		{"string field", `{"pattern":"func main","glob":"*.go"}`, "pattern", "func main"},
		{"missing field", `{"pattern":"x"}`, "nope", ""},
		{"nested quotes", `{"path":"a/b/c.go"}`, "path", "a/b/c.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONField(tt.json, tt.field)
			if got != tt.want {
				t.Errorf("extractJSONField(%q, %q) = %q, want %q", tt.json, tt.field, got, tt.want)
			}
		})
	}
}

func TestProgressShouldShow(t *testing.T) {
	// Quiet always suppresses
	p := &Progress{quiet: true, isTTY: true}
	if p.shouldShow() {
		t.Error("quiet mode should suppress output")
	}

	// Non-TTY, non-verbose suppresses
	p = &Progress{quiet: false, isTTY: false, verbose: false}
	if p.shouldShow() {
		t.Error("non-TTY non-verbose should suppress")
	}

	// TTY shows
	p = &Progress{quiet: false, isTTY: true}
	if !p.shouldShow() {
		t.Error("TTY should show")
	}

	// Verbose forces show
	p = &Progress{quiet: false, isTTY: false, verbose: true}
	if !p.shouldShow() {
		t.Error("verbose should force show")
	}
}

func TestFormatResultsHumanTTY(t *testing.T) {
	result := &AgentResult{
		Results: []SearchResult{
			{File: "main.go", StartLine: 1, EndLine: 10, Reason: "entry point"},
			{File: "agent.go", StartLine: 5, EndLine: 20, Reason: "agent loop"},
		},
		Summary: "found stuff",
		Turns:   2,
	}
	out := FormatResults(result, "human", true, false)
	if !strings.Contains(out, "main.go:1-10") {
		t.Errorf("expected main.go:1-10, got %q", out)
	}
	if !strings.Contains(out, "entry point") {
		t.Errorf("expected reason in TTY output")
	}
}

func TestFormatResultsHumanPipe(t *testing.T) {
	result := &AgentResult{
		Results: []SearchResult{
			{File: "main.go", StartLine: 1, EndLine: 10, Reason: "entry point"},
		},
	}
	out := FormatResults(result, "human", false, false)
	if out != "main.go:1-10\n" {
		t.Errorf("pipe output should be path:lines only, got %q", out)
	}
	if strings.Contains(out, "entry point") {
		t.Error("pipe output should not contain reasons")
	}
}

func TestFormatResultsJSON(t *testing.T) {
	result := &AgentResult{
		Results: []SearchResult{
			{File: "main.go", StartLine: 1, EndLine: 10, Reason: "test"},
		},
		Summary: "found",
		Turns:   1,
	}
	// Pretty (TTY)
	out := FormatResults(result, "json", true, false)
	var parsed AgentResult
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("TTY JSON not valid: %v", err)
	}
	if len(parsed.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(parsed.Results))
	}

	// Compact (pipe)
	out = FormatResults(result, "json", false, false)
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("pipe JSON not valid: %v", err)
	}
	if strings.Contains(out, "\n  ") {
		t.Error("pipe JSON should be compact")
	}
}

func TestFormatResultsJSONEmpty(t *testing.T) {
	result := &AgentResult{
		Results: nil,
		Summary: "No relevant code found",
		Turns:   3,
	}
	out := FormatResults(result, "json", false, false)
	var parsed AgentResult
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("empty JSON not valid: %v", err)
	}
	if parsed.Results != nil {
		t.Errorf("expected null results in empty case")
	}
}

func TestFormatResultsPaths(t *testing.T) {
	result := &AgentResult{
		Results: []SearchResult{
			{File: "main.go", StartLine: 1, EndLine: 10, Reason: "a"},
			{File: "main.go", StartLine: 20, EndLine: 30, Reason: "b"},
			{File: "agent.go", StartLine: 5, EndLine: 15, Reason: "c"},
		},
	}
	out := FormatResults(result, "paths", false, false)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 deduplicated paths, got %d: %v", len(lines), lines)
	}
	if lines[0] != "main.go" || lines[1] != "agent.go" {
		t.Errorf("unexpected paths: %v", lines)
	}
}

func TestFormatResultsHumanColor(t *testing.T) {
	result := &AgentResult{
		Results: []SearchResult{
			{File: "main.go", StartLine: 1, EndLine: 10, Reason: "test"},
		},
	}
	out := FormatResults(result, "human", true, true)
	if !strings.Contains(out, "\033[1;36m") {
		t.Error("expected ANSI color codes in colored output")
	}
}

func TestFormatResultsEmpty(t *testing.T) {
	result := &AgentResult{Results: nil}
	out := FormatResults(result, "human", true, true)
	if out != "" {
		t.Errorf("expected empty string for no results in human format, got %q", out)
	}
}

func TestSpinnerStopIdempotent(t *testing.T) {
	// Spinner.Stop should be safe to call multiple times
	s := NewSpinner("test", false)
	s.Stop()
	s.Stop() // should not panic
}
