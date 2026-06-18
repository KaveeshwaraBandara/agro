package loop

import (
	"fmt"

	"agro/internal/llm"
)

// Context-budget knobs. These bound how much of the conversation stays resident
// in the message slice sent to the model each turn, so a few huge tool outputs
// (a big file, a noisy command) can't blow past the provider's context window.
const (
	// maxToolResultBytes caps a single tool result kept in the history. Larger
	// results are middle-truncated (head + tail preserved).
	maxToolResultBytes = 8 * 1024
	// maxHistoryBytes is the total budget for the whole message slice.
	maxHistoryBytes = 96 * 1024
	// keepRecentMessages is how many trailing messages are always left intact
	// (never compacted), so the model keeps its most recent context.
	keepRecentMessages = 6
)

// elidedToolResult replaces a compacted (older) tool result's content. The
// message itself is kept so the tool_call_id pairing the API requires stays
// valid; only the bulky content is dropped.
const elidedToolResult = "[older tool result elided to fit the context budget]"

// truncateMiddle caps s at budget bytes, preserving the head and the tail and
// replacing the elided middle with a "[... truncated N bytes ...]" marker, where
// N is the number of bytes removed. Strings already within budget (or a
// non-positive budget) are returned unchanged. Unlike a plain head cut, keeping
// the tail preserves the often-important end of a file or command output.
func truncateMiddle(s string, budget int) string {
	if budget <= 0 || len(s) <= budget {
		return s
	}
	head := budget / 2
	tail := budget - head
	removed := len(s) - head - tail
	marker := fmt.Sprintf("\n[... truncated %d bytes ...]\n", removed)
	return s[:head] + marker + s[len(s)-tail:]
}

// messageBytes estimates the wire size of one message (content plus any tool
// call arguments). It is an approximation used only for budgeting decisions.
func messageBytes(m llm.Message) int {
	n := len(m.Role) + len(m.Content) + len(m.Name) + len(m.ToolCallID)
	for _, tc := range m.ToolCalls {
		n += len(tc.ID) + len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	return n
}

// totalBytes sums the estimated size of every message in the slice.
func totalBytes(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += messageBytes(m)
	}
	return n
}

// trimHistory keeps the message slice within budget by compacting the OLDEST
// tool results first. The system prompt (and every non-tool message) is never
// compacted, and the most recent keepRecent messages are always left intact, so
// the model retains its current working context. Compaction replaces a tool
// message's content with a short placeholder rather than deleting the message,
// keeping the assistant tool_call / tool result pairing the API requires valid.
//
// It mutates messages in place and returns the same slice. When the slice is
// already within budget it is returned untouched (the common, no-trim path).
func trimHistory(messages []llm.Message, budget, keepRecent int) []llm.Message {
	if totalBytes(messages) <= budget {
		return messages // boundary: nothing to trim
	}
	protectFrom := len(messages) - keepRecent
	for i := range messages {
		if i >= protectFrom {
			break // reached the protected recent window; stop compacting
		}
		if totalBytes(messages) <= budget {
			break
		}
		if m := &messages[i]; m.Role == "tool" && m.Content != elidedToolResult {
			m.Content = elidedToolResult
		}
	}
	return messages
}
