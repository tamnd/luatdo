package main

import "testing"

func TestAParameterIsANumberWhenItLooksLikeOne(t *testing.T) {
	// A query comparing a string to a numeric property returns nothing at all,
	// silently, which reads as an empty answer rather than as a mistake. So the
	// number case is the one worth getting right without being asked.
	p := paramFlag{}
	for _, arg := range []string{"days=5", "date=2023-01-01", "doc=vn:law:2019:45-2019-qh14"} {
		if err := p.Set(arg); err != nil {
			t.Fatalf("set %s: %v", arg, err)
		}
	}
	if p["days"] != 5 {
		t.Errorf("days is %#v, want the number 5", p["days"])
	}
	// A date is digits and dashes and is not a number. Atoi says so, which is
	// the whole reason the guess is safe.
	if p["date"] != "2023-01-01" {
		t.Errorf("date is %#v", p["date"])
	}
	if p["doc"] != "vn:law:2019:45-2019-qh14" {
		t.Errorf("doc is %#v", p["doc"])
	}
}

func TestAParameterWithoutAValueIsAUsageError(t *testing.T) {
	p := paramFlag{}
	for _, arg := range []string{"days", "=5", ""} {
		if err := p.Set(arg); err == nil {
			t.Errorf("%q was accepted as a parameter", arg)
		}
	}
}

func TestPickQuestionRefusesANumberNobodyAsks(t *testing.T) {
	if _, err := pickQuestion(0); err == nil {
		t.Error("no question number was accepted")
	}
	if _, err := pickQuestion(27); err == nil {
		t.Error("question 27 was accepted, and there are twenty six")
	}
	// Question 24 is the first the act layer added, and the command has to reach
	// the new ones or they ship queryable by everything except the tool the
	// README tells a reader to use.
	if q, err := pickQuestion(24); err != nil || q.N != 24 {
		t.Errorf("question 24: %v, %v", q.N, err)
	}
	q, err := pickQuestion(20)
	if err != nil || q.N != 20 {
		t.Fatalf("question 20: %v, %v", q.N, err)
	}
}
