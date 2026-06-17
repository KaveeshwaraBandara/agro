package loop

import (
	"testing"
	"time"

	"agro/internal/llm"
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
}

func (m *mockClient) Chat(_ []llm.Message, _ []llm.Tool) (*llm.Message, error) {
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
	orig := retryBackoff
	retryBackoff = func(int) time.Duration { return 0 }
	t.Cleanup(func() { retryBackoff = orig })
}

func toolUseFailed() error {
	return &llm.APIError{StatusCode: 400, Body: `{"error":{"code":"tool_use_failed","message":"<function=read_file>..."}}`}
}

// The primary target: an intermittent tool_use_failed 400 should be retried
// and the loop should recover when the next attempt succeeds.
func TestRunRecoversFromToolUseFailed(t *testing.T) {
	noBackoff(t)
	done := &llm.Message{Role: "assistant", Content: "DONE: created primes.py"}
	m := &mockClient{results: []mockResult{
		{nil, toolUseFailed()}, // turn 1, attempt 1: transient 400
		{done, nil},            // turn 1, attempt 2: recovers
	}}

	if err := Run(m, "do the task", 5, false); err != nil {
		t.Fatalf("expected loop to recover, got error: %v", err)
	}
	if m.calls != 2 {
		t.Fatalf("expected 2 Chat calls (1 failure + 1 success), got %d", m.calls)
	}
}

// The give-up path: a persistent transient error is retried maxChatRetries
// times (4 calls total: 1 initial + 3 retries) and then the loop fails.
func TestRunGivesUpAfterRetries(t *testing.T) {
	noBackoff(t)
	m := &mockClient{results: []mockResult{
		{nil, toolUseFailed()}, // repeats forever
	}}

	err := Run(m, "do the task", 5, false)
	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	if m.calls != 1+maxChatRetries {
		t.Fatalf("expected %d Chat calls (1 initial + %d retries), got %d", 1+maxChatRetries, maxChatRetries, m.calls)
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

// 429 and 5xx are also retryable.
func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"429", &llm.APIError{StatusCode: 429, Body: "rate limited"}, true},
		{"500", &llm.APIError{StatusCode: 500, Body: "boom"}, true},
		{"503", &llm.APIError{StatusCode: 503, Body: "unavailable"}, true},
		{"400 tool_use_failed", toolUseFailed(), true},
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

type errPlain string

func (e errPlain) Error() string { return string(e) }
