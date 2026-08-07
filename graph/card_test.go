package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The card is the only part of this that a person sees before they download
// anything, and the YAML at the top of it is the only part a machine reads. A
// config naming a table that is not there does not degrade, it stops the whole
// page from loading, so what the front matter says and what was written have to
// be the same list by construction.

func writeCard(t *testing.T, tables []ParquetTable) string {
	t.Helper()
	dir := t.TempDir()
	in := CardInput{
		Repo:          "open-index/luatdo-graph",
		Archive:       "luatdo-graph-2026.08.1.tar.gz",
		ArchiveBytes:  576243905,
		UnpackedBytes: 3_650_000_000,
	}
	if err := WriteCard(dir, tables, in); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestCardNamesOneConfigPerTable(t *testing.T) {
	tables := []ParquetTable{
		{Name: "documents", Kind: NodeTable, Rows: 128_000, Files: []string{"train-00000-of-00001.parquet"}, Bytes: 40 << 20},
		{Name: "cites", Kind: RelationshipTable, Rows: 2_000_000, Files: []string{"train-00000-of-00002.parquet", "train-00001-of-00002.parquet"}, Bytes: 90 << 20},
	}
	card := writeCard(t, tables)
	front, _, ok := strings.Cut(strings.TrimPrefix(card, "---\n"), "\n---\n")
	if !ok {
		t.Fatal("the card has no YAML front matter, so the hub shows it as plain text and no viewer appears")
	}
	for _, table := range tables {
		if !strings.Contains(front, "config_name: "+table.Name) {
			t.Errorf("the front matter does not name config %s", table.Name)
		}
		// The glob has to match what the shard writer produced. This is the one
		// pairing that cannot be checked by reading either file on its own.
		want := "path: data/" + table.Name + "/train-*.parquet"
		if !strings.Contains(front, want) {
			t.Errorf("the front matter is missing %q", want)
		}
		for _, name := range table.Files {
			if !strings.HasPrefix(name, "train-") || !strings.HasSuffix(name, ".parquet") {
				t.Errorf("shard %s does not match the glob the card declares", name)
			}
		}
	}
	if strings.Contains(front, "\t") {
		t.Error("the front matter holds a tab, which is not valid YAML")
	}
}

func TestCardCountsNodesAndRelationshipsApart(t *testing.T) {
	card := writeCard(t, []ParquetTable{
		{Name: "documents", Kind: NodeTable, Rows: 128_000, Labels: []string{"Document"}},
		{Name: "components", Kind: NodeTable, Rows: 4_000_000, Labels: []string{"Component", "Provision"}},
		{Name: "cites", Kind: RelationshipTable, Rows: 2_000_000},
	})
	// The labels are the union across the node tables, and the count is that
	// union rather than what the schema defines.
	if !strings.Contains(card, "| Node labels | 3 |") {
		t.Error("the card does not count the labels it found")
	}
	if !strings.Contains(card, "Component, Document, Provision") {
		t.Error("the card does not list the labels in sorted order")
	}
	// Folding these into one total would be the same mistake as reporting a
	// single headline number for the graph, which hid an empty layer for three
	// milestones.
	for _, want := range []string{"| Nodes | 4,128,000 |", "| Relationships | 2,000,000 |", "| Node tables | 2 |", "| Relationship tables | 1 |"} {
		if !strings.Contains(card, want) {
			t.Errorf("the card is missing %q", want)
		}
	}
	nodes := strings.Index(card, "| `documents` | 128,000 |")
	edges := strings.Index(card, "| `cites` | 2,000,000 |")
	if nodes < 0 || edges < 0 {
		t.Fatal("the table listing does not hold both tables")
	}
	if nodes > edges {
		t.Error("a node table was listed under relationships")
	}
}

func TestCardReportsSizesSomebodyCanDecideOn(t *testing.T) {
	card := writeCard(t, []ParquetTable{{Name: "documents", Kind: NodeTable, Rows: 1, Bytes: 2 << 30, Files: []string{"train-00000-of-00001.parquet"}}})
	for _, want := range []string{"2.0GB across 1 files", "550MB gzipped", "3.4GB unpacked"} {
		if !strings.Contains(card, want) {
			t.Errorf("the card is missing %q", want)
		}
	}
}

func TestCardSubstitutesTheRepositoryEverywhereItIsNamed(t *testing.T) {
	card := writeCard(t, []ParquetTable{{Name: "documents", Kind: NodeTable}})
	// The card holds a download command and two library calls, and every one of
	// them names the repository. A hard coded name that drifts sends people to a
	// repository that does not exist.
	if n := strings.Count(card, "open-index/luatdo-graph"); n < 3 {
		t.Errorf("the repository is named %d times, want it in the curl, the pandas URL and the datasets call", n)
	}
	if strings.Contains(card, "{{") {
		t.Error("the card holds an unexpanded template action")
	}
}

func TestHumanBytesRoundsToWhatTheDecisionNeeds(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{1, "1KB"},
		{1 << 20, "1MB"},
		{576243905, "550MB"},
		{3 << 30, "3.0GB"},
	} {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d) is %s, want %s", c.n, got, c.want)
		}
	}
}

func TestCommasGroupTheWayAPersonReads(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{{0, "0"}, {999, "999"}, {1000, "1,000"}, {8175346, "8,175,346"}} {
		if got := commas(c.n); got != c.want {
			t.Errorf("commas(%d) is %s, want %s", c.n, got, c.want)
		}
	}
}
