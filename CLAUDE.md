# probe — Project Guidelines

Read `research/probe-spec.md` for the full technical specification. Read `thoughts/tickets/` for implementation tickets. What follows are the rules for writing code in this project.

## What This Is

`probe` is a CLI tool. It takes a natural language query, uses an LLM to iteratively search a codebase with grep/find/read tools, and returns file paths + line numbers. That's it. Not a chatbot, not an explainer, not an indexer.

## Architecture

Everything is `package main`. Eight files, each with a single responsibility:

- `main.go` — CLI entrypoint, flag parsing, signal handling, orchestration
- `agent.go` — The agent while-loop. ~50 lines of real logic.
- `tools.go` — Tool schema definitions (pure data, no behavior)
- `tools_exec.go` — Tool execution (each executor is a pure function: args in, string out)
- `prompt.go` — System prompt construction with project context injection
- `output.go` — Result formatting and progress display
- `config.go` — Config loading with precedence: flags > env > file > defaults
- `gitignore.go` — Gitignore parsing and path filtering

Do not create new files unless the ticket explicitly says to. Do not create `internal/`, `pkg/`, or subdirectories. Do not extract "util" packages. When a file grows uncomfortable, that's a future problem — not today's.

## Task Runner

Use `just` for all project commands. Run `just` to see available commands. Key commands:

- `just build` — compile the binary
- `just test` — run all tests
- `just test-one TestName` — run a single test
- `just run "query"` — build and run with a query
- `just ci` — fmt + vet + test (run before committing)

Always verify your work compiles with `just build` and tests pass with `just test`.

## Code Style

### Go Conventions
- Standard `go fmt` formatting. No exceptions.
- No third-party libraries unless the ticket explicitly approves one. The dependency bar is high.
- Error handling: return errors, don't panic. The only `os.Exit` calls are in `main.go`.
- No `init()` functions. Explicit initialization in `main()`.

### Tool Errors Are LLM Messages, Not Go Errors

This is the most important pattern in the codebase. When a tool fails (file not found, grep returns nothing, path outside sandbox), that is NOT a Go error. It's a string message returned to the LLM so it can self-correct.

```go
// CORRECT: tool failure → string message back to LLM
if !fileExists(path) {
    return fmt.Sprintf("Error: file '%s' not found. Files in same directory: %s", path, siblings), nil
}

// WRONG: tool failure → Go error that crashes or short-circuits
if !fileExists(path) {
    return "", fmt.Errorf("file not found: %s", path)
}
```

Go errors from `ExecuteTool` mean the harness itself is broken (context cancelled, system failure). Tool-level failures are always string results. The LLM recovers from bad guesses — let it.

### stdout vs stderr

This is sacred. Get it wrong and piping breaks.

- **stdout:** Results only. The data the user asked for. Nothing else.
- **stderr:** Everything else — progress, spinners, traces, warnings, errors, summaries.

```go
// Results → stdout
fmt.Printf("%s:%d-%d  %s\n", file, start, end, reason)

// Everything else → stderr
fmt.Fprintf(os.Stderr, "searching %s...\n", pattern)
```

### Path Sandboxing

Every file path from the LLM goes through `safePath()` before any I/O. No exceptions. Don't trust the LLM. Don't trust the input. Resolve, check containment, then operate.

### Tool Result Truncation

Every tool result is truncated and the LLM is told it was truncated. Grep: 30 matches. File read: 200 lines. Dir listing: 100 entries. Always append a feedback message like `"(showing 30 of 312 matches — refine your search)"`. The LLM needs to know its view is partial.

## Testing

- Unit tests go in `*_test.go` next to the file they test.
- Tool executor tests should work without an LLM and without network access.
- Test the sandbox: always include a path-traversal test (`../../etc/passwd` → error string, not crash).
- Test truncation: verify the feedback message appears when results exceed limits.
- Integration/validation tests live in `test/` and require Ollama running.

## What Not To Do

- Don't add packages, directories, or abstractions the ticket doesn't ask for.
- Don't add comments explaining obvious code. Do add comments explaining *why* something non-obvious is done.
- Don't add error handling for impossible cases. Trust internal code.
- Don't add configurability the ticket doesn't specify.
- Don't write to the filesystem. probe is read-only. The tools are read-only. There is no write path.
- Don't wrap the OpenAI SDK. Use it directly.
- Don't parse free text from the LLM to extract results. That's what `submit_answer` is for.
