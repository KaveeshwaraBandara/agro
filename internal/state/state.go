// Package state externalizes the agent's progress files: CLAUDE.md (the project
// guide) and STATE.md (per-run progress). The autonomous loop reads/writes these
// through this package so a run is inspectable and resumable. Moved here from
// internal/auto so the state I/O is reusable and independently testable.
package state

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

// ProjectDoc returns CLAUDE.md from dir, or "" if absent.
func ProjectDoc(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, projectDocFile))
	if err != nil {
		return ""
	}
	return string(b)
}

// Read returns the raw STATE.md from dir, or "" if absent.
func Read(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err != nil {
		return ""
	}
	return string(b)
}

// Write rewrites STATE.md with the given record.
func Write(dir string, r Record) error {
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

// IsComplete reports whether STATE.md marks the task complete. The done-check
// reads the persisted file so completion is decided by STATE.md, not merely by
// an iteration returning.
func IsComplete(dir string) bool {
	for _, line := range strings.Split(Read(dir), "\n") {
		if strings.TrimSpace(line) == "Status: "+statusComplete {
			return true
		}
	}
	return false
}

// Clear removes STATE.md so a run starts fresh. A missing file is not an error.
func Clear(dir string) error {
	err := os.Remove(filepath.Join(dir, stateFile))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// BuildContext assembles the per-iteration prompt context: CLAUDE.md (project
// guide) + STATE.md (progress so far) + the task.
func BuildContext(dir, task string) string {
	var b strings.Builder
	if doc := ProjectDoc(dir); doc != "" {
		b.WriteString("# Project guide (CLAUDE.md)\n")
		b.WriteString(doc)
		b.WriteString("\n\n")
	}
	if st := Read(dir); st != "" {
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
