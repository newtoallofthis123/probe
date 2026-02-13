# Usage

```
probe [flags] <query>
```

## Flags

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

## Environment variables

| Variable | Description |
|---|---|
| `PROBE_API_KEY` | API key for the LLM provider |
| `PROBE_MODEL` | Default model name |
| `PROBE_BASE_URL` | Default API base URL |
| `PROBE_MAX_TURNS` | Default max turns |

Precedence: flags > environment variables > defaults.

## Providers

probe connects to any OpenAI-compatible API. The fastest way to get started is with [Ollama](https://ollama.com) running locally:

```bash
ollama pull ministral-3:3b
cd your-project
probe "where are the database migrations?"
```

Point probe at any cloud provider:

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
```

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
