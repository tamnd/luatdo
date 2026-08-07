package graph

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// The dataset card.
//
// A hub renders the README at the root of a dataset repository, and the block
// of YAML at the top of it is not decoration: the list of configs in there is
// what makes a viewer appear at all, and a config naming a table that is not
// there makes the whole page fail to load rather than skipping it. So the card
// is generated from the tables that were actually written, for the same reason
// import.sh is generated from the files that were actually written. A card
// maintained by hand is a list of table names that goes stale the first time
// somebody adds a node type, and it goes stale silently.
//
// The prose lives in dataset_card.md rather than in a Go string, because it is
// prose, and it is read by people who are deciding whether to download half a
// gigabyte.

//go:embed dataset_card.md
var cardTemplate string

// CardInput is what the card needs beyond the tables themselves.
type CardInput struct {
	Repo    string
	Archive string
	// ArchiveBytes and UnpackedBytes describe the Neo4j import set, which this
	// package does not produce and cannot measure from here.
	ArchiveBytes  int64
	UnpackedBytes int64
}

// cardData is the shape the template sees.
type cardData struct {
	CardInput
	Tables             []ParquetTable
	NodeList           []cardRow
	RelationshipList   []cardRow
	Nodes              string
	Relationships      string
	NodeTables         int
	RelationshipTables int
	Labels             string
	LabelCount         int
	Files              int
	ParquetSize        string
	ArchiveSize        string
	UnpackedSize       string
}

type cardRow struct {
	Name   string
	Rows   string
	Size   string
	Labels string
}

// WriteCard renders the dataset card into the root of the output directory.
func WriteCard(out string, tables []ParquetTable, in CardInput) error {
	data := cardData{CardInput: in, Tables: tables}
	var nodes, relationships, bytes int64
	labels := map[string]bool{}
	for _, t := range tables {
		row := cardRow{Name: t.Name, Rows: commas(t.Rows), Size: humanBytes(t.Bytes), Labels: strings.Join(t.Labels, ", ")}
		for _, label := range t.Labels {
			labels[label] = true
		}
		if t.Kind == RelationshipTable {
			relationships += t.Rows
			data.RelationshipTables++
			data.RelationshipList = append(data.RelationshipList, row)
		} else {
			nodes += t.Rows
			data.NodeTables++
			data.NodeList = append(data.NodeList, row)
		}
		bytes += t.Bytes
		data.Files += len(t.Files)
	}
	names := make([]string, 0, len(labels))
	for label := range labels {
		names = append(names, label)
	}
	sort.Strings(names)
	data.Labels = strings.Join(names, ", ")
	data.LabelCount = len(names)
	data.Nodes = commas(nodes)
	data.Relationships = commas(relationships)
	data.ParquetSize = humanBytes(bytes)
	data.ArchiveSize = humanBytes(in.ArchiveBytes)
	data.UnpackedSize = humanBytes(in.UnpackedBytes)

	t, err := template.New("card").Parse(cardTemplate)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(out, "README.md"))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := t.Execute(f, data); err != nil {
		return err
	}
	return f.Close()
}

// commas groups a count the way a person reads one.
func commas(n int64) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

// humanBytes rounds a size to something worth reading.
//
// Sizes on a dataset card are read to decide whether to start a download, so a
// tenth of a gigabyte is as much precision as the decision can use, and small
// tables round to whole megabytes rather than showing three decimal places of a
// gigabyte.
func humanBytes(n int64) string {
	switch {
	case n <= 0:
		return "0"
	case n < 1<<20:
		return fmt.Sprintf("%dKB", (n+(1<<10)-1)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%dMB", (n+(1<<20)-1)/(1<<20))
	default:
		return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	}
}
