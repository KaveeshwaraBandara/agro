Read the agro codebase (Go). Implement Phase 6: CLI polish, packaging, docs.
Specifically:
1. Polish the CLI: clear `--help`/usage text, a `--version` flag, sensible exit
   codes (0 success, non-zero on failure), and a quieter default verbosity with
   `-v` opting into the detailed turn/tool trace.
2. Add packaging: a `Makefile` (or `build/release.sh`) that produces a single
   static binary (`CGO_ENABLED=0`) and stamps the version via `-ldflags`.
3. Update docs: refresh `README.md` install + usage to match the real CLI, and
   ensure `CLAUDE.md` reflects any new flags/commands.
4. Add unit tests for argument parsing and `--version` output.
Keep dependencies near the standard library; do not break the
OpenAI-compatible interface. Run `go build ./...` and `go test ./...` and make
sure both pass before finishing. Reply starting with DONE: and a summary when
complete.
