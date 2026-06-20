package main

import (
	"testing"
	"time"

	"agro/internal/config"
)

// writeActiveProfile saves a config with one active provider, via the real
// config package, at the location resolveClient will read.
func writeActiveProfile(t *testing.T, name, key string) error {
	t.Helper()
	path, err := config.Path()
	if err != nil {
		return err
	}
	p := config.KnownProviders[name]
	p.Key = key
	return config.Save(path, &config.Config{
		Active:    name,
		Providers: map[string]config.Provider{name: p},
	})
}

// Regression guard: the CLI resolution path (config.Resolve -> llm.NewWith) must
// apply AGENT_MIN_REQUEST_INTERVAL to the constructed client. If construction is
// ever routed around the env read again, MinInterval would silently stay 0 and
// this test fails.
func TestResolveClientAppliesThrottleInterval(t *testing.T) {
	// Isolate from any real ~/.config/agro/config.json; with an env key,
	// resolution uses the env path.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AGENT_API_KEY", "test-key")
	t.Setenv("AGENT_MIN_REQUEST_INTERVAL", "13s")

	client, err := resolveClient()
	if err != nil {
		t.Fatalf("resolveClient: %v", err)
	}
	if client.MinInterval != 13*time.Second {
		t.Fatalf("throttle not applied via CLI path: MinInterval = %s, want 13s", client.MinInterval)
	}
}

// And it holds for the stored-profile path too (no env key; active profile used).
func TestResolveClientThrottleViaStoredProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AGENT_API_KEY", "") // force the config-file branch
	t.Setenv("AGENT_MIN_REQUEST_INTERVAL", "7s")

	// Write a config with an active provider via the real config package path.
	if err := writeActiveProfile(t, "gemini", "stored-key"); err != nil {
		t.Fatal(err)
	}

	client, err := resolveClient()
	if err != nil {
		t.Fatalf("resolveClient: %v", err)
	}
	if client.APIKey != "stored-key" {
		t.Fatalf("expected stored-profile key, got %q", client.APIKey)
	}
	if client.MinInterval != 7*time.Second {
		t.Fatalf("throttle not applied via stored-profile path: MinInterval = %s, want 7s", client.MinInterval)
	}
}
