package app

import (
	"context"
	"strings"
	"testing"

	"github.com/kydex/abx/internal/bwrap"
	"golang.org/x/sys/unix"
)

// Run must refuse a closed standard stream before anything else runs: the
// first retained source would otherwise take the freed descriptor number
// (0-2), and the session would inherit that descriptor as its stdio.
func TestRunRefusesClosedStandardStream(t *testing.T) {
	for fd, name := range standardStreams {
		t.Run(name, func(t *testing.T) {
			saved, err := unix.Dup(fd)
			if err != nil {
				t.Fatalf("dup descriptor %d: %v", fd, err)
			}
			if err := unix.Close(fd); err != nil {
				t.Fatalf("close descriptor %d: %v", fd, err)
			}
			defer func() {
				if err := unix.Dup2(saved, fd); err != nil {
					t.Fatalf("restore descriptor %d: %v", fd, err)
				}
				if err := unix.Close(saved); err != nil {
					t.Fatalf("close saved descriptor: %v", err)
				}
			}()
			var out, errOut strings.Builder
			status := Run(context.Background(), []string{"version"}, bwrap.Stdio{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if status != 1 {
				t.Fatalf("status = %d, want 1", status)
			}
			if want := name + " is closed"; !strings.Contains(errOut.String(), want) {
				t.Fatalf("stderr = %q, want it to name %q", errOut.String(), want)
			}
		})
	}
}
