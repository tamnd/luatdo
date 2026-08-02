//go:build !windows

package main

import (
	"strings"
	"syscall"
	"testing"
	"time"
)

// The other half of the same claim. Windows has no SIGTERM to send, so this one
// is built out there rather than skipped, and the case beside it still runs
// everywhere because it is the one an ordinary passing run exercises.
func TestAnInterruptedRunSaysSoAndCancelsTheContext(t *testing.T) {
	var out safeBuffer
	ctx, stop := drainOnSignal(&out, drainNotice)
	defer stop()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the signal did not cancel the context, so nothing would stop starting work")
	}
	// The context is cancelled before the notice is written, so this waits on
	// the goroutine rather than on the signal.
	time.Sleep(50 * time.Millisecond)
	if !strings.Contains(out.String(), drainNotice) {
		t.Errorf("an interrupted run said nothing about it: %q", out.String())
	}
}
