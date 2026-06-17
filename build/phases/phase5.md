Read the myagent codebase (Go). Implement Phase 5: autonomous loop with safety.
Specifically:
1. Add an autonomous mode (flag `--auto`) where, given a high-level task, the
   agent self-prompts the next concrete sub-goal each iteration and continues
   until the top-level task is satisfied or max-turns is hit.
2. Add a done-check: before accepting a `DONE:` message, run a verification
   pass that confirms the claim against real state (e.g. `go build` succeeds,
   referenced files exist). If verification fails, feed the gap back and keep
   going instead of exiting.
3. Add a destructive-action gate for `run_bash`: detect dangerous commands
   (e.g. `rm -rf`, `git push --force`, `:>` truncation, `mkfs`, fork bombs).
   In non-interactive mode, block them and return a clear refusal string the
   model can read; do not weaken existing run_bash safety.
4. Add unit tests for the destructive-pattern matcher (positive + negative
   cases) and for the done-check accept/reject decision.
Run `go build ./...` and `go test ./...` and make sure both pass before finishing.
Reply starting with DONE: and a summary when complete.
