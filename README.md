# Harper

Harper is a terminal-based coding agent, in the same space as Claude Code, Codex, and pi. It runs an agent loop where a configurable **brain** model drives the conversation and tool use, and a configurable **subtask** model handles delegated, token-heavy work — both provider-agnostic, so the same code runs against local models (e.g. Ollama) or hosted ones without a rewrite.

## Design goals

- A tool-calling agent loop: read a request, call the brain model with tools, execute tool calls, loop to a final answer.
- Independently configurable brain/subtask models and providers — no hardcoded model or vendor.
- Explicit delegation: the brain can hand off a self-contained, token-heavy task to the subtask model via a `Delegate` tool, which runs its own bounded tool loop and returns a summary.
- Sandboxed command execution: tool calls that run shell commands go through an `Executor` interface, backed by a Docker sandbox for standalone use (with a network-denied-by-default policy and container resource limits) or a direct local executor when Harper is embedded inside something that already provides isolation.
- A non-interactive run mode, so Harper can be driven by another harness or CI job, not just an interactive terminal session.
- MCP client support, so external MCP servers' tools extend Harper's tool set without any code changes.

## Current status

Harper builds and runs end to end. The full loop — Ollama-backed brain/subtask models, all six core tools (`Read`/`Write`/`Edit`/`Grep`/`Glob`/`Bash`), `Delegate`, MCP tool merging, Docker or local sandboxing, an interactive REPL, and a non-interactive `run` mode with JSONL session logging — is implemented and covered by tests.

Not yet built: an `AnthropicProvider` (config validates the field but only `"ollama"` is implemented), the network domain-allowlist (v1 ships a coarser `--network none`/`bridge` toggle), and the separate `harper-terminal-bench` adapter for running Harper inside an external eval harness.

## Usage

Requires a running Ollama server with a model that supports native tool calling.

Interactive REPL:

```bash
./harper
```

Non-interactive, single instruction:

```bash
./harper run "<instruction>" --workdir /path/to/project --sandbox local --max-turns 40 --log session.jsonl
```

Both read a config file via `--config path/to/harper.yaml` (optional; sane defaults are built in). Config controls the brain/subtask model and provider, Ollama connection settings, sandbox mode and Docker options, and any MCP servers to connect to.

## Development

Requires Go 1.23+.

```bash
go build ./...
go test ./...
go vet ./...
```

Each package's tests are self-contained and don't require any external services to run — except `internal/executor`'s `DockerExecutor` tests, which skip cleanly if Docker isn't installed or the daemon isn't reachable.

Build the binary with:

```bash
go build -o harper ./cmd/harper
```
