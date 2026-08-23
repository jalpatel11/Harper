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

// subtaskStepMsg is one step of a specific Delegate call's subtask loop,
// tagged with that call's tool call ID so concurrent Delegate calls render
// as separate cards.
type subtaskStepMsg struct {
	toolCallID string
	message    llm.Message
}

// subtaskDoneMsg removes a subtask's card once its Delegate call returns.
type subtaskDoneMsg struct {
	toolCallID string
}
