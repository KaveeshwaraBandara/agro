Read the agro codebase (Go). It's a coding agent with an OpenAI-compatible
client, three tools, and a single-task loop. Implement Phase 2: tool-calling
robustness. Specifically:
1. Handle multiple tool calls in one assistant turn (already looped — verify and test).
2. Add per-tool error recovery so a failed tool result is fed back clearly.
3. Add a `grep` tool (search files by regex, return path:line:match).
4. Add a unit test for tools.Dispatch covering each tool and the error path.
Run `go build ./...` and `go test ./...` and make sure both pass before finishing.
Reply starting with DONE: and a summary when complete.
