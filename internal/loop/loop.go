package loop

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"agro/internal/llm"
	"agro/internal/tools"
)

const systemPrompt = `You are a coding agent. You complete the user's task by using the
provided tools: read_file, write_file, run_bash. Work step by step. Inspect the workspace
before changing it. When the task is fully complete, reply with a final message that begins
with "DONE:" and briefly summarizes what you did. Do not say DONE until the work is verified.`

// Chatter is the subset of *llm.Client the loop depends on. Accepting an
// interface (rather than the concrete client) lets tests inject a mock.
type Chatter interface {
	Chat(messages []llm.Message, tools []llm.Tool) (*llm.Message, error)
}

// Retry caps per transient error class. A 400 tool_use_failed is NOT transient
// (retrying reproduces the same malformed output), so it is excluded here and
// routed to the format-correction path in RunCollect instead.
const (
	maxRetries429 = 5 // 429s respect the provider's retry hint, so allow more
	maxRetries5xx = 3
)

// sleep is overridable in tests so backoffs don't actually block.
var sleep = time.Sleep

// retryHintRe extracts a provider-suggested wait, e.g. "Please try again in 2s"
// or "try again in 1.5s", from a 429 response body.
var retryHintRe = regexp.MustCompile(`(?i)try again in\s+([0-9]+(?:\.[0-9]+)?)\s*s`)

func is429(err error) bool {
	var apiErr *llm.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests
}

func is5xx(err error) bool {
	var apiErr *llm.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode >= 500 && apiErr.StatusCode <= 599
}

// isRetryable reports whether a Chat error is a genuinely transient API failure
// (429 or 5xx). tool_use_failed and all other errors are NOT retryable.
func isRetryable(err error) bool { return is429(err) || is5xx(err) }

// isToolUseFailed reports the Groq 400 "tool_use_failed" case, where the model
// emitted a malformed tool call. This is not transient and must be corrected.
func isToolUseFailed(err error) bool {
	var apiErr *llm.APIError
	return errors.As(err, &apiErr) &&
		apiErr.StatusCode == http.StatusBadRequest &&
		strings.Contains(apiErr.Body, "tool_use_failed")
}

// backoff429 returns how long to wait before a 429 retry: at least the
// provider's suggested time if present, otherwise exponential starting at 2s
// (2s, 4s, 8s, ...). attempt is 1-based.
func backoff429(err error, attempt int) time.Duration {
	if d, ok := parseRetryHint(err); ok {
		return d
	}
	return 2 * time.Second * time.Duration(1<<(attempt-1))
}

func parseRetryHint(err error) (time.Duration, bool) {
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		return 0, false
	}
	m := retryHintRe.FindStringSubmatch(apiErr.Body)
	if m == nil {
		return 0, false
	}
	secs, perr := strconv.ParseFloat(m[1], 64)
	if perr != nil {
		return 0, false
	}
	return time.Duration(secs * float64(time.Second)), true
}

// chatWithRetry runs one turn, retrying only genuinely transient failures.
// 429s honor the provider's retry hint (or exponential-from-2s) and allow more
// attempts; 5xx use a short linear backoff. Non-transient errors (including
// tool_use_failed) are returned immediately for the caller to handle.
func chatWithRetry(client Chatter, messages []llm.Message, toolSchemas []llm.Tool, verbose bool) (*llm.Message, error) {
	var n429, n5xx int
	for {
		msg, err := client.Chat(messages, toolSchemas)
		if err == nil {
			return msg, nil
		}
		switch {
		case is429(err):
			if n429 >= maxRetries429 {
				return nil, err
			}
			n429++
			wait := backoff429(err, n429)
			if verbose {
				fmt.Printf("[retry 429 %d/%d] rate limited, waiting %s: %v\n", n429, maxRetries429, wait, err)
			}
			sleep(wait)
		case is5xx(err):
			if n5xx >= maxRetries5xx {
				return nil, err
			}
			n5xx++
			wait := time.Duration(n5xx) * 200 * time.Millisecond
			if verbose {
				fmt.Printf("[retry 5xx %d/%d] waiting %s: %v\n", n5xx, maxRetries5xx, wait, err)
			}
			sleep(wait)
		default:
			return nil, err
		}
	}
}

// Run executes the agent loop for a single task.
// maxTurns caps model turns so a stuck loop can't burn the whole quota.
func Run(client Chatter, task string, maxTurns int, verbose bool) error {
	_, _, err := RunCollect(client, task, maxTurns, verbose)
	return err
}

// toolFormatCorrection is fed back when the model writes a tool call as plain
// text (<function...>) instead of a structured tool_call. That text never
// executes, so we must not treat it as progress or completion.
const toolFormatCorrection = "Your last message contained a tool call written as plain text " +
	"(e.g. `<function...>`). That text is NOT executed — the tool did not run and nothing changed. " +
	"Re-emit the call as a proper structured tool call (the API tool_calls field), not as text. " +
	"Never claim a step succeeded until the tool actually ran and you have seen its result."

// hasUnparsedToolCall reports whether content contains a tool call the model
// wrote as literal text rather than a structured tool_call. Covers the
// <function, <function/ and <function= variants Groq Llama models emit.
func hasUnparsedToolCall(content string) bool {
	return strings.Contains(content, "<function")
}

// RunCollect is Run but also returns the final assistant message text (the
// DONE: line on success, or the last assistant content if max turns is hit)
// and whether a run_bash tool actually executed during the run. The autonomous
// driver uses both to inspect the outcome of one iteration.
func RunCollect(client Chatter, task string, maxTurns int, verbose bool) (final string, ranBash bool, err error) {
	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: task},
	}
	toolSchemas := tools.Schemas()
	lastContent := ""

	for turn := 1; turn <= maxTurns; turn++ {
		if verbose {
			fmt.Printf("\n--- turn %d/%d ---\n", turn, maxTurns)
		}
		assistant, cerr := chatWithRetry(client, messages, toolSchemas, verbose)
		if cerr != nil {
			// A malformed tool call (400 tool_use_failed) is NOT transient —
			// retrying reproduces it. Feed back a format correction and take a
			// fresh turn instead of failing the run.
			if isToolUseFailed(cerr) {
				if verbose {
					fmt.Printf("[correction] tool_use_failed; requesting a structured tool call (turn %d)\n", turn)
				}
				messages = append(messages, llm.Message{
					Role:    "user",
					Content: toolFormatCorrection,
				})
				continue
			}
			return lastContent, ranBash, fmt.Errorf("turn %d: %w", turn, cerr)
		}
		messages = append(messages, *assistant)
		if assistant.Content != "" {
			lastContent = assistant.Content
		}

		// No structured tool calls => the model is talking (or wrote a tool
		// call as text). Check for the text-tool-call bug before completion.
		if len(assistant.ToolCalls) == 0 {
			if verbose && assistant.Content != "" {
				fmt.Println(assistant.Content)
			}

			// The model sometimes emits a tool call as literal <function...>
			// text. It never ran, so do NOT honor it as progress or a DONE:
			// — correct the format and continue. Checked BEFORE isDone so a
			// "DONE:" mixed with <function...> can't slip through.
			if hasUnparsedToolCall(assistant.Content) {
				if verbose {
					fmt.Println("[correction] unparsed tool-call syntax in content; requesting a structured tool call")
				}
				messages = append(messages, llm.Message{
					Role:    "user",
					Content: toolFormatCorrection,
				})
				continue
			}

			if isDone(assistant.Content) {
				fmt.Println("\n" + assistant.Content)
				return assistant.Content, ranBash, nil
			}
			// Nudge it to either act or declare done.
			messages = append(messages, llm.Message{
				Role:    "user",
				Content: "Continue. Use a tool to make progress, or reply starting with DONE: if finished.",
			})
			continue
		}

		// Execute each requested tool call and feed results back.
		for _, tc := range assistant.ToolCalls {
			if verbose {
				fmt.Printf("[tool] %s %s\n", tc.Function.Name, tc.Function.Arguments)
			}
			result := tools.Dispatch(tc.Function.Name, tc.Function.Arguments)
			// Track that a real verification command ran (a blocked command
			// did not actually execute, so it doesn't count).
			if tc.Function.Name == "run_bash" && !strings.HasPrefix(result, "BLOCKED:") {
				ranBash = true
			}
			if verbose {
				fmt.Printf("[result] %s\n", truncate(result, 500))
			}
			messages = append(messages, llm.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    result,
			})
		}
	}
	return lastContent, ranBash, fmt.Errorf("hit max turns (%d) without completing", maxTurns)
}

func isDone(content string) bool {
	return len(content) >= 5 && content[:5] == "DONE:"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}
