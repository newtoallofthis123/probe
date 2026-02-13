# Changelog

All notable changes to probe will be documented in this file.

## [0.1.0] - 2026-02-14

First public release.

### Features

- **Agentic code search** — ask a question in plain English, get back file paths and line numbers
- **Iterative LLM search loop** — the agent calls grep, find, read, and list tools until it finds what you need
- **Five built-in tools** — `grep` (via ripgrep), `find_files`, `read_file`, `list_dir`, `submit_answer`
- **Any OpenAI-compatible API** — works with Ollama, OpenAI, Groq, OpenRouter, and more
- **Output formats** — `human` (with color), `json`, `paths` (for xargs), `qf` (vim quickfix)
- **Think mode** (`--think` / `-t`) — thorough search with more turns and deeper verification
- **Verbose mode** (`-v`) — see the agent's search trace in real time with spinner and tool calls
- **Config file support** — `~/.config/probe/config.toml` with flags > env > file > defaults precedence
- **Stdin support** — pipe context into probe alongside your query
- **Smart defaults** — auto-detects project language, respects `.gitignore`, sandboxes all paths
- **Pipe-friendly** — stdout has results only, stderr has everything else; colors and reasons stripped when piped

### Infrastructure

- CI workflow with fmt, vet, and test checks
- GoReleaser setup for cross-platform binary releases
- Issue templates for bug reports and feature requests

[0.1.0]: https://github.com/newtoallofthis123/probe/releases/tag/v0.1.0
