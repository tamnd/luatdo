package temporal

import (
	"strings"
	"testing"
)

// amended builds the layer the competency question tests read: clause 2 of
// article 15 amended once in 2022 and again in 2023.
func amended(t *testing.T) *View {
	t.Helper()
	first := op("a1", KindAmend, clause2, "2022-07-01")
	first.NewText = "2. Tự nguyện, bình đẳng, thiện chí."
	second := op("a2", KindAmend, clause2, "2023-01-01")
	second.NewText = "2. Tự nguyện, bình đẳng, thiện chí, hợp tác."
	l, ties := Build(corpus(), []Operation{first, second})
	if len(ties) != 0 {
		t.Fatalf("unexpected ties: %v", ties)
	}
	return NewView(l)
}

// Competency question 16.
func TestQ16WhatItSaidOnTwoDates(t *testing.T) {
	v := amended(t)
	got := v.AskWhatItSaid(clause2, "2021-06-01", "2023-06-01")

	if !got.Changed {
		t.Error("the clause was amended twice and the answer says it did not change")
	}
	if strings.Contains(got.EarlyText, "thiện chí") {
		t.Errorf("the early date shows the later text: %q", got.EarlyText)
	}
	if !strings.Contains(got.LateText, "hợp tác") {
		t.Errorf("the late date does not show the latest text: %q", got.LateText)
	}
	if len(got.Events) != 2 {
		t.Fatalf("the answer names %d events between the dates, want 2", len(got.Events))
	}
	for _, e := range got.Events {
		if e.CausedByDoc != amendDoc {
			t.Errorf("an event names %q as the instrument that caused it", e.CausedByDoc)
		}
	}
}

func TestQ16OnADateBeforeTheLawExisted(t *testing.T) {
	v := amended(t)
	got := v.AskWhatItSaid(clause2, "2015-01-01", "2023-06-01")
	if got.EarlyText != "" {
		t.Errorf("the clause did not exist in 2015 and the answer has text for it: %q", got.EarlyText)
	}
	if _, ok := v.TextAt(clause2, "2015-01-01"); ok {
		t.Error("TextAt reported a version on a date before the law took effect")
	}
}

// Competency question 17.
func TestQ17ShortLivedVersions(t *testing.T) {
	v := amended(t)
	got := v.AskShortLived(365)

	if len(got) == 0 {
		t.Fatal("the 2022 version lasted 184 days and nothing was reported")
	}
	found := false
	for _, s := range got {
		if s.ComponentID == clause2 && s.Days == 184 {
			found = true
			if s.EndedByDoc != amendDoc {
				t.Errorf("the version was ended by %q", s.EndedByDoc)
			}
		}
	}
	if !found {
		t.Errorf("the 184 day version of the clause is not in the answer: %+v", got)
	}
	for _, s := range got {
		if s.To == "" {
			t.Error("a version still in force was counted as short lived, which makes the answer change every day without the corpus moving")
		}
	}
}

func TestQ17IsSortedShortestFirst(t *testing.T) {
	v := amended(t)
	got := v.AskShortLived(3650)
	for i := 1; i < len(got); i++ {
		if got[i-1].Days > got[i].Days {
			t.Fatalf("position %d lasted %d days and position %d lasted %d", i-1, got[i-1].Days, i, got[i].Days)
		}
	}
}

// Competency question 18.
func TestQ18AmendmentHistory(t *testing.T) {
	v := amended(t)
	got := v.AskHistory(clause2)

	if len(got) != 3 {
		t.Fatalf("the clause has %d steps in its history, want the original and two amendments", len(got))
	}
	if got[0].EventKind != KindEnact {
		t.Errorf("the first step is %q, want the enactment", got[0].EventKind)
	}
	for i, s := range got[1:] {
		if s.EventKind != KindAmend {
			t.Errorf("step %d is %q", i+1, s.EventKind)
		}
		if s.CausedBy != amendDoc {
			t.Errorf("step %d names %q as the instrument", i+1, s.CausedBy)
		}
		if s.Instruction == "" {
			t.Errorf("step %d quotes no instruction, so nobody can check it", i+1)
		}
	}
	// The chain is contiguous: each step ends where the next begins.
	for i := 1; i < len(got); i++ {
		if got[i-1].To != got[i].From {
			t.Errorf("step %d ends %s and step %d starts %s", i-1, got[i-1].To, i, got[i].From)
		}
	}
	if got[len(got)-1].To != "" {
		t.Error("the last version of an unrepealed clause has an end date")
	}
}

func TestHistoryOfSomethingNobodyTouched(t *testing.T) {
	v := amended(t)
	if got := v.AskHistory("vn:law:2019:45-2019-qh14:article-99"); len(got) != 0 {
		t.Errorf("a component with no versions has an empty history, got %d steps", len(got))
	}
}

func TestTextAtAssemblesChildren(t *testing.T) {
	v := amended(t)
	text, ok := v.TextAt(article15, "2021-06-01")
	if !ok {
		t.Fatal("article 15 has no version when the law took effect")
	}
	for _, want := range []string{"Điều 15.", "Giao kết trung thực", "Tự nguyện, bình đẳng"} {
		if !strings.Contains(text, want) {
			t.Errorf("the assembled article is missing %q:\n%s", want, text)
		}
	}
	// Order is the order of the children, not of the identifiers.
	if strings.Index(text, "Giao kết") > strings.Index(text, "Tự nguyện") {
		t.Error("the clauses came out in the wrong order")
	}
}

func TestInForceAtExcludesTheSuspended(t *testing.T) {
	l, _ := Build(corpus(), []Operation{op("p1", KindSuspend, clause2, "2022-07-01")})
	v := NewView(l)
	for _, ver := range v.InForceAt(docID, "2022-09-01") {
		if ver.ComponentID == clause2 {
			t.Error("a suspended clause was returned as in force")
		}
	}
}

func TestLastChangeIsTheDayTheNewestVersionBegins(t *testing.T) {
	o := op("p1", KindAmend, clause2, "2022-07-01")
	o.NewText = "van ban moi"
	l, _ := Build(corpus(), []Operation{o})
	v := NewView(l)
	if got := v.LastChange(docID); got != "2022-07-01" {
		t.Errorf("LastChange = %q, want the day the amendment landed", got)
	}
	if got := v.LastChange("vn:law:1990:no-such-doc"); got != "" {
		t.Errorf("LastChange of an unversioned document = %q, want empty", got)
	}
}

func TestDaysBetween(t *testing.T) {
	cases := []struct {
		from, to string
		want     int
		ok       bool
	}{
		{"2022-07-01", "2023-01-01", 184, true},
		{"2020-02-28", "2020-03-01", 2, true}, // 2020 is a leap year
		{"2021-02-28", "2021-03-01", 1, true},
		{"2022-01-01", "2022-01-01", 0, true},
		{"khong phai ngay", "2022-01-01", 0, false},
	}
	for _, c := range cases {
		got, ok := daysBetween(c.from, c.to)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("daysBetween(%s, %s) = %d, %v, want %d, %v", c.from, c.to, got, ok, c.want, c.ok)
		}
	}
}
