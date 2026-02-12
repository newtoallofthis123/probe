package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Progress handles all stderr output during an agent run.
// It respects TTY detection, --verbose, and --quiet flags.
type Progress struct {
	verbose bool
	quiet   bool
	isTTY   bool
	start   time.Time
	mu      sync.Mutex
	spinner *Spinner
}

// NewProgress creates a progress reporter based on config flags.
func NewProgress(cfg *Config) *Progress {
	stderrTTY := term.IsTerminal(int(os.Stderr.Fd()))
	return &Progress{
		verbose: cfg.Verbose,
		quiet:   cfg.Quiet,
		isTTY:   stderrTTY,
		start:   time.Now(),
	}
}

// shouldShow returns true if progress output should be displayed.
func (p *Progress) shouldShow() bool {
	if p.quiet {
		return false
	}
	return p.isTTY || p.verbose
}

// StartSpinner begins the spinner animation with a message.
func (p *Progress) StartSpinner(msg string) {
	if !p.shouldShow() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.spinner != nil {
		p.spinner.Stop()
	}
	p.spinner = NewSpinner(msg, p.isTTY)
}

// StopSpinner stops the current spinner.
func (p *Progress) StopSpinner() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.spinner != nil {
		p.spinner.Stop()
		p.spinner = nil
	}
}

// OnToolCall prints a tool invocation summary to stderr.
func (p *Progress) OnToolCall(name string, args string) {
	if !p.shouldShow() {
		return
	}
	p.StopSpinner()
	summary := summarizeToolCall(name, args)
	fmt.Fprintf(os.Stderr, "├─ %s\n", summary)
	if p.verbose {
		// Print full args indented
		for _, line := range strings.Split(args, "\n") {
			fmt.Fprintf(os.Stderr, "    %s\n", line)
		}
	}
}

// OnToolResult prints a one-line tool result summary to stderr.
func (p *Progress) OnToolResult(name string, result string) {
	if !p.shouldShow() {
		return
	}
	summary := summarizeToolResult(name, result)
	fmt.Fprintf(os.Stderr, "│  → %s\n", summary)
	if p.verbose && result != "" {
		lines := strings.Split(result, "\n")
		limit := 20
		for i, line := range lines {
			if i >= limit {
				fmt.Fprintf(os.Stderr, "    ... (%d more lines)\n", len(lines)-limit)
				break
			}
			fmt.Fprintf(os.Stderr, "    %s\n", line)
		}
	}
}

// OnTokenUsage prints token usage in verbose mode.
func (p *Progress) OnTokenUsage(input, output, total int64) {
	if !p.verbose || p.quiet {
		return
	}
	fmt.Fprintf(os.Stderr, "│  tokens: +%d/+%d (total: %d)\n", input, output, total)
}

// OnDone prints the final summary line.
func (p *Progress) OnDone(turns int) {
	p.StopSpinner()
	if p.quiet {
		return
	}
	if !p.isTTY && !p.verbose {
		return
	}
	elapsed := time.Since(p.start)
	fmt.Fprintf(os.Stderr, "✓ Done (%d turns, %.1fs)\n", turns, elapsed.Seconds())
}

// Spinner animates a braille spinner on stderr.
type Spinner struct {
	msg   string
	stop  chan struct{}
	done  chan struct{}
	isTTY bool
}

var brailleFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// NewSpinner creates and starts a spinner.
func NewSpinner(msg string, isTTY bool) *Spinner {
	s := &Spinner{
		msg:   msg,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		isTTY: isTTY,
	}
	go s.run()
	return s
}

func (s *Spinner) run() {
	defer close(s.done)
	if !s.isTTY {
		fmt.Fprintf(os.Stderr, "%s\n", s.msg)
		<-s.stop
		return
	}
	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			// Clear the spinner line
			fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", len(s.msg)+4))
			return
		case <-ticker.C:
			frame := brailleFrames[i%len(brailleFrames)]
			fmt.Fprintf(os.Stderr, "\r%c %s", frame, s.msg)
			i++
		}
	}
}

// Stop stops the spinner and waits for cleanup.
func (s *Spinner) Stop() {
	select {
	case <-s.stop:
		// Already stopped
	default:
		close(s.stop)
	}
	<-s.done
}

// summarizeToolCall returns a human-readable summary of a tool invocation.
func summarizeToolCall(name string, argsJSON string) string {
	// Parse common patterns from JSON args for a concise summary
	switch name {
	case "grep":
		return fmt.Sprintf("grep %s", extractJSONField(argsJSON, "pattern"))
	case "find_files":
		return fmt.Sprintf("find %s", extractJSONField(argsJSON, "pattern"))
	case "read_file":
		path := extractJSONField(argsJSON, "path")
		startLine := extractJSONField(argsJSON, "start_line")
		endLine := extractJSONField(argsJSON, "end_line")
		if startLine != "" && endLine != "" {
			return fmt.Sprintf("read %s:%s-%s", path, startLine, endLine)
		}
		return fmt.Sprintf("read %s", path)
	case "list_dir":
		return fmt.Sprintf("ls %s", extractJSONField(argsJSON, "path"))
	case "submit_answer":
		return "submit_answer"
	default:
		return name
	}
}

// summarizeToolResult returns a one-line summary of a tool result.
func summarizeToolResult(name string, result string) string {
	if result == "" {
		return "empty"
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	switch name {
	case "grep":
		return fmt.Sprintf("%d matches", len(lines))
	case "find_files":
		return fmt.Sprintf("%d files", len(lines))
	case "read_file":
		return fmt.Sprintf("%d lines", len(lines))
	case "list_dir":
		return fmt.Sprintf("%d entries", len(lines))
	default:
		return fmt.Sprintf("%d lines", len(lines))
	}
}

// extractJSONField does a quick-and-dirty extraction of a string field from JSON.
// Not a full parser — good enough for display summaries.
func extractJSONField(json string, field string) string {
	key := fmt.Sprintf(`"%s"`, field)
	idx := strings.Index(json, key)
	if idx < 0 {
		return ""
	}
	rest := json[idx+len(key):]
	// Skip `: "`
	rest = strings.TrimLeft(rest, ": ")
	if len(rest) == 0 || rest[0] != '"' {
		// Numeric value
		rest = strings.TrimLeft(rest, ": ")
		end := strings.IndexAny(rest, ",}\n ")
		if end < 0 {
			return rest
		}
		return rest[:end]
	}
	// String value — find closing quote
	rest = rest[1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return rest
	}
	return rest[:end]
}
