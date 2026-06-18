package loop

// Phase 4: externalized, resumable state. Two files at the repo root drive it:
//
//   CLAUDE.md  project conventions, loaded on startup and prepended to the
//              system context so the guide is always in scope.
//   STATE.md   an append-only, human-readable Markdown progress log. Each
//              completed run appends one record (timestamp, what was done,
//              status, next step); --resume parses it back to continue.
//
// This is the single-task counterpart to internal/auto's STATE.md handling,
// which instead rewrites a single snapshot each iteration of the autonomous loop.

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

	// stateHeader is written once, when STATE.md is first created.
	stateHeader = "# Task State\n\nAppend-only progress log maintained by agro. Newest entries at the bottom.\n"
)

// StateRecord is one progress entry in STATE.md.
type StateRecord struct {
	Time   time.Time // when the step completed (UTC, RFC3339 in the file)
	Done   string    // what was accomplished
	Status string    // current status, e.g. "complete" or "in-progress"
	Next   string    // the next concrete step, or "-"
}

// LoadProjectDoc returns the contents of CLAUDE.md in dir, or "" if it is
// absent (or unreadable) — an absent guide is not an error.
func LoadProjectDoc(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, projectDocFile))
	if err != nil {
		return ""
	}
	return string(b)
}

// AppendState appends one record to STATE.md in dir, creating the file (with a
// header) if it does not yet exist. The format is deliberately simple Markdown
// so it stays human-readable and round-trips through ParseState.
func AppendState(dir string, r StateRecord) error {
	path := filepath.Join(dir, stateFile)

	var b strings.Builder
	if _, err := os.Stat(path); os.IsNotExist(err) {
		b.WriteString(stateHeader)
	}
	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	fmt.Fprintf(&b, "\n## %s\n- Done: %s\n- Status: %s\n- Next: %s\n",
		ts.UTC().Format(time.RFC3339),
		blankToDash(r.Done), blankToDash(r.Status), blankToDash(r.Next))

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(b.String())
	return err
}

// ParseState reads STATE.md from dir and returns its records, oldest first. A
// missing file yields (nil, nil); only a real read error is reported.
func ParseState(dir string) ([]StateRecord, error) {
	b, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseStateRecords(string(b)), nil
}

// parseStateRecords parses the STATE.md body into records. Each "## <timestamp>"
// heading starts a new record; the "- Done/Status/Next:" bullets fill it. Lines
// before the first heading (the file header) are ignored.
func parseStateRecords(content string) []StateRecord {
	var recs []StateRecord
	var cur *StateRecord
	flush := func() {
		if cur != nil {
			recs = append(recs, *cur)
			cur = nil
		}
	}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "## "):
			flush()
			ts, _ := time.Parse(time.RFC3339, strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			cur = &StateRecord{Time: ts}
		case cur == nil:
			continue // header / preamble before the first record
		case strings.HasPrefix(line, "- Done:"):
			cur.Done = undash(strings.TrimSpace(strings.TrimPrefix(line, "- Done:")))
		case strings.HasPrefix(line, "- Status:"):
			cur.Status = undash(strings.TrimSpace(strings.TrimPrefix(line, "- Status:")))
		case strings.HasPrefix(line, "- Next:"):
			cur.Next = undash(strings.TrimSpace(strings.TrimPrefix(line, "- Next:")))
		}
	}
	flush()
	return recs
}

// resumeSeed turns the STATE.md history into a user message that seeds a resumed
// run with prior progress. Returns "" when there is nothing to resume from.
func resumeSeed(dir string) string {
	recs, err := ParseState(dir)
	if err != nil || len(recs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Resuming a prior run. Progress recorded so far (from STATE.md), oldest first:\n\n")
	for _, r := range recs {
		ts := "-"
		if !r.Time.IsZero() {
			ts = r.Time.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(&b, "- [%s] done: %s; status: %s; next: %s\n",
			ts, blankToDash(r.Done), blankToDash(r.Status), blankToDash(r.Next))
	}
	b.WriteString("\nContinue from here instead of starting over; do not redo work already marked done.")
	return b.String()
}

// doneSummary extracts a one-line summary from the agent's final message,
// preferring the text after the "DONE:" marker.
func doneSummary(final string) string {
	final = strings.TrimSpace(final)
	if rest, ok := strings.CutPrefix(final, "DONE:"); ok {
		final = strings.TrimSpace(rest)
	}
	if i := strings.IndexByte(final, '\n'); i >= 0 {
		final = strings.TrimSpace(final[:i])
	}
	return final
}

func blankToDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return strings.TrimSpace(s)
}

// undash is the inverse of blankToDash for parsing: a lone "-" placeholder reads
// back as an empty field.
func undash(s string) string {
	if s == "-" {
		return ""
	}
	return s
}
