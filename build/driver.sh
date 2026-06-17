#!/usr/bin/env bash
#
# driver.sh — drive Claude Code headless to build agro phase by phase.
#
# For each build/phases/phaseN.md (in order), run Claude Code, then GATE on
# `go build ./...` && `go test ./...`. Advance only when the gate passes.
# On failure, re-run the SAME phase once with the error appended; if it still
# fails, stop and report which phase failed and why.
#
# Progress is recorded in build/.driver_state (last completed phase number),
# so re-running resumes instead of redoing completed phases.

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PHASES_DIR="build/phases"
STATE_FILE="build/.driver_state"
START_PHASE=2          # Phase 0/1 ship in the starter; specs begin at phase2.
MAX_TURNS=30
# From this phase on, a phase that adds no tests fails the gate. phase2.md
# onward is specified to add tests, so the bar applies from phase 2.
TESTS_REQUIRED_FROM_PHASE=2

banner() {
  echo
  echo "============================================================"
  echo "  $*"
  echo "============================================================"
}

# run_claude <spec_file> [extra_context]
run_claude() {
  local spec_file="$1" extra="${2:-}"
  local prompt
  prompt="$(cat "$spec_file")"
  if [[ -n "$extra" ]]; then
    prompt+=$'\n\n'"$extra"
  fi
  claude -p "$prompt" \
    --allowedTools "Read,Write,Bash" \
    --permission-mode dontAsk \
    --max-turns "$MAX_TURNS"
}

# src_md5: a single fingerprint of every Go file under cmd/ and internal/.
# Used to detect a no-op phase (claude changed nothing). Includes *_test.go,
# so adding a test alone still registers as a change.
src_md5() {
  find cmd internal -type f -name '*.go' -print0 2>/dev/null \
    | sort -z | xargs -0 md5sum 2>/dev/null | md5sum | awk '{print $1}'
}

# run_gate <phase> <src_before_md5>
# Echoes diagnostics; returns 0 only if the phase did real, tested work:
#   1. go build ./...                     must pass
#   2. Go source under cmd/ or internal/  must have changed (no-op detection)
#   3. go test ./...                      must pass
#   4. from TESTS_REQUIRED_FROM_PHASE on  at least one package must run tests
#      ("[no test files]" everywhere => no tests were added => fail)
run_gate() {
  local phase="$1" before="$2" out tout trc after

  if ! out="$(go build ./... 2>&1)"; then
    echo "$out"
    echo "GATE FAIL: go build ./... failed"
    return 1
  fi

  after="$(src_md5)"
  if [[ "$after" == "$before" ]]; then
    echo "GATE FAIL: no Go source under cmd/ or internal/ changed during phase $phase (no-op)"
    return 1
  fi

  tout="$(go test ./... 2>&1)"; trc=$?
  echo "$tout"
  if (( trc != 0 )); then
    echo "GATE FAIL: go test ./... failed"
    return 1
  fi

  if (( phase >= TESTS_REQUIRED_FROM_PHASE )); then
    if ! grep -qE '^ok[[:space:]]' <<<"$tout"; then
      echo "GATE FAIL: phase $phase ran no tests (every package reported [no test files]); this phase is expected to add tests"
      return 1
    fi
    if grep -q '\[no test files\]' <<<"$tout"; then
      echo "GATE WARN: some packages still have no test files (soft warning, not a failure)"
    fi
  fi
  return 0
}

# --- resume from last completed phase -------------------------------------
last_done=0
if [[ -f "$STATE_FILE" ]]; then
  last_done="$(tr -dc '0-9' < "$STATE_FILE")"
  last_done="${last_done:-0}"
fi

if (( last_done >= START_PHASE )); then
  n=$(( last_done + 1 ))
  echo "Resuming: last completed phase = $last_done, next = $n"
else
  n=$START_PHASE
fi

# --- main loop ------------------------------------------------------------
ran_any=0
while [[ -f "$PHASES_DIR/phase$n.md" ]]; do
  ran_any=1
  spec="$PHASES_DIR/phase$n.md"

  banner "PHASE $n — $spec"
  src_before="$(src_md5)"      # snapshot before claude runs, for no-op detection
  run_claude "$spec"

  banner "GATE (phase $n): build + source-changed + tests"
  gate_out="$(run_gate "$n" "$src_before" 2>&1)"; gate_rc=$?
  echo "$gate_out"

  if (( gate_rc != 0 )); then
    banner "GATE FAILED (phase $n) — retrying once with error context"
    run_claude "$spec" \
"The previous attempt did not pass the gate. The gate requires: \`go build ./...\`
passes, Go source under cmd/ or internal/ actually changed, \`go test ./...\`
passes, and this phase adds at least one test. Fix the code so they all hold.
Here is the failing gate output:

$gate_out"

    banner "GATE RETRY (phase $n)"
    gate_out="$(run_gate "$n" "$src_before" 2>&1)"; gate_rc=$?
    echo "$gate_out"
    if (( gate_rc != 0 )); then
      banner "STOPPED: phase $n failed the gate after one retry"
      echo "Reason: see the GATE FAIL line(s) above (build, no-op source, test failure, or missing tests)."
      echo "State file unchanged (last completed: $last_done)."
      exit 1
    fi
  fi

  echo "$n" > "$STATE_FILE"
  last_done=$n
  banner "PHASE $n COMPLETE ✓"
  n=$(( n + 1 ))
done

if (( ran_any == 0 )); then
  banner "Nothing to do — all phases through $last_done already complete."
else
  banner "ALL PHASES COMPLETE ✓ (through phase $last_done)"
fi
