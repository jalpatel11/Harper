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
