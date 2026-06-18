package auto

import (
	"testing"

	"agro/internal/state"
)

// mockStepper completes on the completeAt-th call (0 = never completes).
// verified controls whether each iteration reports a real verification ran.
type mockStepper struct {
	calls      int
	completeAt int
	verified   bool
}

func (m *mockStepper) Step(_ string) (Result, error) {
	m.calls++
	return Result{
		Summary:  "did some work",
		Remains:  "more to do",
		Next:     "the next step",
		Complete: m.completeAt != 0 && m.calls >= m.completeAt,
		Verified: m.verified,
	}, nil
}

// The hard cap is enforced: a task that never completes stops at MaxIterations.
func TestRunEnforcesIterationCap(t *testing.T) {
	dir := t.TempDir()
	m := &mockStepper{completeAt: 0} // never reports complete
	err := Run("do it", m, Options{Dir: dir, MaxIterations: 3})
	if err == nil {
		t.Fatal("expected a cap error when the task never completes")
	}
	if m.calls != 3 {
		t.Fatalf("iteration cap not enforced: expected 3 steps, got %d", m.calls)
	}
	if state.IsComplete(dir) {
		t.Fatal("STATE.md should not be marked complete")
	}
}

// The done-check exits the loop early once STATE.md says complete.
func TestRunDoneCheckExitsEarly(t *testing.T) {
	dir := t.TempDir()
	m := &mockStepper{completeAt: 2, verified: true} // completes (and verified) on iteration 2
	err := Run("do it", m, Options{Dir: dir, MaxIterations: 10})
	if err != nil {
		t.Fatalf("expected clean exit on completion, got: %v", err)
	}
	if m.calls != 2 {
		t.Fatalf("done-check did not exit early: expected 2 steps, got %d", m.calls)
	}
	if !state.IsComplete(dir) {
		t.Fatal("STATE.md should be marked complete after early exit")
	}
}

// A zero/negative MaxIterations falls back to the non-negotiable default cap.
func TestRunDefaultsToTenIterations(t *testing.T) {
	dir := t.TempDir()
	m := &mockStepper{completeAt: 0}
	_ = Run("do it", m, Options{Dir: dir, MaxIterations: 0})
	if m.calls != DefaultMaxIterations {
		t.Fatalf("expected default cap %d, got %d", DefaultMaxIterations, m.calls)
	}
}

// COMPLETE: yes is rejected when no verification (run_bash) ever ran — the loop
// must keep pushing iterations instead of honoring a false completion.
func TestRunRejectsCompletionWithoutVerification(t *testing.T) {
	dir := t.TempDir()
	m := &mockStepper{completeAt: 1, verified: false} // claims complete, never verified
	err := Run("do it", m, Options{Dir: dir, MaxIterations: 3})
	if err == nil {
		t.Fatal("expected completion to be rejected when no run_bash executed")
	}
	if state.IsComplete(dir) {
		t.Fatal("STATE.md must NOT be marked complete without verification")
	}
	if m.calls != 3 {
		t.Fatalf("expected the loop to keep pushing to the cap, got %d iterations", m.calls)
	}
}

// Once verification has run, COMPLETE: yes is honored (guard does not over-reject).
func TestRunHonorsCompletionWithVerification(t *testing.T) {
	dir := t.TempDir()
	m := &mockStepper{completeAt: 1, verified: true}
	if err := Run("do it", m, Options{Dir: dir, MaxIterations: 5}); err != nil {
		t.Fatalf("expected verified completion to be honored, got: %v", err)
	}
	if !state.IsComplete(dir) {
		t.Fatal("STATE.md should be marked complete after verified completion")
	}
	if m.calls != 1 {
		t.Fatalf("expected exit on the first verified completion, got %d", m.calls)
	}
}

func TestParseResult(t *testing.T) {
	complete := parseResult("blah\nDONE: built it\nREMAINS: nothing\nNEXT: none\nCOMPLETE: yes")
	if !complete.Complete || complete.Summary != "built it" || complete.Remains != "nothing" || complete.Next != "none" {
		t.Fatalf("bad parse of complete footer: %+v", complete)
	}
	partial := parseResult("DONE: partial progress\nCOMPLETE: no")
	if partial.Complete {
		t.Fatal("COMPLETE: no must not be treated as complete")
	}
}
