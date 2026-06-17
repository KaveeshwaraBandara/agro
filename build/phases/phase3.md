Read the agro codebase (Go). It's a coding agent with an OpenAI-compatible
client, tools, and a single-task loop. Implement Phase 3: grep + context
management. Specifically:
1. Ensure a `grep` tool exists (added in Phase 2): search files by regex and
   return `path:line:match` lines. If missing, add it. Cover it with a test.
2. Truncate large tool results in the MESSAGE HISTORY (not just on-screen):
   cap each tool result at a configurable byte budget, keeping head and tail,
   with a `[... truncated N bytes ...]` marker in the middle.
3. Add a total context budget for the message slice. When exceeded, drop or
   compact the OLDEST tool results first while always keeping the system
   prompt and the most recent turns intact.
4. Add unit tests for the per-result truncation and the history-trimming logic
   (including the boundary where nothing needs trimming).
Run `go build ./...` and `go test ./...` and make sure both pass before finishing.
Reply starting with DONE: and a summary when complete.
