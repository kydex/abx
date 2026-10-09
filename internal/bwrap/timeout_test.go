package bwrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A wedged bwrap must be reported as the 5s probe deadline, not as the
// "signal: killed" that exec produces when CommandContext kills the child.
func TestVersionProbeTimeoutIsDiagnosed(t *testing.T) {
	dir := t.TempDir()
	hang := filepath.Join(dir, "hang")
	if err := os.WriteFile(hang, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := open(context.Background(), hang, false)
	if err == nil {
		t.Fatal("opened a hanging tool")
	}
	elapsed := time.Since(start)
	if !strings.Contains(err.Error(), "timed out after 5s") {
		t.Fatalf("timeout diagnosis = %q", err.Error())
	}
	if strings.Contains(err.Error(), "signal: killed") {
		t.Fatalf("leaked exec wording: %q", err.Error())
	}
	if elapsed > 15*time.Second {
		t.Fatalf("probe took %s", elapsed)
	}
}

// Caller cancellation is not a probe timeout.
func TestVersionProbeCallerCancellationIsDistinct(t *testing.T) {
	path := writeScript(t, `if [ "$1" = "--version" ]; then
  printf 'bubblewrap 0.12.0\n'
fi`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := open(ctx, path, false)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if strings.Contains(err.Error(), "timed out after 5s") {
		t.Fatalf("cancellation reported as probe timeout: %q", err.Error())
	}
}

func TestVersionProbeFailureKeepsUnderlyingError(t *testing.T) {
	path := writeScript(t, `exit 9`)
	_, err := open(context.Background(), path, false)
	if err == nil || !strings.Contains(err.Error(), "bubblewrap version") {
		t.Fatalf("probe failure = %v", err)
	}
}
