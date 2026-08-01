// Package graph projects the canonical store into Neo4j.
//
// Neo4j is a projection, never the source of truth. A full export writes the
// CSV files and scripts for neo4j-admin database import, which is the fast
// path for a fresh database. An incremental export merges over Bolt and is
// idempotent, so it can run after every pipeline pass.
package graph

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/tamnd/luatdo/cite"
	"github.com/tamnd/luatdo/law"
)

// Schema is applied with cypher-shell after the imported database starts.
const Schema = `CREATE CONSTRAINT doc_id IF NOT EXISTS FOR (d:Document) REQUIRE d.id IS UNIQUE;
CREATE CONSTRAINT prov_id IF NOT EXISTS FOR (p:Provision) REQUIRE p.id IS UNIQUE;
CREATE FULLTEXT INDEX provision_text IF NOT EXISTS FOR (p:Provision) ON EACH [p.text, p.heading];
CREATE FULLTEXT INDEX document_title IF NOT EXISTS FOR (d:Document) ON EACH [d.title, d.title_en];
CREATE INDEX doc_effective IF NOT EXISTS FOR (d:Document) ON (d.effective_from);
`

// Export writes the CSV projection of docs and links into dir.
func Export(dir string, docs []*law.Document, links []cite.Link) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "documents.csv"),
		[]string{"id:ID", "official_number", "title", "title_en", "doc_type", "effective_from", "source", "source_url", "status", ":LABEL"},
		func(w *csv.Writer) error {
			for _, d := range docs {
				if err := w.Write([]string{d.ID, d.OfficialNumber, d.Title, d.TitleEN, d.DocType, d.EffectiveFrom, d.Source, d.SourceURL, d.Status, "Document"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "provisions.csv"),
		[]string{"id:ID", "kind", "number", "heading", "text", "position:int", ":LABEL"},
		func(w *csv.Writer) error {
			for _, d := range docs {
				for i := range d.Provisions {
					p := &d.Provisions[i]
					if err := w.Write([]string{p.ID, p.Kind, p.Number, p.Heading, p.Text, strconv.Itoa(p.Position), "Provision"}); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "contains.csv"),
		[]string{":START_ID", ":END_ID", ":TYPE"},
		func(w *csv.Writer) error {
			for _, d := range docs {
				for i := range d.Provisions {
					p := &d.Provisions[i]
					parent := p.ParentID
					if parent == "" {
						parent = d.ID
					}
					if err := w.Write([]string{parent, p.ID, "CONTAINS"}); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(dir, "cites.csv"),
		[]string{":START_ID", ":END_ID", "method", "snippet", ":TYPE"},
		func(w *csv.Writer) error {
			for _, l := range links {
				if l.ToDoc == "" {
					continue
				}
				relType := "CITES"
				if l.Kind == "amends" {
					relType = "AMENDS"
				}
				from := l.FromProvision
				if from == "" {
					from = l.FromDoc
				}
				if err := w.Write([]string{from, l.ToDoc, l.Method, l.Snippet, relType}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.cypher"), []byte(Schema), 0o644); err != nil {
		return err
	}
	return writeImportScripts(dir)
}

func writeCSV(path string, header []string, body func(*csv.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	if err := body(w); err != nil {
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return f.Close()
}

// writeImportScripts writes the neo4j-admin invocation for both shells, so
// the export directory works on the Linux servers and the Windows machine.
func writeImportScripts(dir string) error {
	args := `database import full luatdo --overwrite-destination ` +
		`--nodes=documents.csv --nodes=provisions.csv ` +
		`--relationships=contains.csv --relationships=cites.csv`
	sh := "#!/bin/sh\n# Run from this directory with the database stopped.\nneo4j-admin " + args + "\n"
	cmd := "@echo off\r\nrem Run from this directory with the database stopped.\r\nneo4j-admin " + args + "\r\n"
	if err := os.WriteFile(filepath.Join(dir, "import.sh"), []byte(sh), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "import.cmd"), []byte(cmd), 0o644)
}

// Summary says what an export contained.
type Summary struct {
	Documents  int `json:"documents"`
	Provisions int `json:"provisions"`
	Contains   int `json:"contains"`
	Cites      int `json:"cites"`
	Unresolved int `json:"unresolved"`
}

// Summarize counts the projection without writing it.
func Summarize(docs []*law.Document, links []cite.Link) Summary {
	s := Summary{Documents: len(docs)}
	for _, d := range docs {
		s.Provisions += len(d.Provisions)
		s.Contains += len(d.Provisions)
	}
	for _, l := range links {
		if l.ToDoc == "" {
			s.Unresolved++
		} else {
			s.Cites++
		}
	}
	return s
}

func (s Summary) String() string {
	return fmt.Sprintf("documents %d, provisions %d, contains %d, cites %d, unresolved %d",
		s.Documents, s.Provisions, s.Contains, s.Cites, s.Unresolved)
}
