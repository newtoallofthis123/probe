# `probe` — Agentic Code Search CLI
## Technical Specification & Engineering Handbook

**Status:** Ready for implementation
**Authors:** CTO / Co-founder
**Date:** February 2026

---

## Table of Contents

1. What We Are Building
2. What We Are Using
3. Design Philosophy & Code Directives
4. Architecture & Code Organization
5. The Agent Harness
6. The Tools
7. System Prompt & LLM Guidance
8. Optimizations & Performance

---

## 1. What We Are Building

`probe` is a command-line tool that takes a natural language query about a codebase and returns the specific files and line numbers that answer that query. Nothing else.

```
$ probe "where is the user authentication middleware defined?"

→ src/middleware/auth.ts:14-58      Defines verifyToken() and requireAdmin()
→ src/utils/jwt.ts:3-22            JWT signing and verification helpers
→ src/routes/login.ts:7-12         Route-level auth check
```

There is no embedding pipeline. No vector database. No indexing step. No persistent infrastructure of any kind. `probe` works by giving an LLM a small set of sharp Unix tools — grep, file search, file read — and letting it search iteratively through a while loop until it finds what the user asked for. The intelligence comes from the LLM's reasoning, not from the retrieval mechanism.

This is the approach that the industry has converged on. Claude Code started with RAG and a local vector DB, then abandoned it. Their engineer stated publicly: "We found pretty quickly that agentic search generally works better. It is also simpler and doesn't have the same issues around security, privacy, staleness, and reliability." Augment Code, the top SWE-Bench performer, found the same thing: "We explored adding various embedding-based retrieval tools, but found that grep and find were sufficient." Modern LLMs are smart enough to compose grep, find, and read iteratively and achieve comparable accuracy to RAG with zero infrastructure overhead.

The tool connects to any OpenAI-compatible API. For local development and validation, we run Ollama with Ministral 3B. For production quality, swap the base URL to OpenAI, Anthropic, Groq, Together, OpenRouter, or any other provider. One-line config change.

### What probe is not

It is not a chatbot. There is no multi-turn conversation. The user asks a question, probe finds the answer, probe exits. It is not a code explainer — it finds code, it does not explain code. It is not an indexer — there is no "building index, please wait" step. It is not a RAG system. It is a search tool that happens to use an LLM for orchestration.

---

## 2. What We Are Using

### Language: Go

Go gives us a single static binary with zero runtime dependencies, fast startup time (no interpreter boot), native cross-compilation, proper signal handling, and excellent concurrency primitives for running parallel tool calls. It is the Unix tool language. The entire tool ships as one file that the user drops into their PATH.

### LLM SDK: openai-go v3

The official OpenAI Go SDK at `github.com/openai/openai-go/v3`. We target the Chat Completions API exclusively — not the Responses API — because Chat Completions is the standard that every provider implements. This gives us portability across OpenAI, Ollama, vLLM, Together, Groq, OpenRouter, and anything else that speaks the same protocol.

The SDK provides built-in support for tool definitions via function calling, streaming via SSE, a `ChatCompletionAccumulator` for assembling streamed tool calls, and structured union types for request/response handling. We use it as-is with no wrappers.

### Validation Model: Ministral 3B via Ollama

For local development and prototyping, we run Ministral 3B (`ministral-3:3b`) through Ollama. This model was explicitly trained for agentic workflows and tool orchestration by Mistral AI. It has a 256k native context window, which eliminates any concern about running out of context during a multi-turn agent loop. At 3B parameters and roughly 3GB of disk, it loads fast and runs fast on consumer hardware. Apache 2.0 license.

Ollama exposes an OpenAI-compatible endpoint at `http://localhost:11434/v1`. The SDK connects to it by overriding the base URL and providing a dummy API key. Swapping to a cloud model later is a one-line change.

The fallback validation model is Qwen3 8B (`qwen3:8b`), which provides stronger code comprehension and a thinking/non-thinking toggle. If Ministral 3B proves too flaky during testing — bad regex construction, incorrect tool selection, failure to converge — drop to Qwen3 8B before investigating further.

### Search Backend: ripgrep

All text search is done by shelling out to `rg` (ripgrep). It is already installed on most developer machines, it respects `.gitignore` natively, it is millisecond-fast on any reasonable repository, and its output format is easy to parse. We do not embed a search library. We shell out to the binary.

### Configuration: TOML

Config files use TOML. Searched in order: `.probe.toml` in the project root, then `$XDG_CONFIG_HOME/probe/config.toml` (typically `~/.config/probe/config.toml`). Environment variables override config file values. No config is required for first run — sensible defaults for everything.

---

## 3. Design Philosophy & Code Directives

These are non-negotiable. Every engineering decision should be checked against these principles.

### 3.1 — Unix Native

`probe` is a Unix tool, not a SaaS product. This means:

**stdout is sacred.** Results — and only results — go to stdout. Everything else goes to stderr: progress indicators, agent turn traces, streaming status, warnings, errors. This makes `probe "auth" | xargs vim` work without any flags. If stdout is a TTY, render with colors and formatting. If stdout is a pipe, emit clean machine-parseable output — one `file:line_start-line_end` per line.

**Exit codes mean something.** 0 means results were found. 1 means no results were found (matching grep convention). 2 means an error occurred (bad config, API unreachable, model failure). This enables `probe "auth" && echo "found"` and `probe "auth" || echo "nothing"`.

**Signals are handled.** SIGINT (Ctrl+C) gracefully stops the agent loop and dumps whatever partial results have been gathered so far — not a silent death. SIGPIPE is handled so `probe "auth" | head -1` does not produce a broken pipe error. SIGTERM performs clean shutdown.

**Respect the environment.** Read `NO_COLOR` and `CLICOLOR` for color output decisions. Follow `$XDG_CONFIG_HOME` for config file location. Read the API key from an environment variable, not from a flag.

**Quiet by default.** No banners, no version announcements, no ASCII art, no tips, no "I found the following results for you." The default output is just the results. `--verbose` or `-v` reveals the agent's search trace on stderr. `--quiet` or `-q` suppresses even the results and returns only the exit code.

### 3.2 — Senior Dev Energy

The tool should feel like asking a senior developer who has been on the codebase for two years. When you ask them "where's the auth middleware?", they do not give you a lecture on authentication, list every file in the project, or ask five clarifying questions. They say: "src/middleware/auth.ts, around line 14. The JWT config is in src/config/auth.ts if you need that too."

This means: fewer, better results. If the LLM returns 15 matches, that is a failure of precision. Three results that are all dead-on is the target. The LLM should be opinionated about what is actually relevant, not dump everything that matched a grep.

Each result includes a one-line reason explaining why it was returned, so the developer can scan the output in under two seconds and know if they found what they need.

### 3.3 — Zero Config to Start

The single most important UX decision. `probe` works with zero configuration the moment you `cd` into a project directory and run it. Auto-detect language from manifest files (package.json, go.mod, Cargo.toml, pyproject.toml). Respect `.gitignore` by default. Use sensible defaults for every parameter. Configuration exists for power users. It is never required.

Compare this to any RAG-based tool: install a vector database, run an indexing pipeline, wait for it to finish, configure chunk sizes and embedding models, then search. `probe`'s superpower is that it works instantly on any repo without setup.

### 3.4 — Read-Only, Always

`probe` never writes, executes, modifies, or deletes anything. Every tool in the toolset is read-only. This is enforced at the harness level, not at the prompt level. The harness physically cannot perform a write operation. There is no "well the model asked nicely" escape hatch. This is a search tool. It searches.

### 3.5 — Path Sandboxing

All tool operations are confined to the project root directory. No path traversal. No `../../etc/passwd`. No symlink escape. Every file path that the LLM requests is resolved and validated against the project root before any I/O occurs. If it falls outside the sandbox, the tool returns an error to the LLM (not to the user, not to the crash handler — to the LLM, so it can self-correct).

### 3.6 — Composability

Output is designed to be piped:

```bash
# Open the top result in your editor
probe "auth middleware" --json | jq -r '.[0].file' | xargs code -g

# Count lines in all matched files
probe "database migrations" --format=paths | xargs wc -l

# Search only recently changed files
git diff --name-only HEAD~5 | probe "error handling" --stdin
```

JSON output via `--json`. Paths-only output via `--format=paths`. Stdin file list support via `--stdin`.

---

## 4. Architecture & Code Organization

### Project Structure

```
probe/
├── main.go           CLI entrypoint: arg parsing, config loading, orchestration
├── agent.go          The agent loop — the core of the entire system
├── tools.go          Tool schema definitions sent to the LLM
├── tools_exec.go     Tool execution — actually runs grep, reads files, etc.
├── prompt.go         System prompt construction and project context injection
├── output.go         Result parsing, formatting, display
├── config.go         Config file loading, env var resolution, defaults
├── gitignore.go      Gitignore rule parsing and path filtering
├── go.mod
├── go.sum
└── README.md
```

Everything lives in `package main`. No internal packages, no pkg directory, no over-abstraction. This is a small, focused tool. When the file count reaches a point where `package main` genuinely hurts readability, split then — not before.

### Component Responsibilities

**main.go** — The entry point. Parses CLI flags and arguments. Loads config (file, then env var overrides). Validates that prerequisites exist (ripgrep binary on PATH). Sets up signal handlers. Calls into the agent loop. Receives results. Formats and prints them. Exits with appropriate code.

**agent.go** — Contains exactly one important function: the agent loop. Takes a query string, config, and tools. Returns structured results. This is the heart of `probe` and it should be around 50 lines of real logic. Everything else in the system exists to serve this loop.

**tools.go** — Defines the tool schemas in the format the OpenAI SDK expects. These are the JSON schemas the LLM sees when deciding what to call. Each tool has a name, description, and parameter schema. This file is pure data — no behavior.

**tools_exec.go** — Implements the actual execution of each tool. When the LLM says "call grep with pattern X and glob Y", this file translates that into a subprocess call to ripgrep, captures the output, truncates it if necessary, and returns it as a string. Each tool executor is a pure function: arguments in, string result out. Errors are returned as string messages, never as Go errors that crash the process.

**prompt.go** — Builds the system prompt at runtime. Generates the project tree by walking the filesystem. Detects language and framework from manifest files. Injects all of this into the prompt template. The prompt is the soul of the agent and this file is where it lives.

**output.go** — Parses the LLM's final structured response (from the `submit_answer` tool call) into a typed result struct. Formats results for human display, JSON output, or paths-only output depending on flags. Handles the edge case where the agent hit the max turn limit without submitting a clean answer.

**config.go** — Defines the config struct with sensible defaults. Loads from TOML files in precedence order. Applies env var overrides. Exposes the final resolved config to the rest of the program.

**gitignore.go** — Parses `.gitignore` rules and provides a filter function. Every tool result passes through this filter before being returned to the LLM. node_modules, .git, .env, build artifacts, and anything else in .gitignore never appear in results.

### Data Flow

```
User query (string)
    │
    ▼
main.go ── loads config, builds system prompt, starts agent loop
    │
    ▼
agent.go ── sends messages to LLM, receives tool calls or final answer
    │           │
    │           ▼
    │       tools_exec.go ── executes tool, returns result string
    │           │
    │           ▼
    │       gitignore.go ── filters result through ignore rules
    │           │
    │           ▼
    │       (result appended to message history, loop continues)
    │
    ▼
output.go ── parses final answer, formats, prints to stdout
    │
    ▼
Exit code 0 (found results) or 1 (no results)
```

---

## 5. The Agent Harness

The harness is the while loop that sits between the user's query and the LLM's final answer. It is the orchestration layer. A quote from Braintrust's engineering team captures it well: "An agent is just a system prompt and a handful of well-crafted tools." The harness is the while loop around that.

### The Core Loop

The loop logic is intentionally minimal. In pseudocode:

```
function agentSearch(query, maxTurns):
    messages = [systemPrompt, userMessage(query)]
    
    for turn in 1..maxTurns:
        response = llm.chat(messages, tools, stream=true)
        
        if response has tool calls:
            results = execute all tool calls in parallel
            append assistant message to messages
            append all tool result messages to messages
        
        else if response is a submit_answer tool call:
            return parse the structured results
        
        else:
            // LLM returned text with no tool calls — treat as done
            return extract best-effort results from text
    
    // Hit max turns — return whatever we have
    return partial results from message history
```

That is roughly 20 lines of real logic. Everything below elaborates on the details that make those 20 lines reliable.

### Tool Result Truncation

This is the single most important harness-level concern. A grep call can return 500 matching lines. If all 500 lines are dumped into the context window, two things happen: the token budget is consumed in one or two turns, and the LLM gets confused by noise.

All tool results must be truncated. Grep results are capped at 30 matches. File reads are capped at 200 lines. Directory listings are capped at 100 entries. When a result is truncated, the harness appends a message to the tool output: "(showing 30 of 312 matches — refine your search to narrow down)". This feedback signal teaches the LLM to construct more specific queries on the next turn. It is critical that the LLM knows its results were truncated, not that it silently received a partial view.

### Error Handling — Errors Go to the LLM

Tools will fail. The file does not exist. Grep returns zero matches. A glob pattern matches nothing. A path is outside the sandbox. These are not crash conditions. They are information for the LLM.

Every tool error is returned to the LLM as a tool result message, not swallowed or thrown. For example: "Error: file 'src/auth/middleware.ts' not found. Similar files: src/middleware/auth.ts, src/auth/index.ts". The LLM will self-correct. This is one of the superpowers of agentic search — the agent recovers from bad guesses. The harness must never crash on a tool error. It must always feed the error back into the conversation.

When returning "file not found" errors, the harness should include suggestions: list files in the same directory, or files with similar names. This saves the LLM a turn.

### The "Done" Signal: submit_answer

The LLM signals completion by calling a special `submit_answer` tool with structured results. This is preferred over the implicit approach (detecting that the LLM returned text without tool calls) because it guarantees a parseable, structured response. No free-text parsing. No regex extraction of file paths from prose. The submit_answer tool forces the LLM to commit to a specific set of file/line_range/reason tuples, which the harness can directly serialize to output.

If the LLM returns a text response without calling submit_answer and without calling any tools, the harness treats it as a "nothing found" case and exits with code 1.

### Parallel Tool Execution

The OpenAI Chat Completions API allows the model to request multiple tool calls in a single turn. For example, the LLM might call grep and find_files simultaneously. The harness must execute these calls concurrently (using goroutines), not sequentially. This is free speed. Each tool call is independent and has no side effects, so there is no ordering concern.

### Guardrails

**Max turns:** Hard cap at 10 by default, configurable. Ministral 3B should be capped at 5 — small models degrade with more turns because they lose the thread.

**Max tokens per tool result:** Enforced by truncation (see above).

**Read-only enforcement:** The harness validates every tool call before execution. Only the four registered tool names are accepted. Only the registered parameter schemas are accepted. There is no eval, no shell passthrough, no arbitrary command execution.

**Path sandboxing:** Every file path argument is resolved to an absolute path and checked against the project root before I/O. Paths containing `..` are rejected. Symlinks that resolve outside the root are rejected.

**Gitignore filtering:** All tool outputs are filtered through .gitignore rules before being returned to the LLM. The LLM never sees node_modules, .git, .env, vendor, build, dist, or anything else the project has chosen to ignore.

---

## 6. The Tools

The LLM gets five tools. Four are search tools. One is the termination signal.

### 6.1 — grep

Searches file contents using ripgrep. This is the primary discovery tool. The LLM will use this more than anything else.

**Parameters:**
- `pattern` (string, required) — The search pattern. Supports regex. This is passed directly to `rg` as the pattern argument.
- `glob` (string, optional) — File glob to narrow the search scope. Examples: `*.ts`, `src/middleware/**`, `*.py`. Passed to `rg --glob`.
- `max_results` (integer, optional, default 30) — Maximum number of matching lines to return.

**Execution:** Shell out to `rg --line-number --no-heading --color never --max-count {max_results} --glob {glob} "{pattern}" {project_root}`. Parse the output into `file:line — content` format. Truncate to max_results. If truncated, append the "(showing N of M matches)" feedback.

**Return format to the LLM:** Each match on its own line, formatted as `file:line — line_content`. The line content preview is critical — it lets the LLM decide whether to read the full file without burning another turn.

### 6.2 — find_files

Discovers files and directories by name or path pattern. Used for structural orientation — "what files exist that relate to auth?"

**Parameters:**
- `pattern` (string, required) — Glob pattern to match against file and directory names. Examples: `**/auth*`, `*.py`, `src/**/models/`.
- `type` (string, optional, one of "file" or "dir") — Filter to only files or only directories. Default is both.

**Execution:** Walk the project tree, filtering through gitignore rules, matching against the glob pattern. Alternatively, shell out to `find` or `fd` if available. Return matching paths, one per line.

### 6.3 — read_file

Reads the contents of a specific file, optionally scoped to a line range. This is the verification tool — the LLM uses grep to find candidate locations, then read_file to confirm relevance and extract exact line numbers.

**Parameters:**
- `path` (string, required) — Relative path to the file from the project root.
- `start_line` (integer, optional) — First line to read (1-indexed). If omitted, starts at line 1.
- `end_line` (integer, optional) — Last line to read (inclusive). If omitted, reads to end of file.

**Execution:** Read the file. If line range is specified, extract those lines. Prepend each line with its line number. Truncate at 200 lines if no range was specified and the file is longer — append "(file truncated at 200 lines, use start_line/end_line to read specific sections)".

**Return format:** Numbered lines, exactly as they appear in the file. The LLM needs accurate line numbers to populate its submit_answer response, so the line numbers in the read_file output must be correct.

### 6.4 — list_dir

Lists files and subdirectories at a given path. Used for initial orientation and for navigating into directories found by other tools.

**Parameters:**
- `path` (string, optional, default ".") — Directory path relative to project root.
- `depth` (integer, optional, default 2) — Maximum depth of the tree. Deeper values show more structure but consume more tokens.

**Execution:** Walk the directory tree to the specified depth. Filter through gitignore rules. Format as an indented tree. Include file counts per directory for large directories (e.g., `utils/ (12 files)` instead of listing all 12).

### 6.5 — submit_answer

The termination tool. When the LLM has found all relevant code, it calls this tool with its final structured results. The harness intercepts this call, parses the structured data, and returns it to the user. This tool is never "executed" — it is a signal.

**Parameters:**
- `results` (array, required) — Each element contains:
  - `file` (string) — Relative file path
  - `start_line` (integer) — First line of the relevant range
  - `end_line` (integer) — Last line of the relevant range
  - `reason` (string) — One-line explanation of why this file and range is relevant
- `summary` (string, required) — Brief overall summary of what was found

### Tool Count Consideration for Small Models

Five tools is the right count for a capable model. For Ministral 3B, if testing reveals that it frequently picks the wrong tool or constructs bad arguments, the fallback strategy is to collapse to three tools: `search` (which combines grep and find_files, with the harness deciding which to use based on the argument pattern), `read_file`, and `submit_answer`. Fewer choices means fewer mistakes.

---

## 7. System Prompt & LLM Guidance

The system prompt is the soul of the agent. It is constructed dynamically at runtime.

### The Prompt

```
You are a code search agent. Your job is to find the specific files and
line numbers in a codebase that are relevant to the user's query.

## How you work

You have access to search tools. Use them iteratively to narrow down
the relevant code. Start broad, then refine.

Typical workflow:
1. Look at the project structure to orient yourself
2. Use grep/find to locate candidate files
3. Read the most promising files to confirm relevance
4. Submit your final results with exact line numbers

## Rules

- Be precise. Return specific line ranges, not entire files.
- Be thorough. Check related files — imports, configs, tests.
- Be efficient. Don't read files you can rule out from grep results.
- If grep returns too many results, refine your search pattern.
- If you can't find what the user is asking about, say so honestly.
- NEVER guess line numbers. Always verify by reading the file.

## Project context

{PROJECT_TREE}

{LANGUAGE_HINT}

## Output

When you've found all relevant code, call submit_answer with:
- Each relevant file, its line range, and why it's relevant
- A brief summary of what you found

Do not explain your search process. Just find the code.
```

### Why the Prompt is Shaped This Way

**Workflow over rules.** "Start broad, then refine" is more useful than twenty specific rules. The LLM figures out the specifics. Every successful agent deployment confirms this: guide the strategy, not the tactics.

**Dynamic context injection.** The `{PROJECT_TREE}` placeholder is filled at runtime with the actual project directory structure (depth 2, with file counts). The `{LANGUAGE_HINT}` is detected from manifest files — "This is a TypeScript project using Express and Prisma" or "This is a Go module using Gin and GORM." This front-loaded context lets the LLM make a smarter first tool call. Instead of wasting Turn 1 on `list_dir /`, it can go straight to `grep "middleware" src/middleware/`. One fewer turn is 1-2 seconds saved.

**"Do not explain your search process."** Without this directive, the LLM spends tokens narrating: "I'll start by looking at the project structure to understand the layout..." That is wasted tokens and wasted time. We want it to go straight to tool calls.

**"NEVER guess line numbers."** LLMs will hallucinate line numbers if permitted. This instruction forces the LLM to always verify by calling read_file before submitting its answer. This is worth the extra turn — accuracy is more important than speed.

### Prompt Variant for Ministral 3B

Ministral 3B needs a simpler, more directive prompt. Remove the workflow suggestions. Be explicit about limits:

```
Search the codebase for files matching the user's query.
Use `grep` to find files, `read_file` to verify, then `submit_answer`.
Be precise with line numbers. Maximum 5 tool calls.

{PROJECT_TREE}
```

The smaller the model, the shorter and more direct the prompt should be. Workflow guidance that helps a large model can confuse a small one.

### Additional Context to Inject

Beyond the project tree, the prompt builder can inject:

- **Recently modified files** from `git log --name-only -10` — biases the LLM toward active areas of the codebase, which is where most queries are directed.
- **File count and repo size** — "This repository contains 247 files across 34 directories" helps the LLM calibrate how thorough its search needs to be.
- **Package dependencies** extracted from manifest files — knowing that the project uses Express or Django or Gin helps the LLM reason about architecture patterns.

---

## 8. Optimizations & Performance

### 8.1 — Stream Everything

This is the single biggest lever for perceived performance. Do not wait for the entire agent loop to finish before showing output. Stream the agent's progress in real time to stderr:

```
🔍 "where is the auth middleware?"

  ⠋ Searching file structure...
  ├─ find_files **/*auth*  →  found 4 files
  ⠋ Grepping for middleware patterns...
  ├─ grep "middleware" src/auth/  →  12 matches
  ⠋ Reading src/middleware/auth.ts...
  ├─ read_file src/middleware/auth.ts:1-60

📍 Results (3 turns, 1.8s):
  src/middleware/auth.ts:14-58     verifyToken middleware
  src/config/auth.ts:3-18         JWT configuration
  src/routes/index.ts:7           middleware registration
```

The user sees progress from the first 200ms. Even though the total time might be 4 seconds, the tool feels active rather than frozen.

The OpenAI Go SDK provides streaming via `NewStreaming()` and a `ChatCompletionAccumulator` that assembles streamed chunks into complete tool calls. Use this. As each tool call is assembled, print the tool name and arguments to stderr. As each tool result comes back, print a summary. The final results go to stdout.

This trace is shown by default when stdout is a TTY. It is hidden when stdout is a pipe (because the user is scripting). It can be forced on with `--verbose` or forced off with `--quiet`.

### 8.2 — Front-Load Project Context

Before the agent loop starts, generate a project tree snapshot and inject it into the system prompt. This costs tokens upfront but saves entire turns. A project tree at depth 2 for a medium repo is roughly 200-400 tokens. That is far cheaper than the LLM spending a full turn calling list_dir, waiting for the round trip, and processing the result.

The tree should be generated once at startup and cached in memory for the duration of the run. It should include directory names with file counts (not individual file listings for large directories), manifest file names, and top-level config files.

For repositories over 1000 files, consider injecting only the first two levels and letting the LLM drill down with list_dir as needed. For very small repos (under 50 files), consider listing every file in the prompt — the token cost is negligible and it can allow the LLM to skip its discovery phase entirely.

### 8.3 — Parallel Tool Execution

When the LLM requests multiple tool calls in a single turn, execute them concurrently using goroutines. A common pattern is the LLM calling grep with one pattern and find_files with another in the same turn. There is no reason to execute these sequentially — they are independent read-only operations.

Use a WaitGroup or errgroup to coordinate. Collect all results. Return them to the LLM in the same order they were requested.

### 8.4 — Rich Tool Results

Help the LLM by returning more context in tool results than the bare minimum. Instead of returning just file paths from grep, return the matching line with its content:

```
src/middleware/auth.ts:14 — export function verifyToken(req, res, next) {
src/middleware/auth.ts:32 — export function requireAdmin(req, res, next) {
```

This line preview helps the LLM decide what to read next without needing another turn. The extra tokens in the result are cheaper than the tokens in an additional LLM round trip.

### 8.5 — Fast Failure

If the first grep returns zero results and the first find_files returns zero matches, the LLM should recognize this quickly and either try a different approach or submit an empty answer. The system prompt instructs it to be honest when nothing is found.

At the harness level, if the LLM has called tools three times with zero results each time, consider injecting a nudge message: "Your last 3 searches returned no results. Consider trying different search terms or submitting what you have." This prevents the LLM from burning all 10 turns on a wild goose chase.

### 8.6 — Ollama Context Window Configuration

Ollama defaults to a 2048 token context window, which is far too small for a multi-turn agent loop. This must be overridden. Create a Modelfile:

```
FROM ministral-3:3b
PARAMETER num_ctx 16384
```

Then build the custom model with `ollama create probe-ministral -f Modelfile`. Use this model name in the probe config. 16k is sufficient for a 5-8 turn search session. Ministral 3B's native 256k context means this can be expanded to 32k or beyond if needed, though 16k should be the starting point.

### 8.7 — Token Budget Awareness

Track cumulative token usage across turns. The OpenAI API returns usage data in each response. If the conversation approaches 80% of the context window, the harness should force the LLM to submit its best answer on the next turn by temporarily setting tool_choice to the submit_answer function specifically. This prevents silent degradation at the end of long sessions where the model starts losing context.

For Ministral 3B at 16k context: with a system prompt of ~500 tokens and a user query of ~50 tokens, that leaves roughly 15k tokens for tool results and LLM responses. Each tool call and result pair is approximately 500-2000 tokens depending on truncation settings. This budget supports 5-8 comfortable turns.

### 8.8 — Strict Mode on Tool Schemas

When the provider supports it, enable strict mode on all tool schemas. This forces the model to produce valid JSON arguments that conform to the schema. It eliminates an entire class of failures — malformed glob patterns, missing required parameters, wrong parameter types. Ollama's OpenAI-compatible endpoint may not support strict mode, but the schemas should be defined with it enabled so that when the tool is used against a provider that does support it (OpenAI, for instance), it activates automatically.

---

## Appendix A: CLI Interface

```
probe [flags] <query>

Flags:
  --model <name>         Model name (default: from config or "ministral-3:3b")
  --base-url <url>       API base URL (default: from config or "http://localhost:11434/v1")
  --max-turns <n>        Maximum agent turns (default: 10)
  --dir <path>           Project directory to search (default: current directory)
  --json                 Output results as JSON
  --format <fmt>         Output format: "human" (default), "json", "paths"
  --verbose, -v          Show agent search trace on stderr
  --quiet, -q            Suppress all output except exit code
  --version              Print version and exit
  --help, -h             Print help and exit

Environment variables:
  PROBE_API_KEY          API key for the LLM provider
  PROBE_MODEL            Default model name
  PROBE_BASE_URL         Default API base URL
  PROBE_MAX_TURNS        Default max turns
```

## Appendix B: Config File Format

```toml
# .probe.toml (project root) or ~/.config/probe/config.toml

[api]
model = "ministral-3:3b"
base_url = "http://localhost:11434/v1"
api_key_env = "PROBE_API_KEY"       # name of env var holding the key

[search]
max_turns = 10
max_results_per_grep = 30
max_file_read_lines = 200
respect_gitignore = true

[output]
default_format = "human"            # "human", "json", "paths"
show_reasons = true                 # include reason text in results
```

## Appendix C: Validation Test Protocol

Before declaring either model ready, run these five queries against a known codebase and verify:

1. **Regex construction.** Query: "where is the authenticate function defined?" — The model should construct something like `function.*authenticate` or `def authenticate` or `func.*[Aa]uthenticate`, not pass the natural language query as a grep pattern.

2. **Convergence.** Query: "where is the database connection pool?" — The model should find the answer within 3-5 turns and call submit_answer. It should not loop endlessly.

3. **Tool selection.** Query: "find all Python files in the tests directory" — The model should use find_files, not grep.

4. **Zero-result handling.** Query: "where is the quantum flux capacitor?" — The model should try 2-3 searches, find nothing, and submit an empty answer honestly. It should not hallucinate results.

5. **Multi-file reasoning.** Query: "how is the rate limiting middleware applied to routes?" — The model should find the middleware definition, the route registration, and the config. This requires at least 3 tool calls across different files.

If Ministral 3B passes all five, it is the production validation model. If it fails on tests 1 or 2, fall back to Qwen3 8B. If it fails only on test 5, that is acceptable for v0 — multi-file reasoning is a stretch goal for 3B models.
