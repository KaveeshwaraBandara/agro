package auto

// State I/O for the autonomous loop. NOTE: phase 4 (externalized CLAUDE.md +
// STATE.md state) was never actually built — the driver was not run — so the
// minimal STATE.md / CLAUDE.md handling the autonomous loop needs lives here.
// If phase 4 is implemented later as its own package, this can move there.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	projectDocFile = "CLAUDE.md"
	stateFile      = "STATE.md"
	statusComplete = "complete"
	statusActive   = "in-progress"
)

// Record is one snapshot of task progress, serialized to STATE.md each iteration.
type Record struct {
	Iteration int
	Summary   string // what was done
	Remains   string // what remains
	Next      string // the next concrete step
	Complete  bool   // the overall task is finished
}

// projectDoc returns CLAUDE.md from dir, or "" if absent.
func projectDoc(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, projectDocFile))
	if err != nil {
		return ""
	}
	return string(b)
}

// readState returns the raw STATE.md from dir, or "" if absent.
func readState(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err != nil {
		return ""
	}
	return string(b)
}

// writeState rewrites STATE.md with the given record.
func writeState(dir string, r Record) error {
	status := statusActive
	if r.Complete {
		status = statusComplete
	}
	content := fmt.Sprintf(`# Task State

Status: %s
Iteration: %d
Updated: %s

## Done
%s

## Remaining
%s

## Next step
%s
`,
		status, r.Iteration, time.Now().UTC().Format(time.RFC3339),
		blankToDash(r.Summary), blankToDash(r.Remains), blankToDash(r.Next))
	return os.WriteFile(filepath.Join(dir, stateFile), []byte(content), 0o644)
}

// isComplete reports whether STATE.md marks the task complete. The done-check
// reads the persisted file so completion is decided by STATE.md, not merely by
// an iteration returning.
func isComplete(dir string) bool {
	for _, line := range strings.Split(readState(dir), "\n") {
		if strings.TrimSpace(line) == "Status: "+statusComplete {
			return true
		}
	}
	return false
}

// buildContext assembles the per-iteration prompt context: CLAUDE.md (project
// guide) + STATE.md (progress so far) + the task.
func buildContext(dir, task string) string {
	var b strings.Builder
	if doc := projectDoc(dir); doc != "" {
		b.WriteString("# Project guide (CLAUDE.md)\n")
		b.WriteString(doc)
		b.WriteString("\n\n")
	}
	if st := readState(dir); st != "" {
		b.WriteString("# Progress so far (STATE.md)\n")
		b.WriteString(st)
		b.WriteString("\n\n")
	}
	b.WriteString("# Task\n")
	b.WriteString(task)
	return b.String()
}

func blankToDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return strings.TrimSpace(s)
}
