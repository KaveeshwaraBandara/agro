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
