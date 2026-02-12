package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// SubmitAnswerResult is a sentinel error returned when the LLM calls submit_answer.
// The agent loop checks for this with errors.As to extract the raw arguments.
type SubmitAnswerResult struct {
	RawArgs json.RawMessage
}

func (e *SubmitAnswerResult) Error() string {
	return "submit_answer"
}

// safePath resolves a requested path relative to projectDir and ensures it
// stays within the project boundary. Returns a Go error because callers
// wrap it into a string message for the LLM.
func safePath(projectDir, requestedPath string) (string, error) {
	// Resolve projectDir symlinks for consistent prefix checking
	resolvedDir, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		resolvedDir = projectDir
	}

	joined := filepath.Join(resolvedDir, requestedPath)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// File might not exist yet — fall back to abs
		resolved = abs
	}
	if !strings.HasPrefix(resolved, resolvedDir) {
		return "", fmt.Errorf("path '%s' is outside the project directory", requestedPath)
	}
	return resolved, nil
}

// ToolContext carries shared state for tool executors.
type ToolContext struct {
	ProjectDir string
	GitIgnore  *GitIgnore
}

// ExecuteTool dispatches a tool call to the appropriate executor.
// Tool-level failures are returned as string results (first return value).
// Go errors (second return value) mean the harness itself is broken.
func ExecuteTool(ctx context.Context, name string, args json.RawMessage, tc ToolContext) (string, error) {
	switch name {
	case "grep":
		return execGrep(ctx, args, tc.ProjectDir)
	case "find_files":
		return execFindFiles(args, tc)
	case "read_file":
		return execReadFile(args, tc)
	case "list_dir":
		return execListDir(args, tc)
	case "submit_answer":
		return "", &SubmitAnswerResult{RawArgs: args}
	default:
		return fmt.Sprintf("Error: unknown tool '%s'", name), nil
	}
}

// --- execGrep ---

func execGrep(ctx context.Context, args json.RawMessage, projectDir string) (string, error) {
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
	if params.MaxResults <= 0 {
		params.MaxResults = 30
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
	cmdArgs = append(cmdArgs, "--", params.Pattern, projectDir)

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

	// Parse output: make paths relative, format as file:line — content
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		// rg output: /abs/path/file.go:123:content
		// We want: file.go:123 — content
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
	// Format: /abs/path/file.go:123:content
	// Find the first colon after projectDir prefix
	if !strings.HasPrefix(line, projectDir) {
		return "", "", false
	}
	rest := line[len(projectDir):]
	if len(rest) > 0 && rest[0] == filepath.Separator {
		rest = rest[1:]
	}
	// rest is now: file.go:123:content
	// Split on first colon to get file, then on second for line number
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

// --- execFindFiles ---

func execFindFiles(args json.RawMessage, tc ToolContext) (string, error) {
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

		// Filter by type
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

// --- execReadFile ---

func execReadFile(args json.RawMessage, tc ToolContext) (string, error) {
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

	absPath, err := safePath(tc.ProjectDir, params.Path)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
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
	// Remove trailing empty line from Split
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
		// Clamp to file boundaries
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

	const maxLines = 200
	truncated := false
	if !hasRange && totalLines > maxLines {
		end = maxLines
		truncated = true
	}

	var buf strings.Builder
	// Line numbers are 1-based; lines slice is 0-based
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

// --- execListDir ---

func execListDir(args json.RawMessage, tc ToolContext) (string, error) {
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

	absPath, err := safePath(tc.ProjectDir, params.Path)
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

func buildTree(buf *strings.Builder, dir, projectDir string, gi *GitIgnore, indent string, currentDepth, maxDepth int) {
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
