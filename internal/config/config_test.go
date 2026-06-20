package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agro", "config.json")
	in := &Config{
		Active: "gemini",
		Providers: map[string]Provider{
			"gemini": {BaseURL: "https://gen", Model: "gemini-2.5-flash", Key: "secret-key-1234"},
			"groq":   {BaseURL: "https://groq", Model: "llama", Key: "gk-abcd"},
		},
	}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config must be 0600 (holds secrets), got %o", perm)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Active != "gemini" || len(out.Providers) != 2 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Providers["gemini"] != in.Providers["gemini"] {
		t.Fatalf("provider not preserved: %+v", out.Providers["gemini"])
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	out, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.Active != "" || len(out.Providers) != 0 {
		t.Fatalf("expected an empty config, got %+v", out)
	}
}

func TestMask(t *testing.T) {
	if got := Mask("sk-1234567890"); got != "****7890" {
		t.Fatalf("Mask(long): got %q, want ****7890", got)
	}
	if got := Mask("sk-1234567890"); got == "sk-1234567890" {
		t.Fatal("Mask must never reveal the full key")
	}
	if got := Mask("abc"); got != "***" {
		t.Fatalf("Mask(short): got %q, want ***", got)
	}
	if got := Mask(""); got != "" {
		t.Fatalf("Mask(empty): got %q, want empty", got)
	}
}

func TestResolvePrecedence(t *testing.T) {
	cfg := &Config{
		Active: "gemini",
		Providers: map[string]Provider{
			"gemini": {BaseURL: "https://cfg-base", Model: "cfg-model", Key: "cfg-key"},
		},
	}
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	// 1. Env wins over config entirely.
	got, err := Resolve(env(map[string]string{
		"AGENT_API_KEY": "env-key", "AGENT_BASE_URL": "https://env-base", "AGENT_MODEL": "env-model",
	}), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Resolved{BaseURL: "https://env-base", Model: "env-model", Key: "env-key"}) {
		t.Fatalf("env should win, got %+v", got)
	}

	// 1b. Env key only => base/model fall back to Gemini defaults (not config).
	got, err = Resolve(env(map[string]string{"AGENT_API_KEY": "env-key"}), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != "env-key" || got.BaseURL != KnownProviders["gemini"].BaseURL || got.Model != KnownProviders["gemini"].Model {
		t.Fatalf("expected gemini defaults with env key, got %+v", got)
	}

	// 2. No env => active config provider.
	got, err = Resolve(env(nil), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Resolved{BaseURL: "https://cfg-base", Model: "cfg-model", Key: "cfg-key"}) {
		t.Fatalf("config active provider should be used, got %+v", got)
	}

	// 3. Neither => helpful error.
	if _, err := Resolve(env(nil), &Config{Providers: map[string]Provider{}}); err == nil {
		t.Fatal("expected ErrNoKey when no key is available anywhere")
	}
}

func TestPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if p != "/tmp/xdg-test/agro/config.json" {
		t.Fatalf("got %q", p)
	}
}
