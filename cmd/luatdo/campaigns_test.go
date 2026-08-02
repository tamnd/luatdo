package main

import (
	"path/filepath"
	"testing"

	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/store"
)

func TestCampaignRecordsCountTheStatementsThatDidNotSurvive(t *testing.T) {
	// The report's acceptance rate is the point of the report, and it can only
	// be computed against everything the pass proposed. Reading the trusted
	// store alone reports that every statement was accepted, every time.
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	job := &extract.NormJob{DocID: "d1", Records: []norm.Record{
		{ID: "r1", DocID: "d1", Status: norm.StatusVerified},
		{ID: "r2", DocID: "d1", Status: norm.StatusRejected},
		{ID: "r3", DocID: "d1", Status: norm.StatusInvalid},
	}}
	if err := store.WriteJSON(filepath.Join(s.Norms(), "p1.json"), job); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	trusted := []norm.Record{{ID: "r1", DocID: "d1", Status: norm.StatusVerified}}
	if err := store.WriteJSON(filepath.Join(s.Trusted(), "statements.json"), trusted); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	got, err := campaignRecords(s)
	if err != nil {
		t.Fatalf("campaignRecords: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("records = %+v, two of the three were thrown away and the report has to say so", got)
	}
}

func TestCampaignRecordsPreferTheRecordTheBuildSettledOn(t *testing.T) {
	// A statement a person kept is rejected in the job artifact, which was
	// written before the review, and approved in the trusted store.
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	job := &extract.NormJob{DocID: "d1", Records: []norm.Record{
		{ID: "r1", DocID: "d1", Status: norm.StatusRejected},
	}}
	if err := store.WriteJSON(filepath.Join(s.Norms(), "p1.json"), job); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	kept := []norm.Record{
		{ID: "r1", DocID: "d1", Status: norm.StatusApproved},
		{ID: "r9", DocID: "d1", Status: norm.StatusVerified},
	}
	if err := store.WriteJSON(filepath.Join(s.Trusted(), "statements.json"), kept); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	got, err := campaignRecords(s)
	if err != nil {
		t.Fatalf("campaignRecords: %v", err)
	}
	byID := map[string]string{}
	for _, rec := range got {
		byID[rec.ID] = rec.Status
	}
	if byID["r1"] != norm.StatusApproved {
		t.Errorf("r1 = %q, the job artifact is the older document and does not overrule the review", byID["r1"])
	}
	if byID["r9"] != norm.StatusVerified {
		t.Errorf("records = %+v, a trusted record whose job file is gone is still in the graph", got)
	}
}
