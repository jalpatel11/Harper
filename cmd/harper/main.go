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
	Mode        string
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

// applyModeOverride applies --mode over the config file's mode, same
// override precedence as applyModelOverrides.
func applyModeOverride(cfg config.Config, mode string) config.Config {
	if mode != "" {
		cfg.Mode = mode
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
	mode := fs.String("mode", "", "agent mode: \"\" (default, orchestrator: brain delegates all task work to the subtask model) or \"simple\" (flat single-loop agent, brain uses tools directly, no subtask model)")

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
		Mode:        *mode,
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

func buildProvider(mc config.ModelConfig, cfg config.Config) (llm.Provider, error) {
	switch mc.Provider {
	case "ollama", "":
		return llm.NewOllamaProvider(cfg.Ollama.BaseURL, mc.Model, cfg.Ollama.NumCtx, mc.Effort), nil
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("provider \"anthropic\": ANTHROPIC_API_KEY is not set")
		}
		return llm.NewAnthropicProvider(apiKey, mc.Model, mc.Effort), nil
	case "lmstudio":
		return llm.NewOpenAICompatProvider(cfg.LMStudio.BaseURL, mc.Model, mc.Effort), nil
	case "llamacpp":
		return llm.NewOpenAICompatProvider(cfg.LlamaCpp.BaseURL, mc.Model, mc.Effort), nil
	case "vllm":
		return llm.NewOpenAICompatProvider(cfg.VLLM.BaseURL, mc.Model, mc.Effort), nil
	default:
		return nil, fmt.Errorf("provider %q is not implemented", mc.Provider)
	}
}

func buildBrainLoop(ctx context.Context, cfg config.Config, exec executor.Executor) (*agent.Loop, error) {
	brainProvider, err := buildProvider(cfg.Brain, cfg)
	if err != nil {
		return nil, fmt.Errorf("brain: %w", err)
	}
	if err := agent.CheckToolCallCapability(ctx, brainProvider, "brain"); err != nil {
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

	// Simple mode: a flat, single-loop agent (no Delegate, no subtask
	// model) — the brain calls core/MCP tools directly, same shape as a
	// minimal single-loop coding agent. Skips the subtask provider and its
	// capability check entirely, which also halves the fixed startup cost
	// versus orchestrator mode.
	if cfg.Mode == "simple" {
		brainTools := buildSimpleModeBrainTools(core, mcpTools)
		return agent.NewLoop(brainProvider, brainTools, "you are harper, a terminal coding agent"), nil
	}

	subtaskProvider, err := buildProvider(cfg.Subtask, cfg)
	if err != nil {
		return nil, fmt.Errorf("subtask: %w", err)
	}
	if err := agent.CheckToolCallCapability(ctx, subtaskProvider, "subtask"); err != nil {
		return nil, err
	}

	delegate := agent.NewDelegateTool(subtaskProvider, append(append([]tools.Tool{}, core...), mcpTools...), "you are harper's subtask agent", 40)
	// The subtask loop has no terminal of its own, REPL or not — its
	// checker must always be static, never interactive (see Global
	// Constraints in the permission-modes plan).
	delegate.SetPermissionChecker(newStaticPermissionChecker(cfg.Permissions))

	brainTools := buildBrainTools(delegate)
	brainSystemPrompt := "you are harper, a terminal coding agent acting as a lightweight " +
		"orchestrator. Delegate is your only tool — all task work (investigation, tracing, " +
		"coding, debugging, fixes) happens through it, handled by the subtask model. Break " +
		"the user's request into one or more Delegate calls, then use what they return to " +
		"produce your final answer. If the request naturally splits into independent pieces, " +
		"call Delegate multiple times in the same turn — those subtasks run concurrently."
	return agent.NewLoop(brainProvider, brainTools, brainSystemPrompt), nil
}

// brainToolNamesForValidation returns the brain's own tool names for
// run-mode's startup ask-validation. In simple mode the brain calls core
// tools directly; MCP tool names aren't included here (that would mean
// reconnecting to MCP servers a second time just for their names) — an
// "ask"-configured MCP tool in simple mode still fails safe at call time
// (the static checker denies it), it just isn't caught at startup.
func brainToolNamesForValidation(cfg config.Config, exec executor.Executor) []string {
	if cfg.Mode == "simple" {
		names := make([]string, 0, 6)
		for _, t := range buildCoreTools(exec) {
			names = append(names, t.Name())
		}
		return names
	}
	return []string{"Delegate"}
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

		cfg := applyModeOverride(applyModelOverrides(loadConfig(flags.ConfigPath), flags.Model, flags.Effort), flags.Mode)
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

		// In orchestrator mode the brain's only tool is Delegate; in simple
		// mode it's the core tools directly (see buildBrainLoop). There's
		// no REPL in run mode to answer an "ask", so this must fail fast
		// at startup rather than silently deny or hang mid-task.
		if err := validateNoAskForRunMode(cfg.Permissions, brainToolNamesForValidation(cfg, exec)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		loop.SetPermissionChecker(newStaticPermissionChecker(cfg.Permissions))

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
	mode := replFlags.String("mode", "", "agent mode: \"\" (default, orchestrator) or \"simple\" (flat single-loop agent, no subtask model)")
	if err := replFlags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	cfg := applyModeOverride(applyModelOverrides(loadConfig(*configPath), *model, *effort), *mode)
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
