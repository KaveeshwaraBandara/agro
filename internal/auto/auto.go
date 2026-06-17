package auto

import (
	"fmt"
	"strings"

	"myagent/internal/loop"
)

// DefaultMaxIterations caps the autonomous loop so it can never run forever.
const DefaultMaxIterations = 10

// Options configures an autonomous run.
type Options struct {
	Dir           string // directory holding CLAUDE.md / STATE.md
	MaxIterations int    // hard cap; <= 0 means DefaultMaxIterations
}

// Result is the outcome of one autonomous iteration.
type Result struct {
	Summary  string
	Remains  string
	Next     string
	Complete bool
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

	for i := 1; i <= max; i++ {
		ctx := buildContext(opts.Dir, task)

		res, err := s.Step(ctx)
		if err != nil {
			return fmt.Errorf("iteration %d: %w", i, err)
		}

		if err := writeState(opts.Dir, Record{
			Iteration: i,
			Summary:   res.Summary,
			Remains:   res.Remains,
			Next:      res.Next,
			Complete:  res.Complete,
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
	final, err := loop.RunCollect(s.Client, ctx+stepInstruction, s.MaxTurns, s.Verbose)
	if err != nil {
		return Result{}, err
	}
	return parseResult(final), nil
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
