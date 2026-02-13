# Architecture

## How it works

probe is an agent loop. It sends your query to an LLM along with your project's directory tree, then lets the LLM iteratively call search tools until it finds what you asked for:

```
User query
    │
    ▼
Build system prompt (project tree, language hints, recent files)
    │
    ▼
Agent loop (up to --max-turns iterations):
    │
    ├─ LLM decides which tools to call
    ├─ Tools execute in parallel (grep, find, read, list)
    ├─ Results fed back to LLM
    └─ Repeat until LLM calls submit_answer
    │
    ▼
Format and print results
```

The LLM has five tools:

| Tool | Purpose |
|---|---|
| `grep` | Search file contents with ripgrep (regex, globs) |
| `find_files` | Discover files/directories by pattern |
| `read_file` | Read file contents with line numbers |
| `list_dir` | List directory tree |
| `submit_answer` | Return final results with file locations |

All tools are **read-only**. All file paths are **sandboxed** to the project directory. Results are **filtered** through `.gitignore`. Tool output is **truncated** with feedback so the LLM knows its view is partial.

## Project structure

Standard Go project layout with `cmd/` entrypoint and `internal/` packages:

| Package | Responsibility |
|---|---|
| `cmd/probe/` | CLI entrypoint, flag parsing, signal handling |
| `internal/agent/` | Agent while-loop, result types |
| `internal/tools/` | Tool schemas and execution (pure functions: args in, string out) |
| `internal/prompt/` | System prompt construction with project context |
| `internal/output/` | Progress display and result formatting |
| `internal/config/` | Config loading (flags > env > file > defaults) |
| `internal/sandbox/` | Path sandboxing, gitignore filtering |

## Development

Requires [just](https://github.com/casey/just) as a task runner.

```bash
just build          # compile the binary
just test           # run all tests
just test-one Name  # run a single test
just ci             # fmt + vet + test
just run "query"    # build and run
just run-v "query"  # run with verbose
just run-on DIR "q" # run against a specific directory
```
