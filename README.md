# agro — a lightweight, model-agnostic coding agent

A minimal coding agent in Go. Talks to any OpenAI-compatible API, so you swap
between Gemini (default), Groq, OpenRouter, Cerebras, or local Ollama by changing
config — no code changes. This is the **Phase 0 + Phase 1 slice**: a working
single-task agent loop with three tools.

## Why Go

The runtime bottleneck in a coding agent is the LLM API call (seconds), not your
own code (microseconds). So raw execution speed barely matters; startup time and
single-binary distribution do. Go gives you both without Rust's development drag.

## What works now

- OpenAI-compatible LLM client (swappable backend)
- Three tools: `read_file`, `write_file`, `run_bash`
- Single-task agent loop: model -> tool calls -> results -> repeat -> `DONE:`
- `--max-turns` safety cap so a stuck loop can't drain your quota

## Build

```bash
go build -o agro ./cmd/agro
```

## Run

```bash
export AGENT_API_KEY=your_key_here
./agro "create a file primes.py that prints the first 20 primes, then run it"
```

## Provider config (swap by env var)

| Provider  | AGENT_BASE_URL                          | AGENT_MODEL (example)        |
|-----------|-----------------------------------------|------------------------------|
| Gemini ★  | https://generativelanguage.googleapis.com/v1beta/openai | gemini-2.5-flash |
| Groq      | https://api.groq.com/openai/v1          | llama-3.3-70b-versatile      |
| OpenRouter| https://openrouter.ai/api/v1            | (any :free model)            |
| Cerebras  | https://api.cerebras.ai/v1              | llama-3.3-70b                |
| Ollama    | http://localhost:11434/v1               | qwen2.5-coder:7b             |

Default backend is **Gemini** (★) — it handles structured tool calls cleanly,
whereas Llama-on-Groq frequently malformed them. Just set `AGENT_API_KEY` and go;
override `AGENT_BASE_URL` / `AGENT_MODEL` to switch to any provider above.

> Rate limits: the Gemini free tier allows ~5 requests/minute, so the autonomous
> loop (`--auto`) can hit `429`s on bursts. The client's per-class backoff honors
> the provider's retry hint and retries, so this is handled — runs just slow down.
>
> To stay under the cap *proactively* (instead of reacting to 429s), set
> `AGENT_MIN_REQUEST_INTERVAL` — the client spaces consecutive requests at least
> that far apart. For the Gemini free tier (5 RPM) use `13s`:
>
> ```bash
> export AGENT_MIN_REQUEST_INTERVAL=13s
> ```
>
> Default is `0` (off).

> Privacy note: free no-credit-card tiers are typically funded by your prompts.
> Keep secrets and customer data off them.

## Layout

```
cmd/agro/main.go      CLI entry
internal/llm/client.go   OpenAI-compatible client
internal/tools/tools.go  read_file, write_file, run_bash
internal/loop/loop.go    the agent loop
```

## Roadmap (the full plan)

- [x] Phase 0/1: minimal loop + 3 tools  <- you are here
- [ ] Phase 2: tool-calling robustness (retries, parallel calls, errors)
- [ ] Phase 3: grep tool + better context management
- [ ] Phase 4: externalized state (CLAUDE.md + STATE.md, resumable)
- [ ] Phase 5: self-prompting autonomous loop + done-check + destructive-action gate
- [ ] Phase 6: CLI polish, packaging, docs

---

## Next: refine with Claude Code (Loop A)

Validate this slice first by running a real task against Groq. Then hand the next
phase to Claude Code headless. Kickoff prompt for Phase 2:

```
Read the agro codebase (Go). It's a coding agent with an OpenAI-compatible
client, three tools, and a single-task loop. Implement Phase 2: tool-calling
robustness. Specifically:
1. Handle multiple tool calls in one assistant turn (already looped — verify and test).
2. Add per-tool error recovery so a failed tool result is fed back clearly.
3. Add a `grep` tool (search files by regex, return path:line:match).
4. Add a unit test for tools.Dispatch covering each tool and the error path.
Run `go build ./...` and `go test ./...` and make sure both pass before finishing.
Reply starting with DONE: and a summary when complete.
```

Run it headless:

```bash
claude -p "$(cat build/phases/phase2.md)" \
  --allowedTools "Read,Write,Bash" \
  --permission-mode dontAsk \
  --output-format stream-json \
  --max-turns 30
```

Wrap that in `driver.sh` with a gate (`go build && go test`) that advances to the
next phase only when it passes — that's Loop A.
