package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDebugOptionsWithoutPath proves the flag creates nothing when it is absent.
func TestDebugOptionsWithoutPath(t *testing.T) {
	options, closeLog, err := debugOptions("")
	if err != nil {
		t.Fatalf("debugOptions(\"\"): %v", err)
	}
	if len(options) != 0 {
		t.Errorf("got %d mail options without a path, want none", len(options))
	}

	closeLog() // must be safe to call with nothing open.
}

// TestDebugOptionsCreatesPrivateFile pins the mode: the log carries the account's
// mail traffic, so it must not be world-readable.
func TestDebugOptionsCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chute.log")

	options, closeLog, err := debugOptions(path)
	if err != nil {
		t.Fatalf("debugOptions: %v", err)
	}
	defer closeLog()

	if len(options) != 1 {
		t.Fatalf("got %d mail options, want one writer", len(options))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

// TestDebugOptionsTruncates proves an earlier run's log cannot be read as this
// run's evidence: a stale MOVE line from yesterday would otherwise look like
// today's.
func TestDebugOptionsTruncates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chute.log")
	if err := os.WriteFile(path, []byte("A1 UID MOVE stale\r\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, closeLog, err := debugOptions(path)
	if err != nil {
		t.Fatalf("debugOptions: %v", err)
	}
	closeLog()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("log still holds %q, want an empty file", data)
	}
}

// TestDebugOptionsReportsAnUnopenablePath proves a bad path fails loudly instead of
// silently running without the log the user asked for.
func TestDebugOptionsReportsAnUnopenablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "chute.log")

	if _, _, err := debugOptions(path); err == nil {
		t.Fatal("debugOptions accepted a path under a directory that does not exist")
	}
}
