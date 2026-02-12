package main

import "testing"

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

func TestSpinnerStopIdempotent(t *testing.T) {
	// Spinner.Stop should be safe to call multiple times
	s := NewSpinner("test", false)
	s.Stop()
	s.Stop() // should not panic
}
