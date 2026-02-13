# probe

[![CI](https://github.com/newtoallofthis123/probe/actions/workflows/ci.yml/badge.svg)](https://github.com/newtoallofthis123/probe/actions/workflows/ci.yml)

**Agentic code search from the command line.**

Ask a question in plain English. Get back file paths and line numbers.

<video src="https://github.com/newtoallofthis123/probe/raw/main/assets/demo.mp4" autoplay loop muted playsinline width="100%"></video>

## Why probe?

You know what you're looking for, just not *where* it is. Grep needs you to know the exact string. IDE search needs you to know the filename. probe just needs the question.

```bash
probe "where is the rate limiting logic?"
```

```
src/middleware/ratelimit.go:18-45   Token bucket implementation with per-IP tracking
src/config/limits.go:3-11          Rate limit constants and defaults
```

No embeddings. No vector database. No indexing step. probe hands an LLM a few Unix tools — grep, find, read — and lets it search iteratively until it finds what you need.

## What makes it different

- **Zero setup** — `go install` and go. No indexing, no config files, no database
- **Any LLM** — works with Ollama locally, or OpenAI/Groq/OpenRouter in the cloud
- **Pipe-friendly** — stdout has results, stderr has everything else. Plays nice with Unix
- **Read-only** — probe never writes to your codebase. All paths are sandboxed
- **Fast** — small models (3B) work great. Most searches finish in under 5 seconds

## Install

Requires [Go 1.24+](https://go.dev/dl/) and [ripgrep](https://github.com/BurntSushi/ripgrep#installation).

```bash
go install github.com/newtoallofthis/probe@latest
```

Or grab a binary from [Releases](https://github.com/newtoallofthis123/probe/releases).

## Quick start

```bash
# With Ollama (free, local)
ollama pull ministral-3:3b
probe "where are the database migrations?"

# With any OpenAI-compatible API
export PROBE_API_KEY="sk-..."
export PROBE_BASE_URL="https://api.openai.com/v1"
export PROBE_MODEL="gpt-4o-mini"
probe "how does authentication work?"
```

## Examples

```bash
# Search and open in vim's quickfix list
probe --format=qf "error handling" | vim -q /dev/stdin

# Pipe to other tools
probe --format=paths "auth" | xargs wc -l

# Thorough search for complex questions
probe -t "how does the billing system calculate prorated charges?"

# JSON output for scripts
probe --json "database connection setup" | jq '.results[].file'

# Watch the agent think
probe -v "where is main?"
```

## Docs

- **[Usage](docs/usage.md)** — flags, environment variables, output formats, providers
- **[Architecture](docs/architecture.md)** — how it works, project structure, development setup
- **[Changelog](CHANGELOG.md)** — release history

## License

MIT
