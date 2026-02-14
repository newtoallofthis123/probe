package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/newtoallofthis/probe/internal/sandbox"
	"github.com/openai/openai-go"
)

// TreeTool lists directory contents as a tree.
type TreeTool struct{}

func (t *TreeTool) Name() string { return "tree" }

func (t *TreeTool) Schema() openai.ChatCompletionToolParam {
	return toolParam("tree", "List directory contents as a tree. Returns indented output with files and subdirectories.", map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "Directory path relative to project root (default: \".\").",
		},
		"depth": map[string]any{
			"type":        "integer",
			"description": "Maximum depth to recurse (default 2).",
		},
	}, nil)
}

func (t *TreeTool) Execute(_ context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	var params struct {
		Path  string `json:"path"`
		Depth int    `json:"depth"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err), nil
	}
	if params.Path == "" {
		params.Path = "."
	}
	if params.Depth <= 0 {
		params.Depth = 2
	}

	absPath, err := sandbox.SafePath(tc.ProjectDir, params.Path)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("Error: path '%s' not found", params.Path), nil
	}
	if !info.IsDir() {
		return fmt.Sprintf("Error: '%s' is not a directory", params.Path), nil
	}

	var buf strings.Builder
	buildTree(&buf, absPath, tc.ProjectDir, tc.GitIgnore, "", 0, params.Depth)
	return buf.String(), nil
}

func buildTree(buf *strings.Builder, dir, projectDir string, gi *sandbox.GitIgnore, indent string, currentDepth, maxDepth int) {
	if currentDepth >= maxDepth {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		rel, _ := filepath.Rel(projectDir, filepath.Join(dir, e.Name()))
		if gi.IsIgnored(rel) {
			continue
		}

		if e.IsDir() {
			childPath := filepath.Join(dir, e.Name())
			children, _ := os.ReadDir(childPath)
			childCount := 0
			for _, c := range children {
				cRel, _ := filepath.Rel(projectDir, filepath.Join(childPath, c.Name()))
				if !gi.IsIgnored(cRel) {
					childCount++
				}
			}

			fmt.Fprintf(buf, "%s%s/\n", indent, e.Name())
			if childCount > 10 && currentDepth+1 >= maxDepth {
				fmt.Fprintf(buf, "%s  (%d entries)\n", indent, childCount)
			} else {
				buildTree(buf, childPath, projectDir, gi, indent+"  ", currentDepth+1, maxDepth)
			}
		} else {
			fmt.Fprintf(buf, "%s%s\n", indent, e.Name())
		}
	}
}
