# Agentic Code Search CLI — Brainstorm

## The Idea

A dead-simple CLI tool that takes a natural language query about a codebase and returns relevant files with line numbers. No embeddings, no vector DB, no RAG infrastructure — just an agent loop with a few sharp tools and an OpenAI-compatible API.

```
$ codesearch "where is the user authentication middleware defined?"

→ src/middleware/auth.ts:14-58
→ src/utils/jwt.ts:3-22
→ src/routes/login.ts:7-12
```

---

## What the Industry is Doing Right Now

### The Big Shift: RAG is Out, Agentic Search is In

The most significant finding from the research: **the industry has largely moved away from embeddings/RAG for code search**, and toward agentic search using simple CLI tools.

- **Claude Code** started with RAG + a local vector DB, then ditched it. A Claude engineer confirmed on HN: *"We found pretty quickly that agentic search generally works better. It is also simpler and doesn't have the same issues around security, privacy, staleness, and reliability."*
- **Augment Code** (top SWE-Bench) found the same thing: *"We explored adding various embedding-based retrieval tools, but found that grep and find were sufficient."*
- **Cursor** is the notable exception — they use a hybrid semantic-lexical index and it works well, but it requires persistent infrastructure (indexing, vector DB, continuous re-indexing).

The consensus: **Modern LLMs are smart enough to compose grep/find/read iteratively and achieve comparable accuracy to RAG, with zero infrastructure overhead.**

### The Three-Tier Search Stack (What Actually Works)

Based on Cline, Claude Code, and other top performers, the winning tool combination is:

| Tool | Purpose | Speed |
|------|---------|-------|
| **ripgrep** (`rg`) | Text/regex search across files — find exact strings, patterns | Blazing fast |
| **glob/find** | File discovery — find files by name/path patterns | Instant |
| **ast-grep / tree-sitter** | Structural code search — find functions, classes, imports by AST | Fast |
| **file read** | Read specific file contents or line ranges | Instant |
| **directory listing** (`ls`/`tree`) | Understand project structure | Instant |

The LLM acts as the **orchestrator**: it decides which tool to use, interprets results, refines the search, and repeats until it has enough context. Intelligence emerges from the LLM's reasoning, not from the retrieval mechanism.

### Where Embeddings Still Win

Cursor's blog showed that semantic search helps when the query uses **different terminology** than the code. Example: searching for "authentication" when the code uses `verifyToken`. Grep can't bridge that gap without keyword expansion. But an LLM orchestrating grep *can* — by trying synonyms and related terms across multiple turns.

### Token Cost Tradeoff

The honest downside of agentic search: it burns more tokens than a single vector lookup. Every grep call, every file read, every refinement step is tokens. But the tradeoff is:
- Zero infrastructure (no vector DB, no indexing pipeline, no staleness)
- Works on any codebase instantly (no setup time)
- Always up-to-date (searches live files)
- No security concerns (code never leaves the machine)

---

## Architecture Proposal

### Core Components

```
┌─────────────────────────────────────────┐
│                CLI Interface            │
│  (parse args, display results, config)  │
└──────────────────┬──────────────────────┘
                   │
┌──────────────────▼──────────────────────┐
│              Agent Loop                 │
│  (max N turns, tool dispatch, state)    │
└──────────────────┬──────────────────────┘
                   │
        ┌──────────┼──────────┐
        ▼          ▼          ▼
┌─────────┐ ┌──────────┐ ┌──────────────┐
│ OpenAI  │ │  Tool    │ │   Output     │
│ API     │ │  Runner  │ │   Formatter  │
│ Client  │ │          │ │              │
└─────────┘ └──────────┘ └──────────────┘
```

### The Agent Loop (Heart of the System)

```
1. User provides natural language query
2. System prompt instructs LLM: "You are a code search agent. 
   Use the provided tools to find the files and line numbers 
   relevant to the user's query. Return a final answer as a 
   structured list of file:line_range results."
3. LLM decides which tool(s) to call
4. Tool results are appended to conversation
5. Repeat until:
   - LLM returns final answer (no more tool calls), OR
   - Max turns reached (e.g., 10 turns)
6. Parse and display results
```

### Tool Definitions (OpenAI Function Calling Format)

These are the tools we'd expose to the LLM:

**1. `grep` — Search file contents**
```json
{
  "name": "grep",
  "description": "Search for a text pattern or regex across files in the codebase. Returns matching lines with file paths and line numbers.",
  "parameters": {
    "pattern": "string — the search pattern (supports regex)",
    "glob": "string? — optional file glob to narrow search (e.g. '*.ts')",
    "max_results": "int? — max number of results to return (default 50)"
  }
}
```

**2. `find_files` — Discover files by name/path**
```json
{
  "name": "find_files",
  "description": "Find files and directories matching a glob pattern. Use to discover project structure.",
  "parameters": {
    "pattern": "string — glob pattern (e.g. '**/auth*', '*.py')",
    "type": "string? — 'file' or 'dir' (default both)"
  }
}
```

**3. `read_file` — Read file contents**
```json
{
  "name": "read_file",
  "description": "Read the contents of a specific file. Can read full file or a line range.",
  "parameters": {
    "path": "string — relative path to the file",
    "start_line": "int? — start line (1-indexed)",
    "end_line": "int? — end line (inclusive)"
  }
}
```

**4. `list_dir` — List directory contents**
```json
{
  "name": "list_dir",
  "description": "List files and subdirectories in a directory. Shows the project structure.",
  "parameters": {
    "path": "string? — directory path (default: root)",
    "depth": "int? — max depth to traverse (default 2)"
  }
}
```

**5. `ast_search` — Structural code search (stretch goal)**
```json
{
  "name": "ast_search",
  "description": "Search for code structures: function/class/method definitions, imports, exports. Uses tree-sitter AST parsing.",
  "parameters": {
    "query": "string — what to search for (e.g. 'function authenticate')",
    "language": "string? — language hint (auto-detected if omitted)"
  }
}
```

---

## OpenAI API — Tool Calling Mechanics

The API supports this well. Key details:

- **Endpoint**: `POST /v1/chat/completions` (Chat Completions) or the newer `/v1/responses` (Responses API)
- **Tools param**: Pass tool definitions in `tools` array
- **Strict mode**: Set `strict: true` for guaranteed schema adherence (recommended)
- **Parallel tool calls**: Model can call multiple tools in one turn. Can disable with `parallel_tool_calls: false`
- **Tool choice**: `auto` (model decides), `required` (must call a tool), `none` (no tools), or a specific function name
- **Flow**: Model returns `tool_calls` → you execute them → send back `tool` role messages with results → model continues

Since we want OpenAI-compatible API support, this means any provider that implements the same interface works: OpenAI, Anthropic (via proxy), Ollama, vLLM, Together, Groq, OpenRouter, etc.

### Chat Completions vs Responses API

- **Chat Completions** (`/v1/chat/completions`): The standard. Widest compatibility across providers. Use this.
- **Responses API** (`/v1/responses`): Newer OpenAI-specific. Supports built-in tools, persistent reasoning. Not compatible with other providers.

**Recommendation**: Target Chat Completions API for maximum portability.

---

## Tech Stack Options

### Language Choice

| Language | Pros | Cons |
|----------|------|------|
| **Go** | Single binary, fast, great CLI ecosystem (cobra), easy cross-compile | Less flexible for rapid iteration |
| **Rust** | Single binary, fastest, ripgrep is Rust | Steeper dev time |
| **TypeScript/Node** | Fastest to prototype, huge ecosystem, OpenAI SDK built-in | Requires Node runtime |
| **Python** | Easiest to build, great LLM ecosystem | Requires Python runtime, slow |

**Recommendation for v0**: Go or TypeScript. Go if we want a zero-dep distributable binary. TypeScript if we want speed-to-ship.

### Key Dependencies

- **ripgrep** (`rg`): Already installed on most dev machines. Shell out to it.
- **tree-sitter** / **ast-grep**: Optional but powerful. Can add as a stretch goal.
- **OpenAI SDK**: Use the official one for the target language, or just raw HTTP (it's simple enough).

---

## CLI UX Design

```bash
# Basic usage
$ codesearch "where is the database connection pool configured?"

# With options
$ codesearch "auth middleware" \
  --model gpt-4o \
  --api-key $OPENAI_API_KEY \
  --base-url https://api.openai.com/v1 \
  --max-turns 8 \
  --dir ./my-project

# Output
🔍 Searching: "where is the database connection pool configured?"

  Turn 1: grep "pool" "connection" -- *.ts *.js
  Turn 2: read_file src/db/pool.ts
  Turn 3: grep "createPool" -- src/db/**

📍 Results:
  src/db/pool.ts:12-45        — ConnectionPool class definition
  src/config/database.ts:3-18 — Pool configuration options
  src/db/index.ts:7           — Pool initialization

# Config file (~/.codesearch.toml or .codesearch.toml in project root)
[api]
model = "gpt-4o-mini"
base_url = "https://api.openai.com/v1"
api_key_env = "OPENAI_API_KEY"  # read from env var

[search]
max_turns = 10
max_results_per_tool = 50
respect_gitignore = true
```

---

## Open Questions / Decisions to Make

1. **Language**: Go vs TypeScript for v0?
2. **AST search**: Include tree-sitter/ast-grep from day 1, or add later?
3. **Output format**: Human-readable, JSON, or both?
4. **Streaming**: Show tool calls in real-time, or just the final answer?
5. **Caching**: Cache file listings / project structure between runs?
6. **Config**: Env vars only, or support a config file?
7. **Scope**: Just search, or also allow "explain this code" type queries?
8. **Cost guard**: Show estimated token usage? Hard limit on turns?
9. **Ignore patterns**: Respect `.gitignore`? Custom ignore file?
10. **Diff-awareness**: Should it understand git diff / staged changes?

---

## MVP Scope (What to Build First)

**Phase 1 — Walking skeleton:**
- CLI that takes a query string
- Connects to any OpenAI-compatible API  
- Agent loop with 4 tools: `grep`, `find_files`, `read_file`, `list_dir`
- Shells out to `ripgrep` and standard Unix tools
- Returns file:line results
- Config via env vars + simple config file
- Respects `.gitignore`

**Phase 2 — Polish:**
- Streaming output (show agent's thinking)
- ast-grep / tree-sitter integration
- Token usage tracking
- Multiple output formats (human, JSON, markdown)
- Project-level config file

**Phase 3 — Power features:**
- MCP server mode (expose as MCP tool for other agents)
- Watch mode (re-search on file changes)
- Session history (resume/refine previous searches)
- Custom tool plugins
