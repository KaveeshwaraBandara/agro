package auto

import (
	"fmt"
	"strings"

	"agro/internal/loop"
	"agro/internal/state"
)

// DefaultMaxIterations caps the autonomous loop so it can never run forever.
const DefaultMaxIterations = 10

// Options configures an autonomous run.
type Options struct {
	Dir           string // directory holding CLAUDE.md / STATE.md
	MaxIterations int    // hard cap; <= 0 means DefaultMaxIterations
	Resume        bool   // continue from an existing STATE.md instead of starting fresh
}

// isComplete reports whether STATE.md marks the task complete. Thin wrapper over
// state.IsComplete, used by Run and the package tests.
func isComplete(dir string) bool { return state.IsComplete(dir) }

// Result is the outcome of one autonomous iteration.
type Result struct {
	Summary  string
	Remains  string
	Next     string
	Complete bool
	Verified bool // a verification command (run_bash) actually executed this iteration
}

// Stepper performs one autonomous iteration given the accumulated context.
// Real impl: LLMStepper. Tests inject a mock.
type Stepper interface {
	Step(ctx string) (Result, error)
}

// Run drives the autonomous loop: read context (CLAUDE.md + STATE.md) -> step
// -> rewrite STATE.md -> done-check. It is bounded by a hard iteration cap that
// can never be exceeded, so the loop can never run forever.
func Run(task string, s Stepper, opts Options) error {
	max := opts.MaxIterations
	if max <= 0 {
		max = DefaultMaxIterations
	}

	// Unless resuming, discard any STATE.md from a previous run so we start
	// fresh. With --resume, the existing STATE.md is left in place and seeded
	// into the context by state.BuildContext below.
	if !opts.Resume {
		if err := state.Clear(opts.Dir); err != nil {
			return fmt.Errorf("clearing prior state: %w", err)
		}
	}

	verified := false // has a verification command (run_bash) executed in any iteration?
	for i := 1; i <= max; i++ {
		ctx := state.BuildContext(opts.Dir, task)

		res, err := s.Step(ctx)
		if err != nil {
			return fmt.Errorf("iteration %d: %w", i, err)
		}
		if res.Verified {
			verified = true
		}

		// Completion guard: never honor COMPLETE: yes unless a verification
		// command (run_bash) has actually executed. Otherwise the model can
		// mark the task done on a false belief (e.g. "tests passed" but the
		// test command never ran). When completion is claimed without
		// verification, reject it and push one more iteration demanding the
		// verification actually run.
		complete := res.Complete && verified
		next := res.Next
		if res.Complete && !verified {
			next = "Completion was claimed but NO verification command (run_bash) has executed yet. " +
				"Actually run the verification (e.g. the tests / the program) and include its real output before claiming COMPLETE: yes."
		}

		if err := state.Write(opts.Dir, state.Record{
			Iteration: i,
			Summary:   res.Summary,
			Remains:   res.Remains,
			Next:      next,
			Complete:  complete,
		}); err != nil {
			return fmt.Errorf("iteration %d: writing STATE.md: %w", i, err)
		}

		// Done-check is driven by STATE.md, not merely by the iteration ending.
		if isComplete(opts.Dir) {
			return nil
		}
	}
	return fmt.Errorf("reached iteration cap (%d) without completing; see STATE.md for remaining work", max)
}

// LLMStepper is the production Stepper: it runs the agent for one chunk of work,
// then parses a status footer to decide whether the overall task is complete.
type LLMStepper struct {
	Client   loop.Chatter
	MaxTurns int
	Verbose  bool
}

const stepInstruction = `

Do the next concrete step toward the Task above. Use the tools to make real
progress. End your final message with EXACTLY these four lines:
DONE: <what you accomplished this iteration>
REMAINS: <what is left, or "nothing">
NEXT: <the next concrete step, or "none">
COMPLETE: yes   (only if the entire Task is finished; otherwise COMPLETE: no)`

func (s LLMStepper) Step(ctx string) (Result, error) {
	final, ranBash, err := loop.RunCollect(s.Client, ctx+stepInstruction, s.MaxTurns, s.Verbose)
	if err != nil {
		return Result{}, err
	}
	r := parseResult(final)
	r.Verified = ranBash // completion is only honored if a run_bash actually ran
	return r, nil
}

// parseResult extracts the status footer from the agent's final message.
func parseResult(final string) Result {
	var r Result
	for _, raw := range strings.Split(final, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "DONE:"):
			r.Summary = strings.TrimSpace(strings.TrimPrefix(line, "DONE:"))
		case strings.HasPrefix(line, "REMAINS:"):
			r.Remains = strings.TrimSpace(strings.TrimPrefix(line, "REMAINS:"))
		case strings.HasPrefix(line, "NEXT:"):
			r.Next = strings.TrimSpace(strings.TrimPrefix(line, "NEXT:"))
		case strings.HasPrefix(line, "COMPLETE:"):
			v := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "COMPLETE:")))
			r.Complete = v == "yes" || v == "true" || v == "done"
		}
	}
	return r
}
