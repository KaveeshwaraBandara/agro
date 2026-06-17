package auto

import "testing"

// mockStepper completes on the completeAt-th call (0 = never completes).
type mockStepper struct {
	calls      int
	completeAt int
}

func (m *mockStepper) Step(_ string) (Result, error) {
	m.calls++
	return Result{
		Summary:  "did some work",
		Remains:  "more to do",
		Next:     "the next step",
		Complete: m.completeAt != 0 && m.calls >= m.completeAt,
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
	if isComplete(dir) {
		t.Fatal("STATE.md should not be marked complete")
	}
}

// The done-check exits the loop early once STATE.md says complete.
func TestRunDoneCheckExitsEarly(t *testing.T) {
	dir := t.TempDir()
	m := &mockStepper{completeAt: 2} // completes on the 2nd iteration
	err := Run("do it", m, Options{Dir: dir, MaxIterations: 10})
	if err != nil {
		t.Fatalf("expected clean exit on completion, got: %v", err)
	}
	if m.calls != 2 {
		t.Fatalf("done-check did not exit early: expected 2 steps, got %d", m.calls)
	}
	if !isComplete(dir) {
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
