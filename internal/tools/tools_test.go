package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A destructive command is blocked when --yes is not set (default gate denies).
func TestRunBashBlocksDestructiveWithoutYes(t *testing.T) {
	Gate = DestructiveGate{} // default: Allow=false, Confirm=nil => deny
	defer func() { Gate = DestructiveGate{} }()

	marker := filepath.Join(t.TempDir(), "must-not-be-deleted")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := Dispatch("run_bash", `{"command":"rm -rf `+marker+`"}`)
	if !strings.HasPrefix(out, "BLOCKED:") {
		t.Fatalf("expected destructive command to be BLOCKED, got: %s", out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("blocked command must NOT have executed, but file is gone: %v", err)
	}
}

// With --yes (Gate.Allow), the destructive command actually runs.
func TestRunBashAllowsDestructiveWithYes(t *testing.T) {
	Gate = DestructiveGate{Allow: true}
	defer func() { Gate = DestructiveGate{} }()

	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := Dispatch("run_bash", `{"command":"mv `+src+` `+dst+`"}`)
	if strings.HasPrefix(out, "BLOCKED:") {
		t.Fatalf("with --yes the command should run, got: %s", out)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("expected mv to have executed: %v", err)
	}
}

func TestMatchDestructive(t *testing.T) {
	dangerous := []string{
		"rm -rf /", "sudo rm x", "mv a b", "dd if=/dev/zero of=x",
		"git push origin main", "shred f", "mkfs.ext4 /dev/sda",
		"echo hi > /etc/passwd", ": > file",
	}
	for _, c := range dangerous {
		if matchDestructive(c) == "" {
			t.Errorf("expected %q to match a destructive pattern", c)
		}
	}
	safe := []string{"ls -la", "go build ./...", "cat file.txt", "git status", "echo hello"}
	for _, c := range safe {
		if p := matchDestructive(c); p != "" {
			t.Errorf("expected %q to be safe, but matched %q", c, p)
		}
	}
}

// TestDispatch exercises every tool plus the error paths, all through the
// public Dispatch entrypoint (the one the loop actually calls).
func TestDispatch(t *testing.T) {
	Gate = DestructiveGate{Allow: true} // allow run_bash here; gate is tested separately
	defer func() { Gate = DestructiveGate{} }()

	dir := t.TempDir()
	mustJSON := func(v map[string]string) string {
		var b strings.Builder
		b.WriteByte('{')
		first := true
		for k, val := range v {
			if !first {
				b.WriteByte(',')
			}
			first = false
			b.WriteString(quote(k))
			b.WriteByte(':')
			b.WriteString(quote(val))
		}
		b.WriteByte('}')
		return b.String()
	}

	t.Run("write_file then read_file round-trips", func(t *testing.T) {
		path := filepath.Join(dir, "sub", "hello.txt")
		w := Dispatch("write_file", mustJSON(map[string]string{"path": path, "content": "hello world"}))
		if !strings.HasPrefix(w, "OK:") {
			t.Fatalf("write_file: expected OK, got %q", w)
		}
		r := Dispatch("read_file", mustJSON(map[string]string{"path": path}))
		if r != "hello world" {
			t.Fatalf("read_file: expected round-trip content, got %q", r)
		}
	})

	t.Run("run_bash returns command output", func(t *testing.T) {
		out := Dispatch("run_bash", `{"command":"echo agro-ok"}`)
		if !strings.Contains(out, "agro-ok") {
			t.Fatalf("run_bash: expected echoed output, got %q", out)
		}
	})

	t.Run("grep finds matches as path:line:match", func(t *testing.T) {
		path := filepath.Join(dir, "code.go")
		if err := os.WriteFile(path, []byte("package main\nfunc Foo() {}\nfunc Bar() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out := Dispatch("grep", mustJSON(map[string]string{"pattern": `func \w+`, "path": path}))
		if !strings.Contains(out, path+":2:func Foo() {}") {
			t.Fatalf("grep: expected path:line:match for Foo, got %q", out)
		}
		if !strings.Contains(out, path+":3:func Bar() {}") {
			t.Fatalf("grep: expected path:line:match for Bar, got %q", out)
		}
		if strings.Contains(out, ":1:") {
			t.Fatalf("grep: package line should not match %q, got %q", `func \w+`, out)
		}
	})

	t.Run("grep over a directory recurses", func(t *testing.T) {
		out := Dispatch("grep", mustJSON(map[string]string{"pattern": "hello world", "path": dir}))
		if !strings.Contains(out, "hello.txt:1:hello world") {
			t.Fatalf("grep dir: expected to find file under %s, got %q", dir, out)
		}
	})

	t.Run("grep with no match reports cleanly", func(t *testing.T) {
		out := Dispatch("grep", mustJSON(map[string]string{"pattern": "zzz-nope-zzz", "path": dir}))
		if !strings.HasPrefix(out, "[no matches") {
			t.Fatalf("grep: expected a no-match notice, got %q", out)
		}
	})

	// --- error paths: all returned as strings, never panics or empty ---

	t.Run("unknown tool", func(t *testing.T) {
		out := Dispatch("does_not_exist", `{}`)
		if !strings.HasPrefix(out, "ERROR: unknown tool") {
			t.Fatalf("expected unknown-tool error, got %q", out)
		}
	})

	t.Run("malformed arguments JSON", func(t *testing.T) {
		out := Dispatch("read_file", `{not valid json`)
		if !strings.HasPrefix(out, "ERROR parsing arguments") {
			t.Fatalf("expected arg-parse error, got %q", out)
		}
	})

	t.Run("read_file missing file", func(t *testing.T) {
		out := Dispatch("read_file", mustJSON(map[string]string{"path": filepath.Join(dir, "nope.txt")}))
		if !strings.HasPrefix(out, "ERROR reading") {
			t.Fatalf("expected read error, got %q", out)
		}
	})

	t.Run("grep invalid regex", func(t *testing.T) {
		out := Dispatch("grep", mustJSON(map[string]string{"pattern": "(unclosed", "path": dir}))
		if !strings.HasPrefix(out, "ERROR compiling regex") {
			t.Fatalf("expected regex-compile error, got %q", out)
		}
	})
}

// quote is a tiny JSON string encoder sufficient for the filesystem paths and
// patterns used in these tests (no embedded quotes/newlines).
func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}
