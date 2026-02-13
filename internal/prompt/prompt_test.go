package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/newtoallofthis/probe/internal/sandbox"
	"github.com/newtoallofthis/probe/internal/tools"
)

func TestBuildSystemPrompt_Fast(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "cmd"), 0o755)
	os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644)

	gi := sandbox.LoadGitIgnore(dir)
	tc := tools.ToolContext{ProjectDir: dir, GitIgnore: gi}

	prompt := BuildSystemPrompt(tc, "gpt-4", false)
	if !strings.Contains(prompt, "code search agent") {
		t.Error("fast prompt should contain 'code search agent'")
	}
	if strings.Contains(prompt, "Start broad") {
		t.Error("fast prompt should NOT contain 'Start broad'")
	}
	if strings.Contains(prompt, "Be thorough") {
		t.Error("fast prompt should NOT contain 'Be thorough'")
	}
	if !strings.Contains(prompt, "Submit early") {
		t.Error("fast prompt should contain 'Submit early'")
	}
	if !strings.Contains(prompt, "Go project") {
		t.Error("should detect Go project")
	}
	if !strings.Contains(prompt, "submit_answer") {
		t.Error("should mention submit_answer")
	}
}

func TestBuildSystemPrompt_Think(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "cmd"), 0o755)
	os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0o644)

	gi := sandbox.LoadGitIgnore(dir)
	tc := tools.ToolContext{ProjectDir: dir, GitIgnore: gi}

	prompt := BuildSystemPrompt(tc, "gpt-4", true)
	if !strings.Contains(prompt, "Start broad") {
		t.Error("think prompt should contain 'Start broad'")
	}
	if !strings.Contains(prompt, "Be thorough") {
		t.Error("think prompt should contain 'Be thorough'")
	}
}

func TestBuildSystemPrompt_Small(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)

	gi := sandbox.LoadGitIgnore(dir)
	tc := tools.ToolContext{ProjectDir: dir, GitIgnore: gi}

	prompt := BuildSystemPrompt(tc, "ministral-3:3b", false)
	if !strings.Contains(prompt, "Submit early") {
		t.Error("small model prompt should contain 'Submit early'")
	}
	if strings.Contains(prompt, "code search agent") {
		t.Error("small model prompt should NOT contain full prompt text")
	}
}

func TestIsSmallModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"ministral-3:3b", true},
		{"Ministral-8B", true},
		{"qwen3:1b", true},
		{"gpt-4", false},
		{"claude-3-sonnet", false},
		{"llama3:3b", true},
	}
	for _, tt := range tests {
		if got := isSmallModel(tt.model); got != tt.want {
			t.Errorf("isSmallModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		wantSub string
	}{
		{
			name:    "go project",
			files:   map[string]string{"go.mod": "module example.com/foo\n"},
			wantSub: "Go project (module: example.com/foo)",
		},
		{
			name:    "python project",
			files:   map[string]string{"requirements.txt": "flask\n"},
			wantSub: "Python project",
		},
		{
			name:    "typescript project",
			files:   map[string]string{"package.json": `{"dependencies":{"typescript":"^5.0","express":"^4.0"}}`, "tsconfig.json": "{}"},
			wantSub: "TypeScript project",
		},
		{
			name:    "javascript project",
			files:   map[string]string{"package.json": `{"dependencies":{"react":"^18.0"}}`},
			wantSub: "JavaScript project",
		},
		{
			name:    "rust project",
			files:   map[string]string{"Cargo.toml": "[package]\nname = \"myapp\"\n"},
			wantSub: "Rust project (package: myapp)",
		},
		{
			name:    "java project",
			files:   map[string]string{"pom.xml": "<project></project>"},
			wantSub: "Java project",
		},
		{
			name:    "ruby project",
			files:   map[string]string{"Gemfile": "source 'https://rubygems.org'\n"},
			wantSub: "Ruby project",
		},
		{
			name:    "php project",
			files:   map[string]string{"composer.json": "{}"},
			wantSub: "PHP project",
		},
		{
			name:    "no manifest",
			files:   map[string]string{"README.md": "hello"},
			wantSub: "Language not detected",
		},
		{
			name:    "multiple languages",
			files:   map[string]string{"go.mod": "module test\n", "requirements.txt": "flask\n"},
			wantSub: "Go project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
			}
			got := detectLanguage(dir)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("detectLanguage() = %q, want substring %q", got, tt.wantSub)
			}
		})
	}
}

func TestProjectTree(t *testing.T) {
	dir := t.TempDir()
	// Create structure
	os.MkdirAll(filepath.Join(dir, "src", "auth"), 0o755)
	os.MkdirAll(filepath.Join(dir, "src", "models"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "auth", "login.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "models", "user.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(""), 0o644)

	gi := sandbox.LoadGitIgnore(dir)
	tree := projectTree(dir, gi)

	if strings.Contains(tree, ".git") {
		t.Error("tree should not contain .git")
	}
	if !strings.Contains(tree, "src/") {
		t.Error("tree should contain src/")
	}
	if !strings.Contains(tree, "README.md") {
		t.Error("tree should contain README.md")
	}
}

func TestProjectTree_Truncation(t *testing.T) {
	dir := t.TempDir()
	// Create >100 entries to trigger truncation
	for i := 0; i < 120; i++ {
		os.WriteFile(filepath.Join(dir, strings.Repeat("a", 5)+string(rune('A'+i/26))+string(rune('a'+i%26))+".txt"), []byte(""), 0o644)
	}

	gi := sandbox.LoadGitIgnore(dir)
	tree := projectTree(dir, gi)

	if !strings.Contains(tree, "more entries") {
		t.Error("tree should show truncation message for >100 entries")
	}
}

func TestProjectTree_LargeDirCollapse(t *testing.T) {
	dir := t.TempDir()
	bigDir := filepath.Join(dir, "vendor")
	os.MkdirAll(bigDir, 0o755)
	// Create >10 files in vendor/
	for i := 0; i < 15; i++ {
		os.WriteFile(filepath.Join(bigDir, strings.Repeat("x", 3)+string(rune('a'+i))+".go"), []byte(""), 0o644)
	}

	gi := sandbox.LoadGitIgnore(dir)
	tree := projectTree(dir, gi)

	if !strings.Contains(tree, "15 files") {
		t.Errorf("large dir should show file count, got:\n%s", tree)
	}
}

func TestBuildSystemPrompt_AllowList(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "config.go"), []byte("package main"), 0o644)

	gi := sandbox.LoadGitIgnore(dir)
	tc := tools.ToolContext{
		ProjectDir: dir,
		GitIgnore:  gi,
		AllowList:  []string{filepath.Join(dir, "main.go"), filepath.Join(dir, "config.go")},
	}

	prompt := BuildSystemPrompt(tc, "gpt-4", false)
	if !strings.Contains(prompt, "Search scope") {
		t.Error("prompt should contain 'Search scope' when AllowList is set")
	}
	if !strings.Contains(prompt, "main.go") {
		t.Error("prompt should list main.go")
	}
	if !strings.Contains(prompt, "config.go") {
		t.Error("prompt should list config.go")
	}
	if !strings.Contains(prompt, "Only search within these files") {
		t.Error("prompt should instruct LLM to stay within scope")
	}
}

func TestBuildSystemPrompt_AllowListTruncation(t *testing.T) {
	dir := t.TempDir()
	var allowList []string
	for i := 0; i < 60; i++ {
		name := filepath.Join(dir, strings.Repeat("f", 3)+string(rune('A'+i/26))+string(rune('a'+i%26))+".go")
		os.WriteFile(name, []byte("package main"), 0o644)
		allowList = append(allowList, name)
	}

	gi := sandbox.LoadGitIgnore(dir)
	tc := tools.ToolContext{ProjectDir: dir, GitIgnore: gi, AllowList: allowList}
	prompt := BuildSystemPrompt(tc, "gpt-4", false)
	if !strings.Contains(prompt, "and 10 more files") {
		t.Errorf("expected truncation at 50 files, got prompt without truncation message")
	}
}

func TestFileStats(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "app.go"), []byte(""), 0o644)

	gi := sandbox.LoadGitIgnore(dir)
	stats := fileStats(dir, gi)

	if !strings.Contains(stats, "2 files") {
		t.Errorf("expected 2 files, got: %s", stats)
	}
	if !strings.Contains(stats, "1 directories") {
		t.Errorf("expected 1 directory, got: %s", stats)
	}
}
