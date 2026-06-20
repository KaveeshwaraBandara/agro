package loop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agro/internal/llm"
	"agro/internal/tools"
)

// mockResult is one scripted response from the mock client.
type mockResult struct {
	msg *llm.Message
	err error
}

// mockClient implements Chatter. It returns results in order; once the script
// is exhausted it repeats the last entry (so a single failing result models a
// provider that keeps failing).
type mockClient struct {
	results []mockResult
	calls   int
	lastIn  []llm.Message // messages passed to the most recent Chat call (copied)
}

func (m *mockClient) Chat(messages []llm.Message, _ []llm.Tool) (*llm.Message, error) {
	m.lastIn = append([]llm.Message(nil), messages...)
	i := m.calls
	m.calls++
	if i >= len(m.results) {
		i = len(m.results) - 1
	}
	r := m.results[i]
	return r.msg, r.err
}

// noBackoff disables sleeping so retry tests run instantly. It restores the
// original on cleanup.
func noBackoff(t *testing.T) {
	t.Helper()
	orig := sleep
	sleep = func(time.Duration) {}
	t.Cleanup(func() { sleep = orig })
}

// recordSleeps captures backoff durations instead of sleeping.
func recordSleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	var waits []time.Duration
	orig := sleep
	sleep = func(d time.Duration) { waits = append(waits, d) }
	t.Cleanup(func() { sleep = orig })
	return &waits
}

func toolUseFailed() error {
	return &llm.APIError{StatusCode: 400, Body: `{"error":{"code":"tool_use_failed","message":"<function=read_file>..."}}`}
}

// Issue 1: tool_use_failed is NOT transient. It must be CORRECTED (format
// feedback + a fresh turn), not retried. One failure then success => exactly
// 2 Chat calls, with a correction fed back in between (proving correction, not
// a same-turn retry).
func TestToolUseFailedCorrectedNotRetried(t *testing.T) {
	noBackoff(t)
	done := &llm.Message{Role: "assistant", Content: "DONE: created primes.py"}
	m := &mockClient{results: []mockResult{
		{nil, toolUseFailed()}, // turn 1: malformed tool call
		{done, nil},            // turn 2: succeeds after correction
	}}

	if err := Run(m, "do the task", 5, false); err != nil {
		t.Fatalf("expected loop to recover via correction, got error: %v", err)
	}
	if m.calls != 2 {
		t.Fatalf("expected correction + 1 new turn (2 calls), not retries, got %d", m.calls)
	}
	if !hasCorrection(m.lastIn) {
		t.Fatal("expected a tool-format correction fed back, proving correction (not retry)")
	}
}

// The give-up path: a persistent genuinely-transient 5xx is retried maxRetries5xx
// times (4 calls total: 1 initial + 3 retries) and then the loop fails.
func TestRunGivesUpAfter5xxRetries(t *testing.T) {
	noBackoff(t)
	m := &mockClient{results: []mockResult{
		{nil, &llm.APIError{StatusCode: 500, Body: "boom"}}, // repeats forever
	}}

	err := Run(m, "do the task", 5, false)
	if err == nil {
		t.Fatal("expected error after exhausting 5xx retries, got nil")
	}
	if m.calls != 1+maxRetries5xx {
		t.Fatalf("expected %d Chat calls (1 initial + %d retries), got %d", 1+maxRetries5xx, maxRetries5xx, m.calls)
	}
}

// Non-retryable errors must fail immediately without burning retries.
func TestRunNonRetryableFailsImmediately(t *testing.T) {
	noBackoff(t)
	m := &mockClient{results: []mockResult{
		{nil, &llm.APIError{StatusCode: 401, Body: "unauthorized"}},
	}}

	err := Run(m, "do the task", 5, false)
	if err == nil {
		t.Fatal("expected immediate failure on 401, got nil")
	}
	if m.calls != 1 {
		t.Fatalf("expected exactly 1 Chat call (no retries), got %d", m.calls)
	}
}

// Only 429 and 5xx are retryable. tool_use_failed is NOT (it's corrected).
func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"429", &llm.APIError{StatusCode: 429, Body: "rate limited"}, true},
		{"500", &llm.APIError{StatusCode: 500, Body: "boom"}, true},
		{"503", &llm.APIError{StatusCode: 503, Body: "unavailable"}, true},
		{"400 tool_use_failed", toolUseFailed(), false}, // corrected, not retried
		{"400 other", &llm.APIError{StatusCode: 400, Body: "bad request"}, false},
		{"401", &llm.APIError{StatusCode: 401, Body: "nope"}, false},
		{"non-api error", errPlain("network down"), false},
	}
	for _, tc := range cases {
		if got := isRetryable(tc.err); got != tc.want {
			t.Errorf("%s: isRetryable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsToolUseFailed(t *testing.T) {
	if !isToolUseFailed(toolUseFailed()) {
		t.Error("expected tool_use_failed 400 to be detected")
	}
	if isToolUseFailed(&llm.APIError{StatusCode: 400, Body: "some other 400"}) {
		t.Error("a plain 400 must not be treated as tool_use_failed")
	}
	if isToolUseFailed(&llm.APIError{StatusCode: 429, Body: "tool_use_failed"}) {
		t.Error("429 is not tool_use_failed even if the body mentions it")
	}
}

// Issue 2: a 429 carrying a provider retry hint waits at least that long.
func TestRetry429RespectsHint(t *testing.T) {
	waits := recordSleeps(t)
	done := &llm.Message{Role: "assistant", Content: "DONE: ok"}
	m := &mockClient{results: []mockResult{
		{nil, &llm.APIError{StatusCode: 429, Body: `{"error":{"message":"Rate limit reached. Please try again in 2s."}}`}},
		{done, nil},
	}}

	if err := Run(m, "do the task", 5, false); err != nil {
		t.Fatalf("expected recovery after 429, got: %v", err)
	}
	if len(*waits) != 1 {
		t.Fatalf("expected exactly one backoff sleep, got %d: %v", len(*waits), *waits)
	}
	if (*waits)[0] < 2*time.Second {
		t.Fatalf("expected >=2s wait from the provider hint, got %s", (*waits)[0])
	}
}

// With no hint, 429 backoff is exponential starting at 2s.
func TestRetry429ExponentialDefault(t *testing.T) {
	waits := recordSleeps(t)
	done := &llm.Message{Role: "assistant", Content: "DONE: ok"}
	m := &mockClient{results: []mockResult{
		{nil, &llm.APIError{StatusCode: 429, Body: "rate limited (no hint)"}},
		{done, nil},
	}}

	if err := Run(m, "do the task", 5, false); err != nil {
		t.Fatalf("expected recovery, got: %v", err)
	}
	if len(*waits) != 1 || (*waits)[0] != 2*time.Second {
		t.Fatalf("expected a single 2s backoff, got %v", *waits)
	}
}

type errPlain string

func (e errPlain) Error() string { return string(e) }

// A tool call emitted as plain <function...> text must trigger the format
// correction and continue the loop — not be treated as a normal turn/exit.
func TestRunCorrectsUnparsedToolCall(t *testing.T) {
	noBackoff(t)
	// Note the content even ends with "DONE:"-ish intent, yet must NOT exit:
	textCall := &llm.Message{Role: "assistant", Content: `DONE: ran tests <function/run_bash({"command":"go test ./..."})</function>`}
	done := &llm.Message{Role: "assistant", Content: "DONE: actually ran the tests"}
	m := &mockClient{results: []mockResult{
		{textCall, nil}, // turn 1: tool call written as TEXT (and a false DONE)
		{done, nil},     // turn 2: proper completion
	}}

	final, _, err := RunCollect(m, "do the task", 5, false)
	if err != nil {
		t.Fatalf("expected loop to recover, got error: %v", err)
	}
	if m.calls != 2 {
		t.Fatalf("expected the text tool-call to be corrected and the loop to continue (2 calls), got %d", m.calls)
	}
	if final != "DONE: actually ran the tests" {
		t.Fatalf("expected the real DONE as final, got %q", final)
	}
	// Prove the *correction* was fed back (not the generic continue nudge).
	if !hasCorrection(m.lastIn) {
		t.Fatal("expected a tool-format correction message to be fed back before the next turn")
	}
}

func hasCorrection(msgs []llm.Message) bool {
	for _, mm := range msgs {
		if mm.Role == "user" && strings.Contains(mm.Content, "structured tool call") {
			return true
		}
	}
	return false
}

// Issue 1: an assistant turn may request MULTIPLE tool calls. Every one must be
// executed and its result fed back as a tool-role message keyed by the matching
// tool_call_id, before the loop takes its next turn.
func TestRunExecutesMultipleToolCallsInOneTurn(t *testing.T) {
	noBackoff(t)
	tools.Gate = tools.DestructiveGate{Allow: true}
	defer func() { tools.Gate = tools.DestructiveGate{} }()

	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.txt")
	fileB := filepath.Join(dir, "b.txt")

	multi := &llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{
		{ID: "call_1", Type: "function", Function: llm.FunctionCall{
			Name: "write_file", Arguments: `{"path":"` + fileA + `","content":"alpha"}`}},
		{ID: "call_2", Type: "function", Function: llm.FunctionCall{
			Name: "write_file", Arguments: `{"path":"` + fileB + `","content":"beta"}`}},
	}}
	done := &llm.Message{Role: "assistant", Content: "DONE: wrote both files"}
	m := &mockClient{results: []mockResult{
		{multi, nil}, // turn 1: two tool calls at once
		{done, nil},  // turn 2: completion
	}}

	if err := Run(m, "write both files", 5, false); err != nil {
		t.Fatalf("expected loop to complete, got: %v", err)
	}

	// Both tools actually ran.
	if b, err := os.ReadFile(fileA); err != nil || string(b) != "alpha" {
		t.Fatalf("first tool call did not execute: content=%q err=%v", b, err)
	}
	if b, err := os.ReadFile(fileB); err != nil || string(b) != "beta" {
		t.Fatalf("second tool call did not execute: content=%q err=%v", b, err)
	}

	// Both results were fed back as tool messages with matching IDs, before the
	// next turn (captured as the most recent Chat input).
	got := map[string]bool{}
	for _, mm := range m.lastIn {
		if mm.Role == "tool" {
			got[mm.ToolCallID] = true
		}
	}
	if !got["call_1"] || !got["call_2"] {
		t.Fatalf("expected tool results for both call_1 and call_2 fed back, got %v", got)
	}
}

// Issue 2: when a tool fails, its error result must be fed back to the model as
// a tool message (clearly, not swallowed) so the model can recover on the next
// turn. The loop must not abort just because one tool returned an error.
func TestRunFeedsBackToolError(t *testing.T) {
	noBackoff(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")

	failing := &llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{
		{ID: "call_err", Type: "function", Function: llm.FunctionCall{
			Name: "read_file", Arguments: `{"path":"` + missing + `"}`}},
	}}
	done := &llm.Message{Role: "assistant", Content: "DONE: recovered after the error"}
	m := &mockClient{results: []mockResult{
		{failing, nil}, // turn 1: a tool that errors
		{done, nil},    // turn 2: model recovers
	}}

	if err := Run(m, "read a missing file then recover", 5, false); err != nil {
		t.Fatalf("a failing tool must not abort the loop, got: %v", err)
	}
	if m.calls != 2 {
		t.Fatalf("expected the loop to continue past the tool error (2 calls), got %d", m.calls)
	}

	var fedBack string
	for _, mm := range m.lastIn {
		if mm.Role == "tool" && mm.ToolCallID == "call_err" {
			fedBack = mm.Content
		}
	}
	if !strings.HasPrefix(fedBack, "ERROR reading") {
		t.Fatalf("expected the tool error fed back clearly as a tool message, got %q", fedBack)
	}
}

func TestHasUnparsedToolCall(t *testing.T) {
	pos := []string{
		`<function/run_bash({"command":"ls"})</function>`,
		`text before <function=read_file{...}`,
		`<function name="run_bash">`,
	}
	for _, c := range pos {
		if !hasUnparsedToolCall(c) {
			t.Errorf("expected %q to be detected as an unparsed tool call", c)
		}
	}
	neg := []string{"DONE: all good", "I will run the tests next.", "the function works"}
	for _, c := range neg {
		if hasUnparsedToolCall(c) {
			t.Errorf("expected %q NOT to be flagged", c)
		}
	}
}

// Three consecutive tool-format corrections abort the iteration instead of
// looping all the way to max turns.
func TestRunAbortsAfterMaxCorrections(t *testing.T) {
	noBackoff(t)
	textCall := &llm.Message{Role: "assistant", Content: `<function/run_bash({"command":"ls"})</function>`}
	m := &mockClient{results: []mockResult{
		{textCall, nil}, // repeats forever: every turn is a text (unparsed) tool call
	}}

	_, _, err := RunCollect(m, "do the task", 10, false)
	if err == nil || !strings.Contains(err.Error(), "gave up after 3 correction attempts") {
		t.Fatalf("expected the correction-cap abort error, got %v", err)
	}
	// maxTurns is 10, so hitting exactly maxCorrections calls proves the abort
	// came from the correction cap, not the turn cap.
	if m.calls != maxCorrections {
		t.Fatalf("expected abort after %d corrections (%d Chat calls), got %d", maxCorrections, maxCorrections, m.calls)
	}
}

// A valid structured tool call mid-sequence resets the correction counter, so
// it takes a fresh run of corrections to abort.
func TestRunCorrectionCounterResetsOnValidToolCall(t *testing.T) {
	noBackoff(t)
	tools.Gate = tools.DestructiveGate{Allow: true}
	defer func() { tools.Gate = tools.DestructiveGate{} }()

	f := filepath.Join(t.TempDir(), "ok.txt")
	if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	text := &llm.Message{Role: "assistant", Content: `<function/run_bash({"command":"ls"})</function>`}
	valid := &llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
		ID: "c1", Type: "function",
		Function: llm.FunctionCall{Name: "read_file", Arguments: `{"path":"` + f + `"}`},
	}}}
	m := &mockClient{results: []mockResult{
		{text, nil},  // correction 1
		{text, nil},  // correction 2
		{valid, nil}, // valid structured tool call -> resets the counter
		{text, nil},  // correction 1 (again)
		{text, nil},  // correction 2
		{text, nil},  // correction 3 -> abort (and repeats from here)
	}}

	_, _, err := RunCollect(m, "do the task", 20, false)
	if err == nil || !strings.Contains(err.Error(), "gave up after 3 correction attempts") {
		t.Fatalf("expected the correction-cap abort error, got %v", err)
	}
	// Without the reset it would abort at the 3rd Chat call; the reset pushes
	// the abort out to the 6th.
	if m.calls != 6 {
		t.Fatalf("expected the reset to delay abort to the 6th Chat call, got %d", m.calls)
	}
}
