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

type Config struct {
	Brain       ModelConfig       `yaml:"brain"`
	Subtask     ModelConfig       `yaml:"subtask"`
	Ollama      OllamaConfig      `yaml:"ollama"`
	SandboxMode string            `yaml:"sandbox_mode"`
	Docker      DockerConfig      `yaml:"docker"`
	MCPServers  []MCPServerConfig `yaml:"mcp_servers"`
}

func Default() Config {
	return Config{
		Brain:       ModelConfig{Provider: "ollama", Model: "qwen3-coder:30b"},
		Subtask:     ModelConfig{Provider: "ollama", Model: "qwen3-coder:30b"},
		Ollama:      OllamaConfig{BaseURL: "http://localhost:11434", NumCtx: 16384},
		SandboxMode: "docker",
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
