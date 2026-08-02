package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/rdf"
)

// The RDF export reads the dump this package writes, so the contract between
// them is only checked by running one over the other. The rdf package's own
// tests state what it does with a header; this one states that the headers it
// is given are the ones this package produces.
//
// It lives here rather than there because the fixture does. A test in the rdf
// package would have to build a property graph by hand to have something to
// project, and then it would be checking its own hand written CSV rather than
// the exporter's.
func TestTheRDFProjectionCoversEveryNodeAndEdgeTheDumpHolds(t *testing.T) {
	dump, out := t.TempDir(), t.TempDir()
	in := competencyFixture()
	if err := Export(dump, in); err != nil {
		t.Fatalf("Export: %v", err)
	}
	s, err := rdf.Export(dump, out)
	if err != nil {
		t.Fatalf("rdf.Export: %v", err)
	}

	// Every node in the dump has to reach the RDF. The count is taken off the
	// CSV files rather than off Summary, because the failure worth catching is
	// a layer the RDF stops projecting, and a hand written list of which
	// counters to add up would be a second place to forget it.
	nodes := 0
	entries, err := os.ReadDir(dump)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".csv") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dump, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if len(lines) == 0 || !strings.HasPrefix(lines[0], "id:ID") {
			continue
		}
		nodes += len(lines) - 1
	}
	if s.Nodes != nodes {
		t.Errorf("RDF holds %d nodes and the dump holds %d", s.Nodes, nodes)
	}
	if s.Dangling != 0 {
		t.Errorf("%d edges point at a node the dump does not contain", s.Dangling)
	}
	if s.Edges == 0 || s.Triples == 0 {
		t.Fatalf("summary = %+v, the projection is empty", s)
	}

	b, err := os.ReadFile(filepath.Join(out, "graph.nt"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	// One assertion per layer, because the failure this catches is a layer that
	// silently stops being projected, and a total count would not notice one
	// layer moving its triples to another.
	for _, want := range []string{
		"<https://luatdo.dev/ns#Document>",
		"<https://luatdo.dev/ns#Component>",
		"<https://luatdo.dev/ns#Norm>",
		"<https://luatdo.dev/ns#Event>",
		"<https://luatdo.dev/ns#TemporalVersion>",
		"<https://luatdo.dev/ns#Concept>",
		"<https://luatdo.dev/ns#TermUse>",
		"<http://www.w3.org/2004/02/skos/core#Concept>",
		"<https://luatdo.dev/ns#hasNorm>",
		"<https://luatdo.dev/ns#producesVersion>",
		"<https://luatdo.dev/ns#conceptBroader>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the projection has no %s", want)
		}
	}
	// A norm is a node with properties rather than a triple, which is the
	// reason the property graph was chosen and the thing an RDF consumer most
	// needs to be told.
	if !strings.Contains(text, "<https://luatdo.dev/ns#modality>") {
		t.Error("norms reached the projection without their modality")
	}
}
