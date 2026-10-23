package bwrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A wedged bwrap must be reported as the probe deadline, not as the
// "signal: killed" that exec produces when CommandContext kills the child.
// The deadline is shortened here so the test observes the classification
// instead of waiting out the real one.
func TestVersionProbeTimeoutIsDiagnosed(t *testing.T) {
	bwrapProbeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { bwrapProbeTimeout = 5 * time.Second })
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
	want := fmt.Sprintf("timed out after %s", bwrapProbeTimeout)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("timeout diagnosis = %q, want %q", err.Error(), want)
	}
	if strings.Contains(err.Error(), "signal: killed") {
		t.Fatalf("leaked exec wording: %q", err.Error())
	}
	if elapsed > 5*time.Second {
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
	if !strings.Contains(err.Error(), "cancelled") || strings.Contains(err.Error(), "timed out after") {
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

// A caller deadline shorter than the probe budget must name the caller, not
// the probe's fixed five seconds: the child is killed by the parent deadline
// and never reaches the probe's own limit.
func TestVersionProbeCallerDeadlineIsNotProbeBudget(t *testing.T) {
	dir := t.TempDir()
	hang := filepath.Join(dir, "hang")
	if err := os.WriteFile(hang, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := open(ctx, hang, false)
	if err == nil {
		t.Fatal("opened a hanging tool")
	}
	if !strings.Contains(err.Error(), "caller deadline") || strings.Contains(err.Error(), "timed out after") {
		t.Fatalf("caller deadline reported as probe budget: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("probe ignored the caller deadline: %s", elapsed)
	}
}
