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
	// An empty default here would silently mean no limit at all, and
	// nothing else stops a runaway command since there's no confirmation
	// prompt before a tool call.
	cfg := Default()
	if cfg.Docker.Memory == "" {
		t.Fatalf("expected a non-empty default memory limit")
	}
	if cfg.Docker.CPUs == "" {
		t.Fatalf("expected a non-empty default CPU limit")
	}
}

func TestResolvePermissionMode_UsesOverrideWhenPresent(t *testing.T) {
	cfg := PermissionsConfig{Default: "allow", Overrides: map[string]string{"Bash": "ask"}}
	if got := ResolvePermissionMode("Bash", cfg); got != "ask" {
		t.Fatalf("expected override 'ask', got %q", got)
	}
}

func TestResolvePermissionMode_FallsBackToDefault(t *testing.T) {
	cfg := PermissionsConfig{Default: "deny", Overrides: map[string]string{"Bash": "ask"}}
	if got := ResolvePermissionMode("Read", cfg); got != "deny" {
		t.Fatalf("expected default 'deny' for a tool with no override, got %q", got)
	}
}

func TestResolvePermissionMode_FallsBackToAllowWhenNothingConfigured(t *testing.T) {
	if got := ResolvePermissionMode("Bash", PermissionsConfig{}); got != "allow" {
		t.Fatalf("expected 'allow' when nothing is configured at all, got %q", got)
	}
}

func TestLoad_ParsesPermissionsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harper.yaml")
	yamlContent := `
permissions:
  default: allow
  overrides:
    Bash: ask
    Write: deny
`
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Permissions.Default != "allow" {
		t.Fatalf("unexpected permissions.default: %q", cfg.Permissions.Default)
	}
	if cfg.Permissions.Overrides["Bash"] != "ask" || cfg.Permissions.Overrides["Write"] != "deny" {
		t.Fatalf("unexpected permissions.overrides: %v", cfg.Permissions.Overrides)
	}
}

func TestDefault_HasNoPermissionOverrides(t *testing.T) {
	// An unconfigured Harper must behave exactly as it did before this
	// feature existed — every tool resolves to "allow".
	cfg := Default()
	if cfg.Permissions.Default != "" || len(cfg.Permissions.Overrides) != 0 {
		t.Fatalf("expected no permission configuration by default, got %+v", cfg.Permissions)
	}
	if ResolvePermissionMode("Bash", cfg.Permissions) != "allow" {
		t.Fatalf("expected Default() to resolve every tool to allow")
	}
}

func TestDefault_HasSensibleOpenAICompatBaseURLs(t *testing.T) {
	cfg := Default()
	if cfg.LMStudio.BaseURL != "http://localhost:1234/v1" {
		t.Fatalf("unexpected default LM Studio base_url: %q", cfg.LMStudio.BaseURL)
	}
	if cfg.LlamaCpp.BaseURL != "http://localhost:8080/v1" {
		t.Fatalf("unexpected default llama.cpp base_url: %q", cfg.LlamaCpp.BaseURL)
	}
	if cfg.VLLM.BaseURL != "http://localhost:8000/v1" {
		t.Fatalf("unexpected default vLLM base_url: %q", cfg.VLLM.BaseURL)
	}
}

func TestLoad_ParsesOpenAICompatBaseURLOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harper.yaml")
	yamlContent := `
lmstudio:
  base_url: http://192.168.1.50:1234/v1
llamacpp:
  base_url: http://192.168.1.51:8080/v1
vllm:
  base_url: http://192.168.1.52:8000/v1
`
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LMStudio.BaseURL != "http://192.168.1.50:1234/v1" {
		t.Fatalf("unexpected lmstudio.base_url: %q", cfg.LMStudio.BaseURL)
	}
	if cfg.LlamaCpp.BaseURL != "http://192.168.1.51:8080/v1" {
		t.Fatalf("unexpected llamacpp.base_url: %q", cfg.LlamaCpp.BaseURL)
	}
	if cfg.VLLM.BaseURL != "http://192.168.1.52:8000/v1" {
		t.Fatalf("unexpected vllm.base_url: %q", cfg.VLLM.BaseURL)
	}
}
