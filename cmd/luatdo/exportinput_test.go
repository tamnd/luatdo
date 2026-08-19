package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/conflict"
	"github.com/tamnd/luatdo/event"
	"github.com/tamnd/luatdo/graph"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/link"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/relation"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/subject"
	"github.com/tamnd/luatdo/temporal"
	"github.com/tamnd/luatdo/term"
)

// A layer left out of the projection input fails silently. Nothing errors, no
// gate complains, and the export writes that layer's CSV holding its header,
// which reads exactly like a store where the pass was never run. It has
// happened three times: the relation layer, the temporal layer, and then the
// act layer, which shipped 362 acts and 100 chains to a file with nothing under
// the header while every test in the graph package passed.
//
// So this seeds one record in every layer and asks for all of them back. The
// field list is taken from graph.Input by reflection rather than written out,
// because the failure is always an omission and a hand written list omits the
// same field the loader did.
func TestEveryLayerTheStoreHoldsReachesTheProjection(t *testing.T) {
	data := t.TempDir()
	s, err := openStore(data)
	if err != nil {
		t.Fatal(err)
	}
	seedEveryLayer(t, s)

	in, err := loadExportInput(s)
	if err != nil {
		t.Fatalf("load export input: %v", err)
	}

	// One expectation per field, each asking whether the seeded record arrived
	// rather than whether the field is non nil. temporal.ReadLayer hands back an
	// empty layer for a store with no temporal pass, so a nil check would pass
	// on a loader that read nothing.
	arrived := map[string]func(graph.Input) bool{
		"Docs":        func(i graph.Input) bool { return len(i.Docs) > 0 },
		"Links":       func(i graph.Input) bool { return len(i.Links) > 0 },
		"Registry":    func(i graph.Input) bool { return i.Registry != nil && len(i.Registry.Classes) > 0 },
		"Definitions": func(i graph.Input) bool { return len(i.Definitions) > 0 },
		"Mentions":    func(i graph.Input) bool { return len(i.Mentions) > 0 },
		"Statements":  func(i graph.Input) bool { return len(i.Statements) > 0 },
		"Vocabulary":  func(i graph.Input) bool { return i.Vocabulary != nil && len(i.Vocabulary.Subjects) > 0 },
		"Subjects":    func(i graph.Input) bool { return len(i.Subjects) > 0 },
		"Layer":       func(i graph.Input) bool { return i.Layer != nil && len(i.Layer.TermUses) > 0 },
		"Relations":   func(i graph.Input) bool { return len(i.Relations) > 0 },
		"Temporal":    func(i graph.Input) bool { return i.Temporal != nil && len(i.Temporal.Versions) > 0 },
		"Conflicts":   func(i graph.Input) bool { return len(i.Conflicts) > 0 },
		"Acts":        func(i graph.Input) bool { return len(i.Acts) > 0 },
		"Chains":      func(i graph.Input) bool { return len(i.Chains) > 0 },
		"NormActs":    func(i graph.Input) bool { return len(i.NormActs) > 0 },
	}

	typ := reflect.TypeOf(graph.Input{})
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		check, ok := arrived[name]
		if !ok {
			t.Errorf("graph.Input has a field %s with no expectation here, so nothing says whether loadExportInput fills it", name)
			continue
		}
		if !check(in) {
			t.Errorf("%s was seeded in the store and did not reach the projection input", name)
		}
	}
}

// seedEveryLayer writes one record into each layer of a store.
//
// The records are the smallest thing each reader will hand back rather than
// realistic ones, because what is under test is the wiring. Every other layer's
// own package states what its records mean.
func seedEveryLayer(t *testing.T, s *store.Store) {
	t.Helper()
	const docID = "vn:law:2019:45-2019-qh14"
	const provisionID = docID + ":article-1"

	writeJSON := func(path string, v any) {
		t.Helper()
		if err := store.WriteJSON(path, v); err != nil {
			t.Fatal(err)
		}
	}
	appendJSONL := func(path string, rows any) {
		t.Helper()
		v := reflect.ValueOf(rows)
		out := make([]any, v.Len())
		for i := range v.Len() {
			out[i] = v.Index(i).Interface()
		}
		if err := store.AppendJSONL(path, out); err != nil {
			t.Fatal(err)
		}
	}

	writeJSON(filepath.Join(s.Docs(), "doc.json"), law.Document{
		ID:         docID,
		Provisions: []law.Provision{{ID: provisionID, Kind: "article", Number: "1"}},
	})
	writeJSON(filepath.Join(s.Cite(), "doc.json"), []cite.Link{
		{FromDoc: docID, FromProvision: provisionID, ToDoc: docID, ToNumber: "45/2019/QH14", Kind: "cites"},
	})
	writeJSON(filepath.Join(s.Terms(), "doc.json"), []term.Definition{
		{TermID: "vn:term:lao-dong", Term: "lao động", DocID: docID, ProvisionID: provisionID, Text: "lao động là"},
	})
	writeJSON(filepath.Join(s.Ontology(), "v1.json"), ontology.Seed())
	writeJSON(filepath.Join(s.Links(), "doc.json"), []link.Resolution{
		{ProvisionID: provisionID, DocID: docID, Text: "người lao động", TargetKind: "class"},
	})
	appendJSONL(filepath.Join(s.Subject(), subject.AssignmentsFile), []subject.Record{
		{DocID: docID, DocType: "law"},
	})
	writeJSON(filepath.Join(s.Trusted(), "statements.json"), []norm.Record{
		{ID: "vn:norm:1", DocID: docID, ProvisionID: provisionID},
	})
	writeJSON(filepath.Join(s.Concepts(), concept.LayerFile), concept.Layer{
		TermUses: []concept.TermUse{{ID: "vn:termuse:1", DocID: docID, LabelVI: "lao động"}},
	})
	appendJSONL(filepath.Join(s.Relation(), relation.EdgesFile), []relation.Edge{
		{FromID: "a", ToID: "b", Type: relation.Broader, Status: relation.StatusCanonical},
	})
	appendJSONL(filepath.Join(s.Temporal(), temporal.VersionsFile), []temporal.Version{
		{ID: "vn:version:1", ComponentID: provisionID, DocID: docID, Kind: "article"},
	})
	writeJSON(filepath.Join(s.Conflict(), conflict.ReportFile), conflict.Report{
		Findings: []conflict.Finding{{
			Rule: "duty-vs-prohibition",
			A:    &conflict.Form{DocID: docID, ProvisionID: provisionID},
			B:    &conflict.Form{DocID: docID, ProvisionID: provisionID},
		}},
	})
	appendJSONL(filepath.Join(s.Event(), event.EventsFile), []event.Event{
		{ID: "vn:event:1", Class: "APPOINT", LabelVI: "bổ nhiệm", Status: "provisional"},
	})
	appendJSONL(filepath.Join(s.Event(), event.ChainsFile), []event.Chain{
		{FromID: "vn:event:1", ToID: "vn:event:1", Type: "PRECEDES", Status: "provisional"},
	})
	appendJSONL(filepath.Join(s.Event(), event.LinksFile), []event.Link{
		{StatementID: "vn:norm:1", ProvisionID: provisionID, DocID: docID, EventID: "vn:event:1", Kind: "action"},
	})
}
