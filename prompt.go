package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BuildSystemPrompt generates the full system prompt with project context injected.
func BuildSystemPrompt(tc ToolContext, model string) string {
	if isSmallModel(model) {
		return buildSmallPrompt(tc)
	}
	return buildFullPrompt(tc)
}

func isSmallModel(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "ministral") || strings.Contains(m, "3b") || strings.Contains(m, "1b")
}

func buildSmallPrompt(tc ToolContext) string {
	tree := projectTree(tc.ProjectDir, tc.GitIgnore)
	return fmt.Sprintf(`Search the codebase for files matching the user's query.
Use grep to find files, read_file to verify, then call submit_answer.
You MUST call submit_answer when done — it is the only way to return results.
Be precise with line numbers. Maximum 5 tool calls.

%s`, tree)
}

func buildFullPrompt(tc ToolContext) string {
	tree := projectTree(tc.ProjectDir, tc.GitIgnore)
	langHint := detectLanguage(tc.ProjectDir)
	fileStats := fileStats(tc.ProjectDir, tc.GitIgnore)
	recentFiles := recentGitFiles(tc.ProjectDir)

	var buf strings.Builder
	buf.WriteString(`You are a code search agent. Your job is to find the specific files and
line numbers in a codebase that are relevant to the user's query.

## How you work

You have access to search tools. Use them iteratively to narrow down
the relevant code. Start broad, then refine.

Typical workflow:
1. Look at the project structure to orient yourself
2. Use grep/find to locate candidate files
3. Read the most promising files to confirm relevance
4. Submit your final results with exact line numbers

## Rules

- Be precise. Return specific line ranges, not entire files.
- Be thorough. Check related files — imports, configs, tests.
- Be efficient. Don't read files you can rule out from grep results.
- If grep returns too many results, refine your search pattern.
- If you can't find what the user is asking about, say so honestly.
- NEVER guess line numbers. Always verify by reading the file.

## Project context

`)
	buf.WriteString(tree)
	buf.WriteString("\n\n")
	if langHint != "" {
		buf.WriteString(langHint)
		buf.WriteString("\n\n")
	}
	buf.WriteString(fileStats)
	buf.WriteString("\n")
	if recentFiles != "" {
		buf.WriteString("\n")
		buf.WriteString(recentFiles)
		buf.WriteString("\n")
	}
	buf.WriteString(`
## Output

You MUST call submit_answer when you are done. This is the only way to return results.
If you respond with text instead of calling submit_answer, the user sees nothing.

When you've found relevant code, call submit_answer with:
- Each relevant file, its line range, and why it's relevant
- A brief summary of what you found

If you found nothing relevant, call submit_answer with an empty results array.

Do not explain your search process. Just find the code and submit_answer.`)
	return buf.String()
}

// projectTree generates a directory tree string for the project.
func projectTree(projectDir string, gi *GitIgnore) string {
	// Count total files to decide depth
	totalFiles := 0
	filepath.WalkDir(projectDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(projectDir, path)
		if rel == "." {
			return nil
		}
		if gi.IsIgnored(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			totalFiles++
		}
		return nil
	})

	maxDepth := 2
	if totalFiles > 1000 {
		maxDepth = 1
	} else if totalFiles < 50 {
		maxDepth = 3
	}

	var buf strings.Builder
	name := filepath.Base(projectDir)
	buf.WriteString(name + "/\n")
	lines := buildPromptTree(projectDir, projectDir, gi, 0, maxDepth)

	const maxLines = 100
	if len(lines) > maxLines {
		for _, l := range lines[:maxLines] {
			buf.WriteString(l)
			buf.WriteString("\n")
		}
		buf.WriteString(fmt.Sprintf("... (%d more entries)", len(lines)-maxLines))
	} else {
		for _, l := range lines {
			buf.WriteString(l)
			buf.WriteString("\n")
		}
	}
	return strings.TrimRight(buf.String(), "\n")
}

// buildPromptTree returns formatted lines for the tree.
func buildPromptTree(dir, projectDir string, gi *GitIgnore, depth, maxDepth int) []string {
	if depth >= maxDepth {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	// Filter entries
	var visible []os.DirEntry
	for _, e := range entries {
		rel, _ := filepath.Rel(projectDir, filepath.Join(dir, e.Name()))
		if !gi.IsIgnored(rel) {
			visible = append(visible, e)
		}
	}

	var lines []string
	indent := strings.Repeat("  ", depth)

	for _, e := range visible {
		if e.IsDir() {
			childPath := filepath.Join(dir, e.Name())
			childCount := countVisible(childPath, projectDir, gi)
			if childCount > 10 {
				lines = append(lines, fmt.Sprintf("%s%s/ (%d files)", indent, e.Name(), childCount))
			} else {
				lines = append(lines, fmt.Sprintf("%s%s/", indent, e.Name()))
				lines = append(lines, buildPromptTree(childPath, projectDir, gi, depth+1, maxDepth)...)
			}
		} else {
			lines = append(lines, fmt.Sprintf("%s%s", indent, e.Name()))
		}
	}
	return lines
}

// countVisible counts non-ignored files (not dirs) in a directory.
func countVisible(dir, projectDir string, gi *GitIgnore) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		rel, _ := filepath.Rel(projectDir, filepath.Join(dir, e.Name()))
		if !gi.IsIgnored(rel) {
			count++
		}
	}
	return count
}

// detectLanguage scans the project root for manifest files and returns language hints.
func detectLanguage(projectDir string) string {
	var hints []string

	// Go
	if data, err := os.ReadFile(filepath.Join(projectDir, "go.mod")); err == nil {
		mod := ""
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "module ") {
				mod = strings.TrimPrefix(line, "module ")
				break
			}
		}
		if mod != "" {
			hints = append(hints, fmt.Sprintf("This is a Go project (module: %s).", mod))
		} else {
			hints = append(hints, "This is a Go project.")
		}
	}

	// JavaScript/TypeScript
	if data, err := os.ReadFile(filepath.Join(projectDir, "package.json")); err == nil {
		hint := parsePackageJSON(data, projectDir)
		hints = append(hints, hint)
	}

	// Rust
	if data, err := os.ReadFile(filepath.Join(projectDir, "Cargo.toml")); err == nil {
		pkg := ""
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "name") && strings.Contains(line, "=") {
				parts := strings.SplitN(line, "=", 2)
				pkg = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				break
			}
		}
		if pkg != "" {
			hints = append(hints, fmt.Sprintf("This is a Rust project (package: %s).", pkg))
		} else {
			hints = append(hints, "This is a Rust project.")
		}
	}

	// Python
	for _, f := range []string{"pyproject.toml", "requirements.txt", "setup.py"} {
		if _, err := os.Stat(filepath.Join(projectDir, f)); err == nil {
			hints = append(hints, "This is a Python project.")
			break
		}
	}

	// Java
	for _, f := range []string{"pom.xml", "build.gradle"} {
		if _, err := os.Stat(filepath.Join(projectDir, f)); err == nil {
			hints = append(hints, "This is a Java project.")
			break
		}
	}

	// Ruby
	if _, err := os.Stat(filepath.Join(projectDir, "Gemfile")); err == nil {
		hints = append(hints, "This is a Ruby project.")
	}

	// PHP
	if _, err := os.Stat(filepath.Join(projectDir, "composer.json")); err == nil {
		hints = append(hints, "This is a PHP project.")
	}

	if len(hints) == 0 {
		return "Language not detected from manifest files."
	}
	return strings.Join(hints, " ")
}

func parsePackageJSON(data []byte, projectDir string) string {
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	json.Unmarshal(data, &pkg)

	// Detect TypeScript
	isTS := false
	if _, ok := pkg.Dependencies["typescript"]; ok {
		isTS = true
	}
	if !isTS {
		if _, err := os.Stat(filepath.Join(projectDir, "tsconfig.json")); err == nil {
			isTS = true
		}
	}

	lang := "JavaScript"
	if isTS {
		lang = "TypeScript"
	}

	// Top 3 deps
	var deps []string
	for name := range pkg.Dependencies {
		if name == "typescript" {
			continue
		}
		deps = append(deps, name)
		if len(deps) >= 3 {
			break
		}
	}

	if len(deps) > 0 {
		return fmt.Sprintf("This is a %s project using %s.", lang, strings.Join(deps, ", "))
	}
	return fmt.Sprintf("This is a %s project.", lang)
}

// fileStats returns a string with file and directory counts.
func fileStats(projectDir string, gi *GitIgnore) string {
	files, dirs := 0, 0
	filepath.WalkDir(projectDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(projectDir, path)
		if rel == "." {
			return nil
		}
		if gi.IsIgnored(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			dirs++
		} else {
			files++
		}
		return nil
	})
	return fmt.Sprintf("This repository contains %d files across %d directories.", files, dirs)
}

// recentGitFiles returns recently modified files from git log, or empty string on failure.
func recentGitFiles(projectDir string) string {
	cmd := exec.Command("git", "log", "--name-only", "--format=", "-10")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	seen := make(map[string]bool)
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		files = append(files, line)
		if len(files) >= 20 {
			break
		}
	}

	if len(files) == 0 {
		return ""
	}
	return "Recently modified files: " + strings.Join(files, ", ")
}
