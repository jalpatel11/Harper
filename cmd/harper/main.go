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
	"harper/internal/version"
)

type RunFlags struct {
	Instruction string
	WorkDir     string
	Sandbox     string
	LogPath     string
	ConfigPath  string
	Model       string
	Effort      string
	MaxTurns    int
}

func applyModelOverrides(cfg config.Config, model, effort string) config.Config {
	if model != "" {
		cfg.Brain.Model = model
		cfg.Subtask.Model = model
	}
	if effort != "" {
		cfg.Brain.Effort = effort
		cfg.Subtask.Effort = effort
	}
	return cfg
}

// resolveSandboxMode applies --sandbox > config's sandbox_mode > "local".
func resolveSandboxMode(flagValue, configValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if configValue != "" {
		return configValue
	}
	return "local"
}

func parseRunFlags(args []string) (RunFlags, error) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		return RunFlags{}, fmt.Errorf("harper run: missing required <instruction> argument")
	}

	instruction := args[0]
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	workDir := fs.String("workdir", ".", "working directory")
	sandbox := fs.String("sandbox", "", "sandbox mode: local or docker (default: the config file's sandbox_mode, or local)")
	maxTurns := fs.Int("max-turns", 40, "maximum agent turns before aborting")
	logPath := fs.String("log", "", "path to write the JSONL session log (default: stderr)")
	configPath := fs.String("config", "", "path to the harper config file")
	model := fs.String("model", "", "model name for both brain and subtask roles (overrides config)")
	effort := fs.String("effort", "", "reasoning effort for both roles: low, medium, or high (overrides config; meaning is provider-specific)")

	if err := fs.Parse(args[1:]); err != nil {
		return RunFlags{}, err
	}

	return RunFlags{
		Instruction: instruction,
		WorkDir:     *workDir,
		Sandbox:     *sandbox,
		LogPath:     *logPath,
		ConfigPath:  *configPath,
		Model:       *model,
		Effort:      *effort,
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
		return llm.NewOllamaProvider(ollamaCfg.BaseURL, mc.Model, ollamaCfg.NumCtx, mc.Effort), nil
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("provider \"anthropic\": ANTHROPIC_API_KEY is not set")
		}
		return llm.NewAnthropicProvider(apiKey, mc.Model, mc.Effort), nil
	default:
		return nil, fmt.Errorf("provider %q is not implemented", mc.Provider)
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

	brainTools := buildBrainTools(delegate)
	brainSystemPrompt := "you are harper, a terminal coding agent acting as a lightweight " +
		"orchestrator. Delegate is your only tool — all task work (investigation, tracing, " +
		"coding, debugging, fixes) happens through it, handled by the subtask model. Break " +
		"the user's request into one or more Delegate calls, then use what they return to " +
		"produce your final answer. If the request naturally splits into independent pieces, " +
		"call Delegate multiple times in the same turn — those subtasks run concurrently."
	return agent.NewLoop(brainProvider, brainTools, brainSystemPrompt), nil
}

func main() {
	ctx := context.Background()

	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("harper " + version.Version)
		os.Exit(0)
	}

	if len(os.Args) > 1 && os.Args[1] == "run" {
		flags, err := parseRunFlags(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}

		cfg := applyModelOverrides(loadConfig(flags.ConfigPath), flags.Model, flags.Effort)
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

	// Default path: interactive REPL.
	replFlags := flag.NewFlagSet("harper", flag.ContinueOnError)
	configPath := replFlags.String("config", "", "path to the harper config file")
	model := replFlags.String("model", "", "model name for both brain and subtask roles (overrides config)")
	effort := replFlags.String("effort", "", "reasoning effort for both roles: low, medium, or high (overrides config; meaning is provider-specific)")
	if err := replFlags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	cfg := applyModelOverrides(loadConfig(*configPath), *model, *effort)
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

	if err := RunREPL(ctx, loop, os.Stdin, os.Stdout, cfg.Permissions); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
