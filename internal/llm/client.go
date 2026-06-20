package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// Client talks to any OpenAI-compatible /chat/completions endpoint.
// Swap providers (Groq, Gemini-OpenAI, OpenRouter, Cerebras, Ollama)
// by changing BaseURL + Model + the API key env var. No code change.
type Client struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client

	// MinInterval, when > 0, spaces consecutive requests at least this far
	// apart to stay under provider RPM limits proactively (vs. reacting to
	// 429s). Set via AGENT_MIN_REQUEST_INTERVAL.
	MinInterval time.Duration

	// throttle state + injectable clock (now/sleep are overridden in tests).
	mu      sync.Mutex
	lastReq time.Time
	now     func() time.Time
	sleep   func(time.Duration)
}

// New reads config from env with Gemini as the default backend (it handles
// structured tool calls cleanly). Groq and others remain swappable via env.
//   AGENT_BASE_URL              (default: https://generativelanguage.googleapis.com/v1beta/openai)
//   AGENT_MODEL                 (default: gemini-2.5-flash)
//   AGENT_API_KEY               (required)
//   AGENT_MIN_REQUEST_INTERVAL  (duration, e.g. "13s"; default 0 = off) — min
//                               spacing between requests for free-tier RPM caps
func New() (*Client, error) {
	key := os.Getenv("AGENT_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("AGENT_API_KEY not set")
	}
	base := getenv("AGENT_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai")
	model := getenv("AGENT_MODEL", "gemini-2.5-flash")
	return NewWith(base, model, key)
}

// NewWith builds a Client for an explicit backend (e.g. resolved from a stored
// provider profile). apiKey is required. Throttling still honors
// AGENT_MIN_REQUEST_INTERVAL.
func NewWith(baseURL, model, apiKey string) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required")
	}
	minInterval, err := parseInterval(os.Getenv("AGENT_MIN_REQUEST_INTERVAL"))
	if err != nil {
		return nil, err
	}
	return &Client{
		BaseURL:     baseURL,
		Model:       model,
		APIKey:      apiKey,
		HTTP:        &http.Client{Timeout: 120 * time.Second},
		MinInterval: minInterval,
		now:         time.Now,
		sleep:       time.Sleep,
	}, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// parseInterval parses an optional duration env value. "" => 0 (off).
func parseInterval(v string) (time.Duration, error) {
	if v == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid AGENT_MIN_REQUEST_INTERVAL %q: %w", v, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("AGENT_MIN_REQUEST_INTERVAL must be >= 0, got %q", v)
	}
	return d, nil
}

// throttle blocks until at least MinInterval has elapsed since the previous
// request, then records this request's time. No-op when MinInterval <= 0.
func (c *Client) throttle() {
	if c.MinInterval <= 0 {
		return
	}
	now := c.now
	if now == nil {
		now = time.Now
	}
	slp := c.sleep
	if slp == nil {
		slp = time.Sleep
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.lastReq.IsZero() {
		if wait := c.MinInterval - now().Sub(c.lastReq); wait > 0 {
			slp(wait)
		}
	}
	c.lastReq = now()
}

// --- Wire types (OpenAI chat completions subset) ---

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// APIError is returned by Chat for any non-2xx HTTP response. It carries the
// status code and raw body so callers can classify the failure (e.g. retry on
// 429/5xx, or on a 400 carrying "tool_use_failed" from Groq Llama models).
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API %d: %s", e.StatusCode, e.Body)
}

// Chat sends one turn and returns the assistant message.
func (c *Client) Chat(messages []Message, tools []Tool) (*Message, error) {
	c.throttle() // proactively space requests under provider RPM caps

	reqBody := chatRequest{Model: c.Model, Messages: messages, Tools: tools}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode: %w (body: %s)", err, string(data))
	}
	if out.Error != nil {
		return nil, fmt.Errorf("API error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}
	return &out.Choices[0].Message, nil
}
