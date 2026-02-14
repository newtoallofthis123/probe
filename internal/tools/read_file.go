package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/newtoallofthis/probe/internal/connector"
	"github.com/newtoallofthis/probe/internal/sandbox"
)

// ReadFileTool reads file contents with line numbers.
type ReadFileTool struct{}

func (r *ReadFileTool) Name() string { return "read_file" }

func (r *ReadFileTool) Schema() connector.ToolSchema {
	return toolSchema("read_file", "Read the contents of a file with line numbers. Paths are relative to the project root.", map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "File path relative to project root.",
		},
		"start_line": map[string]any{
			"type":        "integer",
			"description": "First line to read (1-based, inclusive).",
		},
		"end_line": map[string]any{
			"type":        "integer",
			"description": "Last line to read (1-based, inclusive).",
		},
	}, []string{"path"})
}

func (r *ReadFileTool) Execute(_ context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	var params struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err), nil
	}
	if params.Path == "" {
		return "Error: 'path' is required", nil
	}

	if tc.GitIgnore.IsIgnored(params.Path) {
		return fmt.Sprintf("Error: '%s' is in .gitignore and excluded from search", params.Path), nil
	}

	absPath, err := sandbox.SafePath(tc.ProjectDir, params.Path)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}

	if len(tc.AllowList) > 0 {
		allowed := false
		for _, f := range tc.AllowList {
			if f == absPath {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Sprintf("Error: '%s' is not in the provided file list (--stdin scope)", params.Path), nil
		}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			siblings := listSiblings(filepath.Dir(absPath))
			return fmt.Sprintf("Error: file '%s' not found. Files in same directory: %s", params.Path, siblings), nil
		}
		return fmt.Sprintf("Error reading file: %s", err), nil
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	totalLines := len(lines)

	hasRange := params.StartLine > 0 || params.EndLine > 0
	start := 1
	end := totalLines

	if hasRange {
		if params.StartLine > 0 {
			start = params.StartLine
		}
		if params.EndLine > 0 {
			end = params.EndLine
		}
		if start < 1 {
			start = 1
		}
		if end > totalLines {
			end = totalLines
		}
		if start > totalLines {
			return fmt.Sprintf("Error: start_line %d is beyond end of file (%d lines)", params.StartLine, totalLines), nil
		}
	}

	maxLines := tc.MaxFileReadLines
	if maxLines <= 0 {
		maxLines = 200
	}
	truncated := false
	if !hasRange && totalLines > maxLines {
		end = maxLines
		truncated = true
	}

	var buf strings.Builder
	width := len(strconv.Itoa(end))
	for i := start; i <= end; i++ {
		fmt.Fprintf(&buf, "%*d | %s\n", width, i, lines[i-1])
	}

	if truncated {
		fmt.Fprintf(&buf, "(showing first %d of %d lines — use start_line/end_line to read more)", maxLines, totalLines)
	}

	return buf.String(), nil
}

// listSiblings returns a comma-separated list of files in the given directory.
func listSiblings(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "(unable to list directory)"
	}
	var names []string
	for _, e := range entries {
		if len(names) >= 10 {
			names = append(names, "...")
			break
		}
		names = append(names, e.Name())
	}
	return strings.Join(names, ", ")
}
