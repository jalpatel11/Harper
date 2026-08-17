package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ParsesFullConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harper.yaml")
	yaml := `
brain:
  provider: ollama
  model: qwen3-coder:30b
  effort: high
subtask:
  provider: ollama
  model: qwen3-coder:30b
ollama:
  base_url: http://localhost:11434
  num_ctx: 32768
sandbox_mode: docker
# sandbox_mode is deliberately still "docker" here — this test is checking
# that an explicit, non-default value round-trips correctly through Load,
# not asserting what the default is (see TestDefault_UsesLocalSandboxByDefault).
docker:
  image: harper/sandbox:latest
  network: none
  memory: 2g
  cpus: "2"
mcp_servers:
  - name: fs
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Brain.Model != "qwen3-coder:30b" {
		t.Fatalf("unexpected brain model: %q", cfg.Brain.Model)
	}
	if cfg.Brain.Effort != "high" {
		t.Fatalf("unexpected brain effort: %q", cfg.Brain.Effort)
	}
	if cfg.Subtask.Effort != "" {
		t.Fatalf("expected subtask effort to default empty, got %q", cfg.Subtask.Effort)
	}
	if cfg.Ollama.NumCtx != 32768 {
		t.Fatalf("unexpected num_ctx: %d", cfg.Ollama.NumCtx)
	}
	if cfg.SandboxMode != "docker" {
		t.Fatalf("expected the explicit sandbox_mode: docker to round-trip, got %q", cfg.SandboxMode)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Name != "fs" {
		t.Fatalf("unexpected mcp servers: %v", cfg.MCPServers)
	}
}

func TestDefault_UsesLocalSandboxByDefault(t *testing.T) {
	// No sandbox in the default path, no startup cost from a container.
	// Docker is opt-in (--sandbox=docker) for untrusted projects.
	cfg := Default()
	if cfg.SandboxMode != "local" {
		t.Fatalf("expected default sandbox mode 'local', got %q", cfg.SandboxMode)
	}
}

func TestDefault_DockerConfigHasSafeNetworkDefault(t *testing.T) {
	// Even though docker mode is opt-in now, its own defaults must still be
	// the safe ones for when it is chosen.
	cfg := Default()
	if cfg.Docker.Network != "none" {
		t.Fatalf("expected default network mode 'none', got %q", cfg.Docker.Network)
	}
}

func TestDefault_HasResourceLimits(t *testing.T) {
	// spec.md's safety model explicitly relies on resource limits ("nothing
	// else stops a runaway command" given there are no confirmation
	// prompts) — an empty default here would silently mean no limit at all.
	cfg := Default()
	if cfg.Docker.Memory == "" {
		t.Fatalf("expected a non-empty default memory limit")
	}
	if cfg.Docker.CPUs == "" {
		t.Fatalf("expected a non-empty default CPU limit")
	}
}
