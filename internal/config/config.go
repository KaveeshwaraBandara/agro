// Package config stores and resolves provider profiles (base URL, model, API
// key) in ~/.config/agro/config.json so users don't have to re-export env vars.
// Resolution order for a run: environment (AGENT_*) first, then the active
// profile in the config file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Provider is one stored backend profile.
type Provider struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Key     string `json:"key"`
}

// Config is the on-disk shape: named providers plus the active one.
type Config struct {
	Active    string              `json:"active"`
	Providers map[string]Provider `json:"providers"`
}

// DefaultProvider is what `agro login` defaults to.
const DefaultProvider = "gemini"

// KnownProviders supplies default base_url/model for well-known backends so
// `agro login` only needs a key for them.
var KnownProviders = map[string]Provider{
	"gemini":     {BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-2.5-flash"},
	"groq":       {BaseURL: "https://api.groq.com/openai/v1", Model: "llama-3.3-70b-versatile"},
	"openrouter": {BaseURL: "https://openrouter.ai/api/v1", Model: ""},
	"cerebras":   {BaseURL: "https://api.cerebras.ai/v1", Model: "llama-3.3-70b"},
	"ollama":     {BaseURL: "http://localhost:11434/v1", Model: "qwen2.5-coder:7b"},
}

// ErrNoKey is returned by Resolve when neither env nor config provides a key.
var ErrNoKey = errors.New("no API key configured.\n" +
	"  Set AGENT_API_KEY, or run `agro login` to save one.\n" +
	"  Get a free Gemini key at https://aistudio.google.com/apikey")

// Path returns $XDG_CONFIG_HOME/agro/config.json, falling back to
// ~/.config/agro/config.json.
func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "agro", "config.json"), nil
}

// Load reads the config from path. A missing file yields an empty Config.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Providers: map[string]Provider{}}, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if c.Providers == nil {
		c.Providers = map[string]Provider{}
	}
	return &c, nil
}

// Save writes the config to path with 0600 perms (it holds secrets), creating
// the parent directory (0700) as needed.
func Save(path string, c *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// Mask hides all but the last 4 characters of a key, for display.
func Mask(key string) string {
	switch {
	case key == "":
		return ""
	case len(key) <= 4:
		return strings.Repeat("*", len(key))
	default:
		return "****" + key[len(key)-4:]
	}
}

// Resolved is a concrete backend selection.
type Resolved struct {
	BaseURL string
	Model   string
	Key     string
}

// Resolve selects the backend with precedence: environment (AGENT_*) first,
// then the active provider in cfg. Returns ErrNoKey if neither yields a key.
// getenv is injected for testability.
func Resolve(getenv func(string) string, cfg *Config) (Resolved, error) {
	if key := getenv("AGENT_API_KEY"); key != "" {
		base := getenv("AGENT_BASE_URL")
		if base == "" {
			base = KnownProviders[DefaultProvider].BaseURL
		}
		model := getenv("AGENT_MODEL")
		if model == "" {
			model = KnownProviders[DefaultProvider].Model
		}
		return Resolved{BaseURL: base, Model: model, Key: key}, nil
	}
	if cfg != nil && cfg.Active != "" {
		if p, ok := cfg.Providers[cfg.Active]; ok && p.Key != "" {
			return Resolved{BaseURL: p.BaseURL, Model: p.Model, Key: p.Key}, nil
		}
	}
	return Resolved{}, ErrNoKey
}
