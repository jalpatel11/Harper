package main

import (
	"time"

	"harper/internal/executor"
	"harper/internal/tools"
)

func buildCoreTools(exec executor.Executor) []tools.Tool {
	return []tools.Tool{
		tools.NewReadTool(),
		tools.NewWriteTool(),
		tools.NewEditTool(),
		tools.NewGrepTool(),
		tools.NewGlobTool(),
		tools.NewBashTool(exec, 2*time.Minute),
	}
}

// buildBrainTools gives the brain only Delegate — it orchestrates by
// breaking a request into one or more Delegate calls rather than doing
// task work itself. The subtask loop behind Delegate already has full
// access to core and MCP tools, so nothing is lost by not also handing
// them to the brain.
func buildBrainTools(delegate tools.Tool) []tools.Tool {
	return []tools.Tool{delegate}
}

// buildSimpleModeBrainTools gives the brain direct access to the core and
// MCP tools — a flat, single-loop agent with no Delegate/subtask split and
// no second model in the loop, for `mode: simple`.
func buildSimpleModeBrainTools(coreTools, mcpTools []tools.Tool) []tools.Tool {
	all := make([]tools.Tool, 0, len(coreTools)+len(mcpTools))
	all = append(all, coreTools...)
	all = append(all, mcpTools...)
	return all
}
