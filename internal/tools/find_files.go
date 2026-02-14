package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/openai/openai-go"
)

// FindFilesTool finds files and directories by glob pattern.
type FindFilesTool struct{}

func (f *FindFilesTool) Name() string { return "find_files" }

func (f *FindFilesTool) Schema() openai.ChatCompletionToolParam {
	return toolParam("find_files", "Find files and directories by glob pattern. Returns paths relative to project root.", map[string]any{
		"pattern": map[string]any{
			"type":        "string",
			"description": "Glob pattern to match (supports ** for recursive matching).",
		},
		"type": map[string]any{
			"type":        "string",
			"enum":        []string{"file", "dir"},
			"description": "Filter by entry type: 'file' or 'dir'.",
		},
	}, []string{"pattern"})
}

func (f *FindFilesTool) Execute(_ context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Type    string `json:"type"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err), nil
	}
	if params.Pattern == "" {
		return "Error: 'pattern' is required", nil
	}

	const maxResults = 100
	var matches []string

	var allowSet map[string]bool
	if len(tc.AllowList) > 0 {
		allowSet = make(map[string]bool, len(tc.AllowList))
		for _, af := range tc.AllowList {
			allowSet[af] = true
		}
	}

	filepath.WalkDir(tc.ProjectDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(tc.ProjectDir, path)
		if rel == "." {
			return nil
		}

		if tc.GitIgnore.IsIgnored(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if allowSet != nil && !d.IsDir() && !allowSet[path] {
			return nil
		}

		if params.Type == "file" && d.IsDir() {
			return nil
		}
		if params.Type == "dir" && !d.IsDir() {
			return nil
		}

		matched, _ := doublestar.Match(params.Pattern, rel)
		if matched {
			suffix := ""
			if d.IsDir() {
				suffix = "/"
			}
			matches = append(matches, rel+suffix)
			if len(matches) >= maxResults+1 {
				return filepath.SkipAll
			}
		}
		return nil
	})

	if len(matches) == 0 {
		return fmt.Sprintf("No files found matching '%s'", params.Pattern), nil
	}

	truncated := len(matches) > maxResults
	if truncated {
		matches = matches[:maxResults]
	}

	result := strings.Join(matches, "\n")
	if truncated {
		result += fmt.Sprintf("\n(showing %d results — refine your pattern)", maxResults)
	}
	return result, nil
}
