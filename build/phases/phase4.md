Read the agro codebase (Go). Implement Phase 4: externalized, resumable
state. Specifically:
1. On startup, if `CLAUDE.md` exists at the repo root, load it and prepend its
   contents to the system context so project conventions are always in scope.
2. Maintain a `STATE.md` file at the repo root: after each completed step,
   append a short record (timestamp, what was done, current status, next step).
   Keep it human-readable Markdown.
3. Add a `--resume` flag. When set, read `STATE.md` on startup and seed the
   conversation with the prior progress so a re-run continues instead of
   restarting from scratch.
4. Add unit tests for: reading CLAUDE.md when present/absent, appending a
   STATE.md record, and parsing STATE.md back on resume.
Keep additions near the standard library; do not change the OpenAI-compatible
wire format. Run `go build ./...` and `go test ./...` and make sure both pass
before finishing. Reply starting with DONE: and a summary when complete.
