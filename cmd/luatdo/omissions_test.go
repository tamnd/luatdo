package main

import "testing"

// Two samples that cost hours of model calls each must not share a file name.
func TestRecoveryFileSeparatesTheGoldSampleFromTheQueue(t *testing.T) {
	scope := recoveryFile("tax-2025", false)
	gold := recoveryFile("tax-2025", true)
	if scope == gold {
		t.Fatalf("both samples write to %s, so the second run destroys the first", scope)
	}
	if scope != "recovery_tax-2025.json" || gold != "recovery_tax-2025_gold.json" {
		t.Errorf("names are %q and %q", scope, gold)
	}
	if got := recoveryFile("", false); got != "recovery_all.json" {
		t.Errorf("unscoped run writes %q", got)
	}
}
