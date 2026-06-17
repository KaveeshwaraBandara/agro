package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"agro/internal/llm"
)

// Gate guards run_bash against destructive commands. By default
// (Allow=false, Confirm=nil) any destructive command is BLOCKED. The CLI sets
// Allow=true on --yes, or installs an interactive Confirm hook otherwise.
var Gate = DestructiveGate{}

// DestructiveGate controls whether run_bash may execute destructive commands.
type DestructiveGate struct {
	Allow   bool                  // --yes: run destructive commands without prompting
	Confirm func(cmd string) bool // interactive confirmation; nil means "deny"
}

// destructivePatterns flags commands that can irreversibly delete or overwrite
// data. The matcher errs toward blocking (a stray "rm" in an argument trips it);
// for a safety gate that is the correct bias.
var destructivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\brm\b`),
	regexp.MustCompile(`\brmdir\b`),
	regexp.MustCompile(`\bmv\b`),
	regexp.MustCompile(`\bdd\b`),
	regexp.MustCompile(`\bgit\s+push\b`),
	regexp.MustCompile(`\bmkfs\b`),
	regexp.MustCompile(`\bshred\b`),
	regexp.MustCompile(`\btruncate\b`),
	regexp.MustCompile(`>\s*/`), // redirect/overwrite into an absolute path
	regexp.MustCompile(`:\s*>`), // truncate-to-empty
}

// matchDestructive returns the first matched pattern, or "" if none.
func matchDestructive(cmd string) string {
	for _, re := range destructivePatterns {
		if re.MatchString(cmd) {
			return re.String()
		}
	}
	return ""
}

// Schemas returns the OpenAI tool definitions advertised to the model.
func Schemas() []llm.Tool {
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	return []llm.Tool{
		{Type: "function", Function: llm.ToolFunction{
			Name:        "read_file",
			Description: "Read the contents of a file at the given path.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": str("Path to the file to read")},
				"required":   []string{"path"},
			},
		}},
		{Type: "function", Function: llm.ToolFunction{
			Name:        "write_file",
			Description: "Write content to a file, creating directories as needed. Overwrites if it exists.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    str("Path to the file to write"),
					"content": str("Full content to write to the file"),
				},
				"required": []string{"path", "content"},
			},
		}},
		{Type: "function", Function: llm.ToolFunction{
			Name:        "run_bash",
			Description: "Run a bash command in the working directory and return combined stdout/stderr.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"command": str("The bash command to run")},
				"required":   []string{"command"},
			},
		}},
	}
}

// Dispatch executes a tool call by name and returns a string result.
// Errors are returned as strings so the model can read and recover from them.
func Dispatch(name, argsJSON string) string {
	var args map[string]string
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("ERROR parsing arguments: %v", err)
	}
	switch name {
	case "read_file":
		return readFile(args["path"])
	case "write_file":
		return writeFile(args["path"], args["content"])
	case "run_bash":
		return runBash(args["command"])
	default:
		return fmt.Sprintf("ERROR: unknown tool %q", name)
	}
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("ERROR reading %s: %v", path, err)
	}
	return string(b)
}

func writeFile(path, content string) string {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Sprintf("ERROR creating dir for %s: %v", path, err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("ERROR writing %s: %v", path, err)
	}
	return fmt.Sprintf("OK: wrote %d bytes to %s", len(content), path)
}

func runBash(command string) string {
	if pat := matchDestructive(command); pat != "" && !Gate.Allow {
		if Gate.Confirm == nil || !Gate.Confirm(command) {
			return fmt.Sprintf("BLOCKED: command %q matches a destructive pattern (%s) and was not confirmed. Re-run with --yes to allow destructive commands.", command, pat)
		}
	}
	cmd := exec.Command("bash", "-c", command)
	out, err := cmd.CombinedOutput()
	result := string(out)
	if err != nil {
		result += fmt.Sprintf("\n[exit error: %v]", err)
	}
	if result == "" {
		result = "[no output]"
	}
	return result
}
