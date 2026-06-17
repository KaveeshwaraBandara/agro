# myagent

Model-agnostic coding agent in Go. Talks to any OpenAI-compatible `/chat/completions`
backend (Groq default), so you swap providers via env vars with no code change. Ships
three tools (`read_file`, `write_file`, `run_bash`) driven by a single-task agent loop:
model → tool calls → results → repeat until the model replies `DONE:`.

## Architecture

- `cmd/myagent` — CLI entry: flags (`-max-turns`, `-v`, `-auto`, `-max-iterations`, `-yes`), builds the client, runs the loop.
- `internal/llm` — OpenAI-compatible HTTP client + wire types; reads config from env. `APIError` carries status+body for retry classification.
- `internal/tools` — tool schemas + `Dispatch`; `Gate` blocks destructive `run_bash` commands unless `--yes`/confirmed.
- `internal/loop` — the agent loop: drives turns (`Run`/`RunCollect`), retries transient API failures, detects completion.
- `internal/auto` — bounded autonomous loop + STATE.md/CLAUDE.md state I/O (the phase-4 state code, implemented here for now).

## Conventions

- Tools return errors as **strings**, never panic — the model reads the error and recovers.
- Backend swapped via env: `AGENT_BASE_URL`, `AGENT_MODEL`, `AGENT_API_KEY` (key required).
- The loop exits when an assistant message starts with `DONE:`.
- Autonomous mode (`--auto`) is hard-capped at `--max-iterations` (default 10) and exits when STATE.md `Status: complete`.
- Destructive `run_bash` commands (rm/mv/dd/git push/...) are blocked unless `--yes` or interactively confirmed.

## Build / test

```bash
go build ./...
go vet ./...
go test ./...
```

## Roadmap

- [x] Phase 0/1: minimal loop + 3 tools
- [ ] Phase 2: tool-calling robustness (retries, parallel calls, errors)
- [ ] Phase 3: grep tool + better context management
- [ ] Phase 4: externalized state (CLAUDE.md + STATE.md, resumable)
- [ ] Phase 5: self-prompting autonomous loop + done-check + destructive-action gate
- [ ] Phase 6: CLI polish, packaging, docs

## Do not

- Don't break the OpenAI-compatible interface (keep the wire types and request shape portable).
- Don't add heavy dependencies — stay near the standard library.
- Don't make `run_bash` less safe.
