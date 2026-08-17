# Harper

Harper is a terminal-based coding agent. It runs an agent loop where a configurable **brain** model drives the conversation and tool use, and a configurable **subtask** model handles delegated, token-heavy work. Both roles are provider-agnostic, so the same binary runs against local models (via Ollama) or hosted ones without a rewrite.

## Features

- **Tool-calling agent loop** — reads a request, calls the brain model with tools, executes tool calls, loops to a final answer.
- **Independent brain/subtask models** — configure each role's provider, model, and reasoning effort separately, or override both at once from the command line.
- **Delegation** — the brain can hand a self-contained, token-heavy task off to the subtask model via a `Delegate` tool, which runs its own bounded tool loop and returns a summary.
- **Six built-in tools** — `Read`, `Write`, `Edit`, `Grep`, `Glob`, and `Bash`.
- **Two execution modes** — direct execution by default (no sandbox, no startup cost), or an opt-in Docker sandbox for untrusted projects, with network access denied by default and container resource limits.
- **MCP client support** — connect to external MCP servers and their tools are merged into Harper's tool set automatically.
- **Two interfaces** — an interactive REPL, and a non-interactive `run` mode with structured JSONL session logging, so Harper can be driven by scripts or other tooling.

## Installation

Requires Go 1.23+ and a running [Ollama](https://ollama.com) server with a model that supports native tool calling (e.g. `qwen3-coder`, `gpt-oss`).

```bash
git clone git@github.com:jalpatel11/Harper.git
cd Harper
go build -o harper ./cmd/harper
```

## Quick start

Pull a tool-calling-capable model if you don't already have one:

```bash
ollama pull qwen3-coder:30b
```

Run Harper interactively in a project directory:

```bash
./harper
```

Or give it a single instruction and let it exit when done:

```bash
./harper run "summarize what this project does"
```

To use a different model without writing a config file:

```bash
./harper run "list the files in this directory" --model gpt-oss:20b --effort high
```

## How it runs commands

By default, Harper executes shell commands directly — no sandbox, no container-startup delay. This assumes you're running Harper in a project you trust, the same way you'd trust any other tool you run locally.

For untrusted projects, opt into an isolated Docker sandbox:

```bash
./harper --sandbox docker
```

In Docker mode, network access is denied by default, and the container has memory/CPU limits — configurable via `harper.yaml` (see below).

## Configuration

All settings have built-in defaults; a config file is optional. Point either command at one with `--config`:

```bash
./harper --config harper.yaml
./harper run "<instruction>" --config harper.yaml
```

Example `harper.yaml`:

```yaml
brain:
  provider: ollama
  model: qwen3-coder:30b
  effort: high          # optional: low, medium, or high

subtask:
  provider: ollama
  model: qwen3-coder:30b

ollama:
  base_url: http://localhost:11434
  num_ctx: 16384

sandbox_mode: local      # local (default) or docker

docker:
  image: harper/sandbox:latest
  network: none           # none (default) or bridge
  memory: 2g
  cpus: "2"

mcp_servers:
  - name: fs
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
```

## Command reference

**Interactive REPL:**

```bash
./harper [--config PATH] [--model NAME] [--effort low|medium|high]
```

**Non-interactive run:**

```bash
./harper run "<instruction>" [flags]
```

| Flag | Description | Default |
|---|---|---|
| `--workdir` | Working directory | `.` |
| `--sandbox` | `local` or `docker` | config's `sandbox_mode`, or `local` |
| `--max-turns` | Maximum agent turns before aborting | `40` |
| `--log` | Path to write the JSONL session log | stderr |
| `--config` | Path to a config file | built-in defaults |
| `--model` | Model name for both brain and subtask roles | config's models |
| `--effort` | Reasoning effort for both roles (`low`/`medium`/`high`) | config's effort, or provider default |

## Development

```bash
go build ./...
go test ./...
go vet ./...
```

Each package's tests are self-contained and don't require external services — except the Docker sandbox tests, which skip cleanly if Docker isn't installed or the daemon isn't running.

## Status

Not yet implemented: a second model provider beyond Ollama (config accepts a `provider` field, but only `"ollama"` is wired up), and a domain-restricted network allowlist for the Docker sandbox (currently a binary network on/off toggle).
