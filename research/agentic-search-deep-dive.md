# Agentic Code Search CLI — Deep Dive on the 5 Core Questions

---

## 1. How Do I Make It Feel Fast?

This is a UX question disguised as an engineering question. The actual LLM roundtrips will take 1-3 seconds each, and you might have 3-8 of them. You can't eliminate that latency. But you can make it *feel* instant.

### Stream Everything

The single biggest lever: **stream the agent's thinking in real time**. Don't wait for the final answer. Show every tool call as it happens:

```
🔍 "where is the auth middleware?"

  ⠋ Searching file structure...
  ├─ find_files **/*auth*  →  found 4 files
  ⠋ Grepping for middleware patterns...
  ├─ grep "middleware" src/auth/  →  12 matches
  ⠋ Reading src/middleware/auth.ts...
  ├─ read_file src/middleware/auth.ts:1-60

📍 Results (3 turns, 1.8s):
  src/middleware/auth.ts:14-58     — verifyToken middleware
  src/config/auth.ts:3-18         — JWT configuration
  src/routes/index.ts:7           — middleware registration
```

The user sees progress from the first 200ms. Even though the total time might be 4 seconds, it *feels* like the tool is working hard, not frozen.

### Make Tools Fast, Not the LLM

You can't control LLM latency, but you can make sure tools never block:

- **ripgrep** is already ~millisecond-fast on most repos. It's not the bottleneck.
- **File reads** are instant. Pre-read into memory if needed.
- **Directory listing** — cache the project tree on first run. Invalidate on file changes. This avoids the agent wasting a turn just to understand the repo layout.

### Front-load Context (The Big Trick)

Before the agent loop even starts, **inject a project snapshot into the system prompt**:

```
Project structure (auto-generated):
├── src/
│   ├── middleware/ (3 files)
│   ├── routes/ (8 files)
│   ├── models/ (5 files)
│   ├── utils/ (12 files)
│   └── config/ (4 files)
├── tests/ (23 files)
├── package.json (deps: express, prisma, jwt...)
└── tsconfig.json
```

This lets the LLM make a *smarter first tool call*. Instead of wasting Turn 1 on `list_dir /`, it can go straight to `grep "middleware" src/middleware/`. One fewer turn = 1-2 seconds saved.

You could even include a lightweight "code map" — function/class names extracted from tree-sitter — for small-to-medium repos. This is a token investment that pays back in fewer turns.

### Parallel Tool Calls

The OpenAI API supports parallel tool calling — the model can request `grep` and `find_files` in the *same turn*. You execute both concurrently, return both results. This is free speed. Make sure your harness supports it and executes tools in parallel (not sequentially).

### Set Expectations

Show a progress indicator. Show turn count. Show elapsed time. Users tolerate latency well when they can see progress. What they can't tolerate is a blank screen.

### The Speed Tier Strategy

Not every query needs the same model or depth:

| Query Type | Turns Needed | Model Tier |
|---|---|---|
| "where is X defined?" | 1-2 | Fast/cheap model |
| "how does auth flow work?" | 4-6 | Mid-tier model |
| "find all places where we handle rate limiting" | 6-10 | Full model |

Consider auto-detecting query complexity and routing accordingly. Simple "find definition" queries can use fewer max turns and a faster model.

---

## 2. How Should the Agent Harness Work?

The harness is everything between the user's query and the LLM's final answer. It's the orchestration layer. Braintrust's engineering blog nailed it: *"An agent is just a system prompt and a handful of well-crafted tools."* The harness is the while loop around that.

### The Core Loop (Keep It Stupid Simple)

```
function agentSearch(query, maxTurns = 10):
    messages = [systemPrompt, { role: "user", content: query }]
    
    for turn in 1..maxTurns:
        response = llm.chat(messages, tools=TOOLS, stream=true)
        
        if response.hasToolCalls:
            results = executeToolsInParallel(response.toolCalls)
            messages.append(response.assistantMessage)
            messages.append(...toolResultMessages(results))
        else:
            // LLM returned a final text answer (no tool calls)
            return parseResults(response.text)
    
    // Hit max turns — return whatever we have
    return parsePartialResults(messages)
```

That's it. ~20 lines of real logic. Everything else is detail around this core.

### The Details That Actually Matter

**a) Tool Result Truncation**

This is the #1 thing that will bite you. A `grep` call can return 500 lines. If you dump all of that into the context window, you'll:
- Blow your token budget in 2 turns
- Confuse the LLM with too much noise

**Always truncate tool results.** Cap grep at 30-50 results. Cap file reads at ~200 lines. If the result is truncated, tell the LLM: `"(showing 50 of 312 matches — refine your search to narrow down)"`. This is a critical feedback signal — it teaches the LLM to be more specific.

**b) Token Budget Awareness**

Track cumulative token usage across turns. If you're approaching the context window limit (e.g., 80% of 128k), you have options:
- Summarize earlier tool results (compaction)
- Drop the oldest tool call/result pairs
- Force the LLM to return its best answer now

Anthropic's research on long-running agents found that **compaction** (summarizing old context) is essential for reliability. For a search tool with <10 turns this is less critical, but you should still track it.

**c) Error Handling in Tools**

Tools will fail. File not found. Grep returns zero results. Permission denied. Your harness must return these errors *to the LLM* as tool results, not swallow them:

```json
{
  "role": "tool",
  "content": "Error: file 'src/auth/middleware.ts' not found. Similar files: src/middleware/auth.ts, src/auth/index.ts"
}
```

The LLM will self-correct. This is one of the superpowers of agentic search — the agent can recover from bad guesses. **Never crash on tool errors. Always feed them back.**

**d) The "Done" Signal**

How does the LLM signal it's done searching? Two approaches:

1. **Implicit**: If the LLM returns a text response with no tool calls, it's done. This is the simplest and works well.
2. **Explicit**: Add a `submit_results` tool that the LLM calls with structured output. This gives you a clean, parseable final answer.

I'd recommend approach 2 for a search tool. Define a `submit_answer` tool:

```json
{
  "name": "submit_answer",
  "description": "Submit the final search results. Call this when you've found all relevant files.",
  "parameters": {
    "results": [
      {
        "file": "string — relative file path",
        "start_line": "int",
        "end_line": "int",
        "reason": "string — why this is relevant"
      }
    ],
    "summary": "string — brief summary of what was found"
  }
}
```

This forces structured output. No parsing free-text. Clean handoff to the CLI for display.

**e) Guardrails**

- **Max turns**: Hard cap. 10 is reasonable for search. 
- **Max tokens per turn**: Limit tool result sizes.
- **Max total tokens**: Budget cap for the whole search session.
- **Read-only**: Your tools should NEVER write, execute, or modify anything. This is a search tool. Enforce this at the harness level, not the prompt level.
- **Respect .gitignore**: Filter all tool results through gitignore rules. Never surface `node_modules/`, `.env`, etc.
- **Path sandboxing**: Tools should only access files within the project root. No `../../etc/passwd`.

---

## 3. What Should the System Prompt Look Like?

The system prompt is the soul of the agent. It should be concise, opinionated, and give the LLM a clear mental model of what it's doing.

### Proposed System Prompt

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

## Output

When you've found all relevant code, call submit_answer with:
- Each relevant file, its line range, and why it's relevant
- A brief summary of what you found

Do not explain your search process. Just find the code.
```

### Key Principles Behind This Prompt

**a) Give it a workflow, not rules**

"Start broad, then refine" is more useful than 20 specific rules. The LLM will figure out the specifics. This is the lesson from every successful agent deployment — guide the *strategy*, not the *tactics*.

**b) Inject dynamic context**

The `{PROJECT_TREE}` placeholder gets filled at runtime with the actual project structure. This is the front-loaded context from section 1. You can also inject:
- Language/framework hints (detected from package.json, Cargo.toml, etc.)
- Recently modified files (from `git log`)
- File count / repo size (so the LLM knows if it's dealing with 50 files or 50,000)

**c) "Do not explain your search process"**

This is critical for speed. Without this, the LLM will spend tokens narrating: *"I'll start by looking at the project structure to understand..."* That's wasted tokens and wasted time. You want it to go straight to tool calls.

**d) The "NEVER guess" rule**

LLMs will hallucinate line numbers if you let them. The explicit instruction to always verify by reading the file prevents this. This is worth the extra turn.

### Prompt Variants for Different Query Types

You might want slight variations:

- **Definition search** ("where is X defined?"): Prompt emphasizes grep for function/class definitions, reading the exact file.
- **Flow search** ("how does X work?"): Prompt emphasizes tracing imports, following call chains, reading multiple related files.
- **Usage search** ("where is X used?"): Prompt emphasizes grep for references, checking tests, checking imports.

You could auto-detect query type with a simple classifier (or even a regex) and swap in the appropriate variant.

---

## 4. Can I Make This Work with Weaker but Faster Models?

**Yes, but with constraints.** This is actually one of the most interesting design questions.

### The Model Landscape for Tool Calling (Early 2026)

| Model | Tool Calling Quality | Speed | Cost |
|---|---|---|---|
| GPT-4o / Claude Sonnet | Excellent | Medium | $$ |
| GPT-4o-mini / GPT-5-mini | Good | Fast | $ |
| Qwen3-Coder 32B (local) | Good | Fast (local) | Free |
| Qwen3-4B / Phi-4 | Usable | Very fast | Free |
| Mistral Small 3 (24B) | Good | Fast | $ |
| GPT-5-nano / gpt-oss-20B | Decent | Fast | $/Free |

### What Makes Smaller Models Struggle

Tool calling requires the model to:
1. Understand the query semantically
2. Plan which tool to use
3. Construct valid JSON arguments
4. Interpret tool results
5. Decide: search more or submit answer?

Steps 2-3 are where small models fail. They'll:
- Call the wrong tool
- Construct invalid arguments (malformed glob patterns, wrong regex)
- Loop endlessly without converging
- Miss relevant results and submit too early

### How to Compensate (Harness-Level Strategies)

**a) Fewer, simpler tools**

For a weak model, reduce the tool count. Instead of 5 tools, give it 3:
- `search` (combined grep + find)
- `read_file`
- `submit_answer`

Fewer choices = fewer mistakes. You can handle the grep-vs-glob decision in the tool implementation based on the query pattern.

**b) Strict mode (always)**

OpenAI's `strict: true` on tool schemas forces the model to produce valid JSON. This eliminates malformed arguments entirely. Always use this.

**c) Tighter max turns**

Weak models degrade with more turns — they lose the thread. Cap at 5 turns instead of 10. Force convergence.

**d) Richer tool results**

Help the model by returning more context in tool results. Instead of just filenames, return:
```
src/middleware/auth.ts:14 — export function verifyToken(req, res, next) {
src/middleware/auth.ts:32 — export function requireAdmin(req, res, next) {
```

The line preview helps the model decide what to read next without needing another turn.

**e) Simpler system prompt**

Strip the prompt down. Remove workflow suggestions. Be more directive:
```
Search the codebase for files matching the user's query.
Use `search` to find files, `read_file` to verify, then `submit_answer`.
Be precise with line numbers. Maximum 5 tool calls.
```

**f) The "two-model" strategy**

Use a fast/cheap model (GPT-4o-mini, Qwen3-4B) for simple queries ("where is X defined?") and a stronger model for complex ones ("how does the auth flow work across services?"). You can detect complexity from the query length, presence of words like "how", "flow", "across", etc.

### The Honest Assessment

A Qwen3-4B or GPT-5-nano can handle "find where function X is defined" style queries surprisingly well — because it only needs 1-2 turns and basic grep pattern construction. For anything requiring multi-step reasoning across files, you need at least a GPT-4o-mini / Qwen3-32B class model. The sweet spot for a general-purpose code search tool is **GPT-4o-mini** or equivalent — fast, cheap, and reliable enough for 90% of queries.

---

## 5. What Should the Soul of the Search Application Feel Like?

This is the most important question. Tools succeed or fail on *feel*, not features.

### The Feeling: "A Senior Dev Who Knows This Repo"

When you ask a senior dev who's been on a codebase for 2 years "where's the auth middleware?", they don't:
- Give you a lecture on authentication
- List every file in the project
- Ask you 5 clarifying questions

They say: **"It's in src/middleware/auth.ts, around line 14. The JWT config is in src/config/auth.ts if you need that too."**

That's the soul. Fast. Precise. Anticipates follow-ups. Doesn't over-explain.

### Design Principles

**a) Respect the developer's time**

Developers use search tools 50+ times a day. This can't be a "conversation". It should feel like `grep` but smarter — fire and forget.

```
$ cs "database connection pool"
→ src/db/pool.ts:12-45
→ src/config/database.ts:3-18
```

Done. No preamble. No "I found the following results for you." Just the answer.

**b) Show your work, but briefly**

Developers want to trust the results. Show the search path in a compact way:

```
$ cs "rate limiting logic" --verbose
  ↳ grep "rate.limit" → 8 files
  ↳ grep "throttle" → 3 files  
  ↳ read src/middleware/rateLimit.ts
  ↳ read src/config/limits.ts

→ src/middleware/rateLimit.ts:7-42    Rate limiter middleware (sliding window)
→ src/config/limits.ts:1-15          Rate limit configuration per route
→ src/middleware/index.ts:12          Where rate limiter is applied
```

The `--verbose` flag shows the journey. Default is just results.

**c) Err on the side of showing too little**

If you show 15 results, none of them feel relevant. If you show 3, all of them feel precise. **Fewer, better results.** The LLM should be opinionated about what's actually relevant, not dump everything that matched.

**d) The "reason" field matters**

Each result should have a one-line explanation of *why* it's relevant:

```
→ src/middleware/auth.ts:14-58     Defines verifyToken() and requireAdmin()
→ src/utils/jwt.ts:3-22           JWT signing and verification helpers
```

This turns raw file:line output into something a human can scan in <2 seconds and know if they found what they needed.

**e) Zero config to start**

The most important UX decision: it should work with zero configuration in any project directory. Auto-detect language, respect gitignore, use sensible defaults. Configuration is for power users, not the first run.

```
$ cd my-project
$ cs "auth middleware"
# Just works. No setup, no indexing, no config file.
```

Compare this to any RAG-based tool: install vector DB, run indexing, wait, configure, then search. Your tool's superpower is that it works *instantly* on any repo.

**f) Fast failure**

If it can't find anything, say so immediately. Don't burn 10 turns trying harder:

```
$ cs "quantum flux capacitor"
✗ No relevant code found for "quantum flux capacitor"
  Searched 247 files across 12 directories.
  Try rephrasing or being more specific.
```

**g) Composable with existing workflows**

Output should be pipe-friendly:

```bash
# Open results in editor
cs "auth middleware" --json | jq -r '.[0].file' | xargs code -g

# Feed into another tool
cs "database migrations" --format=paths | xargs wc -l
```

### The Anti-Patterns (What It Should NOT Feel Like)

- **Not a chatbot.** No "How can I help you today?" No multi-turn conversation.
- **Not a code explainer.** It finds code, it doesn't explain code. (That's a different tool.)
- **Not a slow indexer.** No "building index... please wait." 
- **Not noisy.** No banners, no ASCII art, no sponsorship messages. Clean output.
- **Not opaque.** The user should always be able to see *how* it searched (via --verbose).

### The Name Matters

The name should feel like a Unix tool, not a SaaS product. Short. Memorable. Lowercase.

Some ideas:
- `cs` (code search — 2 chars, fast to type)
- `seek` (natural language search)
- `probe` (searching/investigating)
- `scout` (explores codebases)
- `ag` (the silver searcher vibes, but agentic — though this name is taken)
- `hound` (hunts through code)

I'd lean toward `cs` or `seek` — they're short and convey exactly what the tool does.

---

## Summary: The DNA of This Tool

| Aspect | Principle |
|---|---|
| Speed | Stream everything, front-load context, parallel tools |
| Harness | While loop + tools. Truncate results. Feed errors back. Structured output. |
| System prompt | Workflow-oriented, dynamic context, "don't narrate" |
| Weak models | Fewer tools, strict mode, richer results, tight turn limits |
| Soul | Senior dev energy. Precise. Fast. Respects your time. Zero config. |
