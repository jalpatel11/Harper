package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ModelConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	// Effort is a provider-neutral reasoning-effort hint ("low", "medium",
	// "high", or "" for the provider's default). Each Provider
	// implementation decides what it means: OllamaProvider maps it to
	// Ollama's native "think" field; a future AnthropicProvider would map
	// it to an extended-thinking budget. The CLI/config surface doesn't
	// change when a new provider is added.
	Effort string `yaml:"effort"`
}

type OllamaConfig struct {
	BaseURL string `yaml:"base_url"`
	NumCtx  int    `yaml:"num_ctx"`
}

// OpenAICompatConfig configures any server implementing the OpenAI
// chat-completions API shape — LM Studio, llama.cpp's server, and vLLM all
// share this same shape, differing only in base URL.
type OpenAICompatConfig struct {
	BaseURL string `yaml:"base_url"`
}

type DockerConfig struct {
	Image   string `yaml:"image"`
	Network string `yaml:"network"`
	Memory  string `yaml:"memory"`
	CPUs    string `yaml:"cpus"`
}

type MCPServerConfig struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

type PermissionsConfig struct {
	Default   string            `yaml:"default"`
	Overrides map[string]string `yaml:"overrides"`
}

// ResolvePermissionMode returns "allow", "ask", or "deny" for a tool,
// falling back to cfg.Default, falling back to "allow" if nothing is
// configured at all — an unconfigured Harper behaves exactly as it did
// before this feature existed.
func ResolvePermissionMode(toolName string, cfg PermissionsConfig) string {
	if mode, ok := cfg.Overrides[toolName]; ok && mode != "" {
		return mode
	}
	if cfg.Default != "" {
		return cfg.Default
	}
	return "allow"
}

type Config struct {
	Brain   ModelConfig  `yaml:"brain"`
	Subtask ModelConfig  `yaml:"subtask"`
	Ollama  OllamaConfig `yaml:"ollama"`
	// Mode is "" (default) for the orchestrator architecture — the brain's
	// only tool is Delegate, and the subtask model does all task work — or
	// "simple" for a flat, single-loop agent: the brain gets the core/MCP
	// tools directly, Subtask is unused, and there's only one model in the
	// loop. Optional, off by default, same as Permissions — nothing changes
	// for a config that doesn't set it.
	Mode        string             `yaml:"mode"`
	LMStudio    OpenAICompatConfig `yaml:"lmstudio"`
	LlamaCpp    OpenAICompatConfig `yaml:"llamacpp"`
	VLLM        OpenAICompatConfig `yaml:"vllm"`
	SandboxMode string             `yaml:"sandbox_mode"`
	Docker      DockerConfig       `yaml:"docker"`
	MCPServers  []MCPServerConfig  `yaml:"mcp_servers"`
	Permissions PermissionsConfig  `yaml:"permissions"`
}

func Default() Config {
	return Config{
		Brain:    ModelConfig{Provider: "ollama", Model: "qwen3-coder:30b"},
		Subtask:  ModelConfig{Provider: "ollama", Model: "qwen3-coder:30b"},
		Ollama:   OllamaConfig{BaseURL: "http://localhost:11434", NumCtx: 16384},
		LMStudio: OpenAICompatConfig{BaseURL: "http://localhost:1234/v1"},
		LlamaCpp: OpenAICompatConfig{BaseURL: "http://localhost:8080/v1"},
		VLLM:     OpenAICompatConfig{BaseURL: "http://localhost:8000/v1"},
		// No sandbox in the default path, so there's no container-startup
		// cost — Docker is opt-in (--sandbox=docker) for untrusted projects.
		SandboxMode: "local",
		Docker: DockerConfig{
			Image:   "harper/sandbox:latest",
			Network: "none",
			Memory:  "2g",
			CPUs:    "2",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}
