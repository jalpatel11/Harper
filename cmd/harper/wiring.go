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

func buildBrainTools(coreTools, mcpTools []tools.Tool, delegate tools.Tool) []tools.Tool {
	all := make([]tools.Tool, 0, len(coreTools)+len(mcpTools)+1)
	all = append(all, coreTools...)
	all = append(all, mcpTools...)
	all = append(all, delegate)
	return all
}
