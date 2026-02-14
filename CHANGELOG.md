# Changelog

All notable changes to probe will be documented in this file.

## [Unreleased]

### Features

- **Query history** (`--list`, `--all`, `--show`) — automatically saves search results to a local SQLite database. Recall past queries and re-display results without re-running the search.
- **Search modes** (`-m` / `--mode`) — three modes: `locate` (fast, find where something is), `explore` (thorough, understand how something works), `trace` (sequential, follow a call/data path). Default `auto` lets the LLM choose.
- **Turn-aware pressure** — each turn injects `[Turn N — M remaining]` so the agent calibrates depth vs. urgency. Mode-specific pressure replaces the old hardcoded nudge.
- **`select_mode` tool** — in auto mode, the first turn forces a `select_mode` tool call so the LLM declares its approach before searching.

### Changed

- **`list_dir` renamed to `tree`** — same functionality, clearer name.
- **`PROBE_MODE` env var** and `mode` config file key added.

## [0.1.0] - 2026-02-14

First public release.

### Features

- **Agentic code search** — ask a question in plain English, get back file paths and line numbers
- **Iterative LLM search loop** — the agent calls grep, find, read, and list tools until it finds what you need
- **Five built-in tools** — `grep` (via ripgrep), `find_files`, `read_file`, `tree`, `submit_answer`
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
