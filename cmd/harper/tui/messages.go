package tui

import "harper/internal/llm"

// turnDoneMsg is sent once loop.Run returns, whether it succeeded or not.
type turnDoneMsg struct {
	history []llm.Message
	err     error
}

// toolStepMsg is a brain-level onStep event (a tool result at the top
// level, not inside a Delegate subtask — see subtaskStepMsg in Task 5 for
// that).
type toolStepMsg struct {
	message llm.Message
}
