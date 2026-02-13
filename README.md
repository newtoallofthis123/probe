# probe

[![CI](https://github.com/newtoallofthis123/probe/actions/workflows/ci.yml/badge.svg)](https://github.com/newtoallofthis123/probe/actions/workflows/ci.yml)

Agentic code search from the command line. Ask a question in plain English, get back file paths and line numbers.

```
$ probe "where is the user authentication middleware?"
src/middleware/auth.ts:14-58     Defines verifyToken() and requireAdmin()
src/utils/jwt.ts:3-22           JWT signing and verification helpers
src/routes/login.ts:7-12        Route-level auth check
```

No embeddings. No vector database. No indexing step. probe gives an LLM a handful of Unix tools (grep, find, read) and lets it search iteratively until it finds what you asked for.

## Install

Requires [Go 1.24+](https://go.dev/dl/) and [ripgrep](https://github.com/BurntSushi/ripgrep#installation).

```bash
go install github.com/newtoallofthis/probe@latest
```

Or build from source:

```bash
git clone https://github.com/newtoallofthis/probe.git
cd probe
go build -o probe ./cmd/probe/
```

## Quick start

probe connects to any OpenAI-compatible API. The fastest way to get started is with [Ollama](https://ollama.com) running locally:

```bash
# Pull a model
ollama pull ministral-3:3b

# Search your codebase
cd your-project
probe "where are the database migrations?"
```

That's it. No config files needed. probe auto-detects your project language, respects `.gitignore`, and searches from the current directory.

### Using a cloud provider

Point probe at any OpenAI-compatible endpoint:

```bash
# OpenAI
export PROBE_API_KEY="sk-..."
export PROBE_BASE_URL="https://api.openai.com/v1"
export PROBE_MODEL="gpt-4o-mini"

# Anthropic (via OpenRouter)
export PROBE_API_KEY="sk-or-..."
export PROBE_BASE_URL="https://openrouter.ai/api/v1"
export PROBE_MODEL="anthropic/claude-sonnet-4-20250514"

# Groq
export PROBE_API_KEY="gsk_..."
export PROBE_BASE_URL="https://api.groq.com/openai/v1"
export PROBE_MODEL="llama-3.3-70b-versatile"

probe "how does the rate limiter work?"
```

## Usage

```
probe [flags] <query>
```

### Flags

| Flag | Description | Default |
|---|---|---|
| `--model <name>` | Model name | `ministral-3:3b` |
| `--base-url <url>` | API base URL | `http://localhost:11434/v1` |
| `--max-turns <n>` | Maximum agent turns | `10` |
| `--dir <path>` | Project directory to search | `.` |
| `--json` | Output results as JSON | |
| `--format <fmt>` | Output format: `human`, `json`, `paths`, `qf` | `human` |
| `-t`, `--think` | Thorough search mode (more turns, deeper verification) | |
| `-v`, `--verbose` | Show agent search trace on stderr | |
| `-q`, `--quiet` | Suppress all stderr output | |
| `--version` | Print version and exit | |

### Environment variables

| Variable | Description |
|---|---|
| `PROBE_API_KEY` | API key for the LLM provider |
| `PROBE_MODEL` | Default model name |
| `PROBE_BASE_URL` | Default API base URL |
| `PROBE_MAX_TURNS` | Default max turns |

Precedence: flags > environment variables > defaults.

## Output formats

### Human (default)

When stdout is a terminal, results include file paths, line ranges, and reasons with color:

```
src/middleware/auth.ts:14-58     Defines verifyToken() and requireAdmin()
src/utils/jwt.ts:3-22           JWT signing and verification helpers
```

When piped, reasons and colors are stripped automatically:

```bash
probe "auth middleware" | head -1
# src/middleware/auth.ts:14-58
```

### JSON

```bash
probe --json "auth middleware"
```

```json
{
  "results": [
    {
      "file": "src/middleware/auth.ts",
      "start_line": 14,
      "end_line": 58,
      "reason": "Defines verifyToken() and requireAdmin()"
    }
  ],
  "summary": "Found auth middleware and JWT helpers",
  "turns": 3
}
```

### Paths

Deduplicated file paths, one per line. Designed for `xargs`:

```bash
probe --format=paths "auth" | xargs wc -l
```

### Quickfix

Vim/Neovim quickfix-compatible format (`file:line:col: message`). Load results directly into your editor's quickfix list:

```bash
probe --format=qf "auth middleware" | vim -q /dev/stdin
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Results found |
| `1` | No results found |
| `2` | Error (bad config, API unreachable, etc.) |

This follows grep conventions, so probe works naturally in scripts:

```bash
probe "auth" && echo "found" || echo "nothing"
```

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

## Verbose mode

See what the agent is doing:

```bash
probe -v "where is main?"
```

```
⠋ Searching...
├─ grep func\s+main
│  → 2 matches
├─ read main.go:1-50
│  → 50 lines
│  tokens: +512/+128 (total: 640)
├─ submit_answer
✓ Done (3 turns, 1.8s)
main.go:28-36    CLI entrypoint and orchestration
```

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

### Architecture

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

## License

MIT
