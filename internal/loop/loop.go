package loop

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"myagent/internal/llm"
	"myagent/internal/tools"
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

// maxChatRetries is the number of RETRIES after the initial attempt, so a
// transient turn is attempted up to 1+maxChatRetries times before giving up.
const maxChatRetries = 3

// retryBackoff is a package var so tests can zero it out to run instantly.
var retryBackoff = func(attempt int) time.Duration {
	return time.Duration(attempt) * 200 * time.Millisecond
}

// isRetryable reports whether a Chat error is a transient API failure worth
// retrying: any 429, any 5xx, or a 400 carrying "tool_use_failed" (Groq Llama
// intermittently emits malformed <function=...> tool-call syntax). All other
// errors — including non-API/network errors — fail immediately.
func isRetryable(err error) bool {
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch {
	case apiErr.StatusCode == http.StatusTooManyRequests:
		return true
	case apiErr.StatusCode >= 500 && apiErr.StatusCode <= 599:
		return true
	case apiErr.StatusCode == http.StatusBadRequest && strings.Contains(apiErr.Body, "tool_use_failed"):
		return true
	default:
		return false
	}
}

// chatWithRetry runs one turn, retrying transient failures with short backoff.
func chatWithRetry(client Chatter, messages []llm.Message, toolSchemas []llm.Tool, verbose bool) (*llm.Message, error) {
	var lastErr error
	for attempt := 0; attempt <= maxChatRetries; attempt++ {
		if attempt > 0 {
			if verbose {
				fmt.Printf("[retry %d/%d] transient error, backing off: %v\n", attempt, maxChatRetries, lastErr)
			}
			time.Sleep(retryBackoff(attempt))
		}
		msg, err := client.Chat(messages, toolSchemas)
		if err == nil {
			return msg, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("gave up after %d retries: %w", maxChatRetries, lastErr)
}

// Run executes the agent loop for a single task.
// maxTurns caps model turns so a stuck loop can't burn the whole quota.
func Run(client Chatter, task string, maxTurns int, verbose bool) error {
	_, err := RunCollect(client, task, maxTurns, verbose)
	return err
}

// RunCollect is Run but also returns the final assistant message text (the
// DONE: line on success, or the last assistant content if max turns is hit).
// The autonomous driver uses it to inspect the outcome of one iteration.
func RunCollect(client Chatter, task string, maxTurns int, verbose bool) (string, error) {
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
		assistant, err := chatWithRetry(client, messages, toolSchemas, verbose)
		if err != nil {
			return lastContent, fmt.Errorf("turn %d: %w", turn, err)
		}
		messages = append(messages, *assistant)
		if assistant.Content != "" {
			lastContent = assistant.Content
		}

		// No tool calls => model is talking. Check for completion.
		if len(assistant.ToolCalls) == 0 {
			if verbose && assistant.Content != "" {
				fmt.Println(assistant.Content)
			}
			if isDone(assistant.Content) {
				fmt.Println("\n" + assistant.Content)
				return assistant.Content, nil
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
	return lastContent, fmt.Errorf("hit max turns (%d) without completing", maxTurns)
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
