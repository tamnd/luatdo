package main

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/norm"
)

func TestEvidenceOffsetsCatchDrift(t *testing.T) {
	text := "Người sử dụng lao động phải trả lương đúng hạn."
	quote := "phải trả lương đúng hạn"
	at := strings.Index(text, quote)
	good := &norm.Statement{Evidence: norm.Evidence{Quote: quote, Start: at, End: at + len(quote)}}
	if err := evidenceOffsets(good, text); err != nil {
		t.Errorf("a quote at its own offsets: %v", err)
	}
	moved := &norm.Statement{Evidence: norm.Evidence{Quote: quote, Start: at + 1, End: at + 1 + len(quote)}}
	if err := evidenceOffsets(moved, text); err == nil {
		t.Error("offsets that moved under the record have to fail")
	}
	past := &norm.Statement{Evidence: norm.Evidence{Quote: quote, Start: 0, End: len(text) + 10}}
	if err := evidenceOffsets(past, text); err == nil {
		t.Error("offsets past the end of the provision have to fail")
	}
	if err := evidenceOffsets(&norm.Statement{}, text); err == nil {
		t.Error("no quote at all has to fail")
	}
}
