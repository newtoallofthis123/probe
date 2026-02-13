package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// SafePath resolves a requested path relative to projectDir and ensures it
// stays within the project boundary. Returns a Go error because callers
// wrap it into a string message for the LLM.
func SafePath(projectDir, requestedPath string) (string, error) {
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

// GitIgnore wraps compiled gitignore rules for path filtering.
type GitIgnore struct {
	matcher *ignore.GitIgnore
}

// LoadGitIgnore reads .gitignore and .git/info/exclude from projectDir,
// combines them with unconditional rules (.git/, .DS_Store), and returns
// a compiled filter. Always returns a usable filter, even if no files exist.
func LoadGitIgnore(projectDir string) *GitIgnore {
	var lines []string

	// Unconditional rules — always ignore these
	lines = append(lines, ".git", ".DS_Store")

	// Read .gitignore
	if data, err := os.ReadFile(filepath.Join(projectDir, ".gitignore")); err == nil {
		lines = append(lines, splitLines(string(data))...)
	}

	// Read .git/info/exclude
	if data, err := os.ReadFile(filepath.Join(projectDir, ".git", "info", "exclude")); err == nil {
		lines = append(lines, splitLines(string(data))...)
	}

	return &GitIgnore{
		matcher: ignore.CompileIgnoreLines(lines...),
	}
}

// IsIgnored returns true if the given path (relative to project root) should
// be excluded from tool results.
func (g *GitIgnore) IsIgnored(path string) bool {
	return g.matcher.MatchesPath(path)
}

// splitLines splits text into lines, filtering out empty lines.
func splitLines(s string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if line != "" {
				result = append(result, line)
			}
			start = i + 1
		}
	}
	return result
}
