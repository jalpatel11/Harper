package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"harper/internal/agent"
	"harper/internal/config"
	"harper/internal/executor"
	"harper/internal/llm"
	"harper/internal/logging"
	"harper/internal/mcp"
	"harper/internal/tools"
)

type RunFlags struct {
	Instruction string
	WorkDir     string
	Sandbox     string
	LogPath     string
	ConfigPath  string
	MaxTurns    int
}

// resolveSandboxMode applies --sandbox > config's sandbox_mode > "docker",
// so a config-file-only sandbox choice isn't silently overridden by a flag
// default the user never actually passed.
func resolveSandboxMode(flagValue, configValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if configValue != "" {
		return configValue
	}
	return "docker"
}

func parseRunFlags(args []string) (RunFlags, error) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		return RunFlags{}, fmt.Errorf("harper run: missing required <instruction> argument")
	}

	instruction := args[0]
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	workDir := fs.String("workdir", ".", "working directory")
	// Sandbox defaults to "" (not "docker") so main() can tell "flag not
	// passed" apart from "explicitly passed docker" and fall back to the
	// loaded config's sandbox_mode instead of always overriding it.
	sandbox := fs.String("sandbox", "", "sandbox mode: docker or local (default: the config file's sandbox_mode, or docker)")
	maxTurns := fs.Int("max-turns", 40, "maximum agent turns before aborting")
	logPath := fs.String("log", "", "path to write the JSONL session log (default: stderr)")
	configPath := fs.String("config", "", "path to the harper config file")

	if err := fs.Parse(args[1:]); err != nil {
		return RunFlags{}, err
	}

	return RunFlags{
		Instruction: instruction,
		WorkDir:     *workDir,
		Sandbox:     *sandbox,
		LogPath:     *logPath,
		ConfigPath:  *configPath,
		MaxTurns:    *maxTurns,
	}, nil
}

func loadConfig(path string) config.Config {
	if path == "" {
		return config.Default()
	}
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load config %s, using defaults: %v\n", path, err)
		return config.Default()
	}
	return cfg
}

func buildExecutor(ctx context.Context, sandboxMode string, cfg config.Config, workDir string) (executor.Executor, func(), error) {
	if sandboxMode == "local" {
		return executor.NewLocalExecutor(workDir), func() {}, nil
	}

	d, err := executor.NewDockerExecutor(ctx, executor.DockerOptions{
		Image:   cfg.Docker.Image,
		WorkDir: workDir,
		Network: cfg.Docker.Network,
		Memory:  cfg.Docker.Memory,
		CPUs:    cfg.Docker.CPUs,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("start docker sandbox: %w", err)
	}
	return d, func() { d.Close(ctx) }, nil
}

func buildProvider(mc config.ModelConfig, ollamaCfg config.OllamaConfig) (llm.Provider, error) {
	switch mc.Provider {
	case "ollama", "":
		return llm.NewOllamaProvider(ollamaCfg.BaseURL, mc.Model, ollamaCfg.NumCtx), nil
	default:
		return nil, fmt.Errorf("provider %q is not implemented in v1 (only \"ollama\" is supported; AnthropicProvider is a stub reserved for a later milestone)", mc.Provider)
	}
}

func buildBrainLoop(ctx context.Context, cfg config.Config, exec executor.Executor) (*agent.Loop, error) {
	brainProvider, err := buildProvider(cfg.Brain, cfg.Ollama)
	if err != nil {
		return nil, fmt.Errorf("brain: %w", err)
	}
	subtaskProvider, err := buildProvider(cfg.Subtask, cfg.Ollama)
	if err != nil {
		return nil, fmt.Errorf("subtask: %w", err)
	}

	if err := agent.CheckToolCallCapability(ctx, brainProvider, "brain"); err != nil {
		return nil, err
	}
	if err := agent.CheckToolCallCapability(ctx, subtaskProvider, "subtask"); err != nil {
		return nil, err
	}

	core := buildCoreTools(exec)

	var mcpTools []tools.Tool
	if len(cfg.MCPServers) > 0 {
		merged, err := mcp.MergeTools(ctx, cfg.MCPServers, mcp.Connect)
		if err != nil {
			return nil, fmt.Errorf("connect mcp servers: %w", err)
		}
		mcpTools = merged
	}

	delegate := agent.NewDelegateTool(subtaskProvider, append(append([]tools.Tool{}, core...), mcpTools...), "you are harper's subtask agent", 40)

	brainTools := buildBrainTools(core, mcpTools, delegate)
	return agent.NewLoop(brainProvider, brainTools, "you are harper, a terminal coding agent"), nil
}

func main() {
	ctx := context.Background()

	if len(os.Args) > 1 && os.Args[1] == "run" {
		flags, err := parseRunFlags(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}

		cfg := loadConfig(flags.ConfigPath)
		exec, cleanup, err := buildExecutor(ctx, resolveSandboxMode(flags.Sandbox, cfg.SandboxMode), cfg, flags.WorkDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer cleanup()

		loop, err := buildBrainLoop(ctx, cfg, exec)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		var logWriter = os.Stderr
		if flags.LogPath != "" {
			f, err := os.Create(flags.LogPath)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			defer f.Close()
			logWriter = f
		}
		logger := logging.NewJSONLLogger(logWriter)

		answer, err := RunOnce(ctx, loop, flags.Instruction, flags.MaxTurns, logger)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(answer)
		os.Exit(0)
	}

	// Default path: interactive REPL. Its own small flag set exists so a
	// custom harper.yaml (a different Ollama model, later a real Anthropic
	// config) can be selected here too, not only for `harper run`.
	replFlags := flag.NewFlagSet("harper", flag.ContinueOnError)
	configPath := replFlags.String("config", "", "path to the harper config file")
	if err := replFlags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	cfg := loadConfig(*configPath)
	exec, cleanup, err := buildExecutor(ctx, cfg.SandboxMode, cfg, ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer cleanup()

	loop, err := buildBrainLoop(ctx, cfg, exec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := RunREPL(ctx, loop, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
