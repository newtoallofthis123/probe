package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/newtoallofthis/probe/internal/connector"
)

// GrepTool searches file contents using ripgrep.
type GrepTool struct{}

func (g *GrepTool) Name() string { return "grep" }

func (g *GrepTool) Schema() connector.ToolSchema {
	return toolSchema("grep", "Search file contents using ripgrep. Returns matching lines with file paths and line numbers.", map[string]any{
		"pattern": map[string]any{
			"type":        "string",
			"description": "Regular expression pattern to search for.",
		},
		"glob": map[string]any{
			"type":        "string",
			"description": "File glob to restrict search (e.g. \"*.go\", \"src/**/*.ts\").",
		},
		"max_results": map[string]any{
			"type":        "integer",
			"description": "Maximum number of matches to return (default 30, max 100).",
		},
	}, []string{"pattern"})
}

func (g *GrepTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	projectDir := tc.ProjectDir
	var params struct {
		Pattern    string `json:"pattern"`
		Glob       string `json:"glob"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err), nil
	}
	if params.Pattern == "" {
		return "Error: 'pattern' is required", nil
	}
	limit := tc.MaxResultsPerGrep
	if limit <= 0 {
		limit = 30
	}
	if params.MaxResults <= 0 {
		params.MaxResults = limit
	}
	if params.MaxResults > 100 {
		params.MaxResults = 100
	}

	cmdArgs := []string{
		"--line-number", "--no-heading", "--color", "never",
		"--max-count", strconv.Itoa(params.MaxResults),
	}
	if params.Glob != "" {
		cmdArgs = append(cmdArgs, "--glob", params.Glob)
	}

	cmdArgs = append(cmdArgs, "--", params.Pattern)
	if len(tc.AllowList) > 0 {
		cmdArgs = append(cmdArgs, tc.AllowList...)
	} else {
		cmdArgs = append(cmdArgs, projectDir)
	}

	cmd := exec.CommandContext(ctx, "rg", cmdArgs...)
	out, err := cmd.Output()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			switch exitErr.ExitCode() {
			case 1:
				return fmt.Sprintf("No matches found for pattern '%s'", params.Pattern), nil
			case 2:
				return fmt.Sprintf("Error: %s", strings.TrimSpace(string(exitErr.Stderr))), nil
			}
		}
		return fmt.Sprintf("Error running grep: %s", err), nil
	}

	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if rel, after, ok := splitRgLine(line, projectDir); ok {
			lines = append(lines, rel+" — "+after)
		} else {
			lines = append(lines, line)
		}
	}

	total := len(lines)
	if total > params.MaxResults {
		lines = lines[:params.MaxResults]
	}

	result := strings.Join(lines, "\n")
	if total > params.MaxResults {
		result += fmt.Sprintf("\n(showing %d of %d matches — refine your search)", params.MaxResults, total)
	}
	return result, nil
}

// splitRgLine splits a ripgrep output line into relative path:line and content.
func splitRgLine(line, projectDir string) (pathLine string, content string, ok bool) {
	if !strings.HasPrefix(line, projectDir) {
		return "", "", false
	}
	rest := line[len(projectDir):]
	if len(rest) > 0 && rest[0] == filepath.Separator {
		rest = rest[1:]
	}
	firstColon := strings.Index(rest, ":")
	if firstColon < 0 {
		return "", "", false
	}
	afterFile := rest[firstColon+1:]
	secondColon := strings.Index(afterFile, ":")
	if secondColon < 0 {
		return rest[:firstColon] + ":" + afterFile, "", true
	}
	pathLine = rest[:firstColon] + ":" + afterFile[:secondColon]
	content = afterFile[secondColon+1:]
	return pathLine, content, true
}
