package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The RDF export is the one command in the tool that reads a file the tool
// wrote rather than the store, so the failure it has to handle well is being
// run before the file exists. A person who has just cloned the repository and
// typed "luatdo export rdf" needs to be told which command to run first, not
// handed a path that is not there.
func TestExportRDFSaysWhichCommandToRunFirstWhenThereIsNoDump(t *testing.T) {
	data := t.TempDir()
	err := cmdExport([]string{"--data", data, "rdf"})
	if err == nil {
		t.Fatal("exporting RDF from a store with no dump reported success")
	}
	if !strings.Contains(err.Error(), "luatdo export neo4j") {
		t.Errorf("the error does not name the command that writes the dump: %v", err)
	}
}

func TestExportRDFProjectsTheDumpTheNeo4jExportWrote(t *testing.T) {
	data := t.TempDir()
	dump := filepath.Join(data, "export", "neo4j")
	if err := os.MkdirAll(dump, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two files, one of each kind, because what this test is checking is the
	// wiring rather than the projection. The rdf package's own tests state what
	// a header becomes, and the graph package's state that the headers here are
	// the ones the exporter writes.
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dump, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("documents.csv", "id:ID,title,:LABEL\nvn:law:2019:45,Bộ luật Lao động,Document\n")
	write("contains.csv", ":START_ID,:END_ID,:TYPE\nvn:law:2019:45,vn:law:2019:45,CONTAINS\n")

	if err := cmdExport([]string{"--data", data, "rdf"}); err != nil {
		t.Fatalf("export rdf: %v", err)
	}
	out := filepath.Join(data, "export", "rdf")
	b, err := os.ReadFile(filepath.Join(out, "graph.nt"))
	if err != nil {
		t.Fatalf("no triples were written: %v", err)
	}
	if !strings.Contains(string(b), "<https://luatdo.dev/ns#Document>") {
		t.Errorf("the document did not reach the projection:\n%s", b)
	}
	// The alignment ships beside the data rather than inside it, and a consumer
	// who never gets the second file never gets the choice.
	if _, err := os.Stat(filepath.Join(out, "vocabulary.ttl")); err != nil {
		t.Errorf("the vocabulary was not written beside the data: %v", err)
	}
}

// LegalRuleML is the one export that says something about how good it is on
// every line, so the campaign is not optional and neither is the gate.
func TestLegalRuleMLRefusesACorpusWideExport(t *testing.T) {
	err := cmdExport([]string{"--data", t.TempDir(), "legalruleml"})
	if err == nil {
		t.Fatal("a corpus wide LegalRuleML export was accepted")
	}
	if !strings.Contains(err.Error(), "--campaign") {
		t.Errorf("the error does not say what is missing: %v", err)
	}
}

func TestLegalRuleMLHasNoWayToSkipTheGates(t *testing.T) {
	// The flag exists, because it is the same flag set the neo4j export uses.
	// What matters is that passing it changes nothing here.
	for _, args := range [][]string{
		{"--data", t.TempDir(), "--campaign", "labour-2025", "legalruleml"},
		{"--data", t.TempDir(), "--campaign", "labour-2025", "--force", "legalruleml"},
	} {
		if err := cmdExport(args); err == nil {
			t.Errorf("%v produced a rule base from an empty store", args)
		}
	}
}

func TestExportRefusesAFormatItDoesNotWrite(t *testing.T) {
	err := cmdExport([]string{"--data", t.TempDir(), "jsonld"})
	if err == nil {
		t.Fatal("an unknown export format was accepted")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("the error does not say what the command accepts: %v", err)
	}
}
