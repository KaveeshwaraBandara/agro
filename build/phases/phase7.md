Read the agro codebase (Go). Implement Phase 7: CLI key / provider
management.

NOTE: the original feature description was not supplied; this spec is an
inference from the project's existing env-var config model
(AGENT_BASE_URL / AGENT_MODEL / AGENT_API_KEY). Adjust the requirements if the
intended design differs.

Goal: let users store and switch between named provider profiles instead of
re-exporting env vars every time. Specifically:
1. Add a `keys` subcommand group:
   - `agro keys set <name> --key K [--base-url U] [--model M]` — save a
     profile (e.g. groq, gemini, openrouter, cerebras, ollama).
   - `agro keys list` — list profile names with base-url/model and a MASKED
     key (e.g. `sk-…last4`); never print full secrets.
   - `agro keys use <name>` — mark a profile as the active default.
   - `agro keys rm <name>` — delete a profile.
2. Persist profiles to a config file under
   `${XDG_CONFIG_HOME:-$HOME/.config}/agro/config.json`, created with `0600`
   permissions (it holds secrets).
3. Resolution order when running a task (highest wins): explicit env vars
   (AGENT_API_KEY/BASE_URL/MODEL) > `--provider <name>` flag > the active
   default profile > built-in Groq defaults. The required-key check still
   applies after resolution.
4. Add unit tests for: config load/save round-trip, the key-masking helper,
   and the resolution-order precedence (env over profile over default).
Keep it near the standard library; do not break the OpenAI-compatible
interface. Run `go build ./...` and `go test ./...` and make sure both pass
before finishing. Reply starting with DONE: and a summary when complete.

Amend build/phases/phase7.md: when a key is added (keys set / login), validate it
with a single test /chat/completions call against the selected provider before
writing it to disk. On failure, do not save — print a clear error pointing the
user to where they generate a free Groq key. Keep the existing keys set/list/use/rm
design otherwise.