# Harper

Harper is a terminal-based coding agent, in the same space as Claude Code, Codex, and pi. It runs an agent loop where a configurable **brain** model drives the conversation and tool use, and a configurable **subtask** model handles delegated, token-heavy work — both provider-agnostic, so the same code runs against local models (e.g. Ollama) or hosted ones without a rewrite.

> **Status: under construction.** Harper is not yet runnable end to end — there is no CLI entrypoint or agent loop wired up yet. See [Current status](#current-status) below for what's implemented so far.

## Design goals

- A tool-calling agent loop: read a request, call the brain model with tools, execute tool calls, loop to a final answer.
- Independently configurable brain/subtask models and providers — no hardcoded model or vendor.
- Explicit delegation: the brain can hand off a self-contained, token-heavy task to the subtask model via a `Delegate` tool, which runs its own bounded tool loop and returns a summary.
- Sandboxed command execution: tool calls that run shell commands go through an `Executor` interface, backed by a Docker sandbox for standalone use (with a network-denied-by-default policy and container resource limits) or a direct local executor when Harper is embedded inside something that already provides isolation.
- A non-interactive run mode, so Harper can be driven by another harness or CI job, not just an interactive terminal session.
- MCP client support, so external MCP servers' tools extend Harper's tool set without any code changes.

## Current status

Implemented so far:

- **`internal/llm`** — provider-agnostic message/tool types and an `OllamaProvider` implementation, including correct multi-turn tool-call history round-tripping.
- **`internal/config`** — YAML config loading with safe defaults (sandboxed-by-default network policy, non-zero container resource limits).
- **`internal/executor`** — the `Executor` interface and a `LocalExecutor` implementation for running shell commands directly.

Not yet built: the Docker-backed executor, the built-in tool set (file read/write/edit/grep/glob), the agent loop itself, delegation, MCP integration, and any CLI entrypoint. There is nothing to run yet.

## Development

Requires Go 1.23+.

```bash
go build ./...
go test ./...
go vet ./...
```

Each package's tests are self-contained and don't require any external services to run.
