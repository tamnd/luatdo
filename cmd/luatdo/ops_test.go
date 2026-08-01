package main

import (
	"runtime"
	"testing"
)

func TestParallelism(t *testing.T) {
	auto, err := parallelism("auto")
	if err != nil {
		t.Fatalf("auto: %v", err)
	}
	// Workers wait on a remote service, not on the processor, so auto stays
	// small however big the machine is.
	if auto < 2 || auto > 4 {
		t.Errorf("auto = %d on a %d core machine, want between 2 and 4", auto, runtime.NumCPU())
	}
	if got, err := parallelism("12"); err != nil || got != 12 {
		t.Errorf("parallelism(12) = %d, %v", got, err)
	}
	if got, err := parallelism(" 3 "); err != nil || got != 3 {
		t.Errorf("parallelism with spaces = %d, %v", got, err)
	}
	for _, bad := range []string{"0", "-1", "many"} {
		if _, err := parallelism(bad); err == nil {
			t.Errorf("parallelism(%q) must fail rather than guess", bad)
		}
	}
}

func TestParallelismFromEnvironment(t *testing.T) {
	t.Setenv("LUATDO_PARALLEL", "7")
	if got, err := parallelism("auto"); err != nil || got != 7 {
		t.Errorf("auto with LUATDO_PARALLEL set = %d, %v", got, err)
	}
	// An explicit flag beats the environment, because it is the thing the
	// operator typed on this run.
	if got, err := parallelism("2"); err != nil || got != 2 {
		t.Errorf("explicit worker count = %d, %v", got, err)
	}
}
