package loop

import (
	"fmt"
	"strings"
	"testing"

	"agro/internal/llm"
)

// A result within budget is returned byte-for-byte unchanged.
func TestTruncateMiddleUnderBudget(t *testing.T) {
	s := "small output"
	if got := truncateMiddle(s, 1024); got != s {
		t.Fatalf("under-budget string must be unchanged, got %q", got)
	}
	// Exactly at the budget is also untouched (boundary).
	exact := strings.Repeat("x", 100)
	if got := truncateMiddle(exact, 100); got != exact {
		t.Fatalf("string exactly at budget must be unchanged, got len %d", len(got))
	}
}

// An over-budget result keeps its head and tail and reports the elided byte
// count in the middle marker.
func TestTruncateMiddleOverBudget(t *testing.T) {
	head := strings.Repeat("A", 5000)
	tail := strings.Repeat("B", 5000)
	s := head + tail // 10000 bytes
	budget := 4000

	got := truncateMiddle(s, budget)

	if len(got) >= len(s) {
		t.Fatalf("expected truncation to shrink the content (%d) below original (%d)", len(got), len(s))
	}
	// Head and tail of the ORIGINAL are preserved.
	if !strings.HasPrefix(got, strings.Repeat("A", budget/2)) {
		t.Fatalf("expected the head of the original preserved")
	}
	if !strings.HasSuffix(got, strings.Repeat("B", budget-budget/2)) {
		t.Fatalf("expected the tail of the original preserved")
	}
	// The marker reports exactly the number of bytes removed.
	removed := len(s) - budget
	wantMarker := fmt.Sprintf("[... truncated %d bytes ...]", removed)
	if !strings.Contains(got, wantMarker) {
		t.Fatalf("expected marker %q in output, got: %q", wantMarker, got)
	}
}

// A non-positive budget disables truncation (defensive: never panic / over-slice).
func TestTruncateMiddleZeroBudget(t *testing.T) {
	s := "anything"
	if got := truncateMiddle(s, 0); got != s {
		t.Fatalf("zero budget should be a no-op, got %q", got)
	}
}

// Boundary: when the whole slice is already within budget, trimHistory returns
// it untouched — no tool result is compacted.
func TestTrimHistoryNoOpUnderBudget(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "do the task"},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "some file contents"},
		{Role: "assistant", Content: "DONE: ok"},
	}
	out := trimHistory(msgs, 1<<20, 2) // huge budget

	for i, m := range out {
		if m.Content != msgs[i].Content {
			t.Fatalf("message %d changed under a generous budget: %q -> %q", i, msgs[i].Content, m.Content)
		}
	}
}

// Over budget: the OLDEST tool result is compacted first, while the system
// prompt and the most recent keepRecent messages are left intact.
func TestTrimHistoryCompactsOldestToolFirst(t *testing.T) {
	big := strings.Repeat("x", 1000)
	msgs := []llm.Message{
		{Role: "system", Content: "system prompt"},                        // 0: never compacted
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1"}}},        // 1
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: big}, // 2: OLDEST tool result
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c2"}}},        // 3
		{Role: "tool", ToolCallID: "c2", Name: "run_bash", Content: big},  // 4: protected (recent)
		{Role: "assistant", Content: "DONE: done"},                        // 5: protected (recent)
	}
	keepRecent := 2 // protect indices 4 and 5

	// Budget that forces compacting exactly one of the two ~1000B tool results.
	budget := totalBytes(msgs) - 500
	out := trimHistory(msgs, budget, keepRecent)

	if out[2].Content != elidedToolResult {
		t.Fatalf("oldest tool result should be compacted, got %q", out[2].Content)
	}
	if out[4].Content != big {
		t.Fatal("recent (protected) tool result must NOT be compacted")
	}
	if out[0].Content != "system prompt" {
		t.Fatal("system prompt must never be compacted")
	}
	if totalBytes(out) > budget {
		t.Fatalf("history still over budget after trimming: %d > %d", totalBytes(out), budget)
	}
}

// When even compacting every eligible (older) tool result can't reach the
// budget, trimHistory compacts what it may and stops — it never touches the
// protected recent window or non-tool messages, even at the cost of staying
// over budget.
func TestTrimHistoryNeverCompactsProtectedRecent(t *testing.T) {
	big := strings.Repeat("y", 5000)
	msgs := []llm.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "tool", ToolCallID: "c1", Name: "run_bash", Content: big}, // recent + protected
		{Role: "assistant", Content: "DONE"},                             // recent + protected
	}
	keepRecent := 2 // protects the only tool message

	out := trimHistory(msgs, 10, keepRecent) // impossibly small budget

	if out[1].Content != big {
		t.Fatal("a tool result inside the protected recent window must never be compacted")
	}
}
