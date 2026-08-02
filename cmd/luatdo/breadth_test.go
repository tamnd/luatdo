package main

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeBuffer is a bytes.Buffer the drain goroutine and the test can both touch.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

const drainNotice = "draining, signal again to abort"

// A run that finished everything it was given has not been interrupted, and
// saying so anyway makes a completed log indistinguishable from an abandoned
// one. Telling those apart is the whole reason the line exists.
func TestAFinishedRunDoesNotClaimItWasInterrupted(t *testing.T) {
	var out safeBuffer
	_, stop := drainOnSignal(&out, drainNotice)
	stop()
	// The notice is written by a goroutine, so a stop that merely raced it would
	// pass this test by finishing first rather than by being right.
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(out.String(), drainNotice) {
		t.Errorf("a run that was never signalled reported draining: %q", out.String())
	}
}
