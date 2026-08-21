# Harper

Harper is a terminal-based coding agent. It runs an agent loop where a configurable **brain** model orchestrates the conversation, and a configurable **subtask** model does the actual task work — reading files, running commands, writing code — handed off through a `Delegate` tool. Both roles are provider-agnostic: the same binary runs against local models (Ollama, LM Studio, llama.cpp, vLLM) or hosted ones (Anthropic) without a rewrite.

## Features

- **Orchestrator/worker agent loop** — the brain's only tool is `Delegate`; it breaks a request into one or more delegated subtasks, and the subtask model does the real work (reading files, running commands, editing code) with its own bounded tool loop.
- **Parallel delegation** — when a request naturally splits into independent pieces, the brain can call `Delegate` multiple times in one turn; those subtasks run concurrently instead of one at a time.
- **Independent brain/subtask models** — configure each role's provider, model, and reasoning effort separately, or override both at once from the command line. Mixing providers across roles is supported.
- **Five providers** — Ollama, LM Studio, llama.cpp, and vLLM (all local), plus Anthropic (hosted), selected per-role in config. LM Studio, llama.cpp, and vLLM all speak the same OpenAI-compatible chat-completions API, so they share one provider implementation, differing only in default port. Set `ANTHROPIC_API_KEY` in the environment to use Anthropic.
- **Per-tool permission modes** — `allow` (default), `ask`, or `deny`, configurable globally or per tool. Fully optional: an unconfigured Harper resolves every tool to `allow`, identical to not having the feature at all — nothing to opt out of, just an on-ramp when you want it. `ask` prompts interactively in the REPL (allow once / allow for session / deny); `run` mode validates at startup and refuses to start if a tool can't be resolved without a human to ask.
- **Six built-in tools** — `Read`, `Write`, `Edit`, `Grep`, `Glob`, and `Bash` — available to the subtask model.
- **Two execution modes** — direct execution by default (no sandbox, no startup cost), or an opt-in Docker sandbox for untrusted projects, with network access denied by default and container resource limits.
- **MCP client support** — connect to external MCP servers and their tools are merged into the subtask model's tool set automatically.
- **Two interfaces** — an interactive REPL, and a non-interactive `run` mode with structured JSONL session logging, so Harper can be driven by scripts or other tooling.

## Installation

**Install script** (macOS/Linux):

```bash
curl -sSL https://raw.githubusercontent.com/jalpatel11/Harper/main/install.sh | sh
```

Detects your OS/architecture, downloads the matching release binary, verifies its checksum, and installs it to `/usr/local/bin` (override with `INSTALL_DIR=...`). Pin a specific version with `HARPER_VERSION=v1.2.0`.

**Or download a pre-built binary manually** from the [latest release](https://github.com/jalpatel11/Harper/releases/latest):

| Platform | Download |
|---|---|
| macOS (Apple Silicon) | `harper_v1.2.0_darwin_arm64.tar.gz` |
| macOS (Intel) | `harper_v1.2.0_darwin_amd64.tar.gz` |
| Linux (arm64) | `harper_v1.2.0_linux_arm64.tar.gz` |
| Linux (amd64) | `harper_v1.2.0_linux_amd64.tar.gz` |

```bash
tar -xzf harper_v1.2.0_<platform>.tar.gz
cd harper_v1.2.0_<platform>
./harper version
```

Checksums are in `SHA256SUMS.txt` on the same release page.

**Or build from source** (requires Go 1.23+):

```bash
git clone git@github.com:jalpatel11/Harper.git
cd Harper
go build -o harper ./cmd/harper
```

## Quick start

Harper needs a model provider for both the brain and subtask roles. Two options:

**Ollama (local, default):**

```bash
ollama pull qwen3-coder:30b
```

**Anthropic (hosted):**

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

and set `provider: anthropic` for a role in `harper.yaml` (see Configuration below) — there's no built-in default model for Anthropic, so a config file or `--model` is required for that role.

**LM Studio / llama.cpp / vLLM (local, OpenAI-compatible):** start the server (LM Studio's local server, `llama-server`, or vLLM's `--api-server`), then set `provider: lmstudio` / `llamacpp` / `vllm` for a role. Each has a sensible default base URL (`:1234`, `:8080`, `:8000` respectively); override with `lmstudio.base_url` / `llamacpp.base_url` / `vllm.base_url` in `harper.yaml` if the server runs elsewhere.

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
  provider: anthropic
  model: claude-haiku-4-5-20251001

subtask:
  provider: anthropic
  model: claude-sonnet-5

ollama:
  base_url: http://localhost:11434
  num_ctx: 16384

lmstudio:
  base_url: http://localhost:1234/v1   # default shown; override if the server runs elsewhere

llamacpp:
  base_url: http://localhost:8080/v1   # default shown

vllm:
  base_url: http://localhost:8000/v1   # default shown

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

permissions:
  default: allow          # allow (default), ask, or deny
  overrides:
    Bash: ask
    Write: ask
```

`ANTHROPIC_API_KEY` is read from the environment, never from the config file. Mixing providers across roles (e.g. Anthropic brain + Ollama subtask) is supported — each role's provider is resolved independently.

### Permission modes

Each tool call resolves to `allow`, `ask`, or `deny`: an exact match in `permissions.overrides` wins, then `permissions.default`, then `allow` if nothing is configured — an unconfigured Harper behaves exactly as before this feature existed.

- **`allow`** (default) — runs without confirmation.
- **`deny`** — the tool call is refused; the model sees a `permission denied` result and can adapt.
- **`ask`** — only honored interactively, in the REPL. On first use of a tool set to `ask`, Harper prompts `allow once / allow for session / deny? [o/s/d]`; `s` is remembered for the rest of that session, so the same tool isn't re-prompted.

`ask` has no one to prompt outside the REPL: the subtask model's own tool calls are always resolved statically (`ask` silently denies, no blocking), and `harper run` validates at startup that its own tool (`Delegate`) doesn't resolve to `ask` — it refuses to start with a clear error rather than hang or silently deny mid-task.

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

**Version:**

```bash
./harper version
```

## Development

```bash
go build ./...
go test ./...
go vet ./...
```

Each package's tests are self-contained and don't require external services — except the Docker sandbox tests, which skip cleanly if Docker isn't installed or the daemon isn't running.

## Status

Not yet implemented: a domain-restricted network allowlist for the Docker sandbox (currently a binary network on/off toggle), session persistence/resume, a rich TUI, and Harper-as-MCP-server.
