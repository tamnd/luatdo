// Package rdf turns the Neo4j dump into RDF.
//
// It reads the CSV files rather than the store, and that is the whole design
// rather than a convenience. The interoperability debt this pays off was taken
// on deliberately when the project chose a property graph: a norm has a bearer,
// an action, an object, conditions, exceptions, a deadline, a sanction, a
// modality, a confidence and an evidence quote, which is one node with
// properties in a property graph and about a dozen triples in RDF, and paying
// that on every norm in the working model would have been a cost with no
// return, since nobody is federating with us today.
//
// So RDF is a projection of a projection. Generating it from the store instead
// would be a second working model, it would drift from the graph the moment
// either changed, and the first symptom would be somebody quoting a number from
// the RDF that the database does not agree with. Reading the dump means the RDF
// can hold nothing the graph does not, by construction rather than by
// discipline.
package rdf

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Summary is what one export produced.
type Summary struct {
	Files    int `json:"files"`
	Nodes    int `json:"nodes"`
	Edges    int `json:"edges"`
	Triples  int `json:"triples"`
	Reified  int `json:"reified"`
	Dangling int `json:"dangling"`
}

func (s Summary) String() string {
	return fmt.Sprintf("%d triples from %d nodes and %d edges across %d files, %d edges reified for their properties, %d dangling",
		s.Triples, s.Nodes, s.Edges, s.Files, s.Reified, s.Dangling)
}

// Export reads the Neo4j dump in dumpDir and writes graph.nt and
// vocabulary.ttl into outDir.
func Export(dumpDir, outDir string) (Summary, error) {
	var s Summary
	entries, err := os.ReadDir(dumpDir)
	if err != nil {
		return s, fmt.Errorf("no dump to project, run luatdo export neo4j first: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".csv") {
			names = append(names, e.Name())
		}
	}
	// Sorted, so two exports of one dump produce the same bytes. A file that
	// changes on every run cannot be diffed, and a projection nobody can diff
	// is a projection nobody checks.
	sort.Strings(names)

	tables := make([]*table, 0, len(names))
	for _, name := range names {
		t, err := readTable(filepath.Join(dumpDir, name), name)
		if err != nil {
			return s, err
		}
		tables = append(tables, t)
	}
	s.Files = len(tables)

	// The node identifiers are collected first, because an edge naming an
	// endpoint the dump does not contain has to be dropped rather than written.
	// It is the importer's contract restated: neo4j-admin refuses an entire
	// import over one such row, and RDF will happily accept it and leave a
	// subject nothing describes.
	nodes := map[string]bool{}
	for _, t := range tables {
		if t.kindOf() != "nodes" {
			continue
		}
		id := t.find("id")
		for _, row := range t.rows {
			nodes[row[id]] = true
		}
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return s, err
	}
	f, err := os.Create(filepath.Join(outDir, "graph.nt"))
	if err != nil {
		return s, err
	}
	defer func() { _ = f.Close() }()
	w := &writer{out: f, known: nodes}

	for _, t := range tables {
		switch t.kindOf() {
		case "nodes":
			n, err := w.nodes(t)
			if err != nil {
				return s, err
			}
			s.Nodes += n
		case "relationships":
			n, dangling, err := w.edges(t, nodes)
			if err != nil {
				return s, err
			}
			s.Edges += n
			s.Dangling += dangling
		}
	}
	s.Triples, s.Reified = w.triples, w.reified
	if err := f.Close(); err != nil {
		return s, err
	}
	if err := writeVocabulary(outDir); err != nil {
		return s, err
	}
	return s, nil
}

// writer emits N-Triples.
//
// N-Triples rather than Turtle because it is the format every tool reads, it
// streams, and a line of it is one fact, which means a corpus sized file can be
// grepped and split without a parser. Turtle would be a third the size and
// would need the whole file in memory to write the prefixes honestly.
type writer struct {
	out *os.File
	// known is every identifier the dump wrote, which is what a reference has
	// to land in before it is written as a link.
	known   map[string]bool
	triples int
	reified int
}

func (w *writer) write(subject, predicate, object string) error {
	w.triples++
	_, err := fmt.Fprintf(w.out, "%s %s %s .\n", subject, predicate, object)
	return err
}

func (w *writer) nodes(t *table) (int, error) {
	idCol, labelCol := t.find("id"), t.find("label")
	if idCol < 0 {
		return 0, nil
	}
	count := 0
	for _, row := range t.rows {
		if row[idCol] == "" {
			continue
		}
		count++
		subject := iri(NSInstance + row[idCol])
		if labelCol >= 0 {
			for _, label := range values(row[labelCol], "string[]") {
				if err := w.write(subject, iri(nsRDF+"type"), iri(NSTerm+label)); err != nil {
					return count, err
				}
				for _, extra := range alignedTypes[label] {
					if err := w.write(subject, iri(nsRDF+"type"), iri(extra)); err != nil {
						return count, err
					}
				}
			}
		}
		for i, c := range t.columns {
			if c.role != "" || c.name == "" {
				continue
			}
			if err := w.property(subject, c, row[i]); err != nil {
				return count, err
			}
		}
	}
	return count, nil
}

// property writes one column of one row, or nothing.
//
// An empty cell writes nothing at all. A property graph and RDF disagree about
// what an absent value is: the CSV has a column for every property any node of
// that label carries, so most rows have empty cells, and turning those into
// triples with an empty string object would say that this document's English
// title is the empty string rather than that nobody has translated it.
func (w *writer) property(subject string, c column, cell string) error {
	if cell == "" {
		return nil
	}
	p := iri(predicate(c.name))
	for _, v := range values(cell, c.kind) {
		if v == "" {
			continue
		}
		object := literal(v, c, c.name)
		switch {
		case asIRI[c.name] != "":
			object = iri(v)
		case references[c.name] != "" && w.known[v]:
			// A reference to something the dump does not hold stays a literal.
			// The information is worth keeping, and inventing an IRI for a node
			// nobody wrote would give a consumer a link that goes nowhere.
			object = iri(NSInstance + v)
		}
		if err := w.write(subject, p, object); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) edges(t *table, nodes map[string]bool) (int, int, error) {
	start, end, typ := t.find("start"), t.find("end"), t.find("type")
	if start < 0 || end < 0 {
		return 0, 0, nil
	}
	count, dangling := 0, 0
	for _, row := range t.rows {
		if typ < 0 || row[typ] == "" {
			continue
		}
		if !nodes[row[start]] || !nodes[row[end]] {
			dangling++
			continue
		}
		count++
		s, p, o := iri(NSInstance+row[start]), edgePredicate(row[typ]), iri(NSInstance+row[end])
		if err := w.write(s, iri(p), o); err != nil {
			return count, dangling, err
		}
		// An aligned edge keeps our predicate as well, so a consumer working
		// in our terms sees the same graph a consumer working in SKOS sees.
		if _, ok := alignedEdges[row[typ]]; ok {
			if err := w.write(s, iri(edgeTerm(row[typ])), o); err != nil {
				return count, dangling, err
			}
		}
		if err := w.reify(t, row, s, p, o); err != nil {
			return count, dangling, err
		}
	}
	return count, dangling, nil
}

// reify writes an edge's properties, when it has any.
//
// A triple has three slots and an edge here can carry six, so the properties go
// on an rdf:Statement that points back at the triple. This is plain RDF 1.1
// reification and not RDF-star, because the point of the export is that
// everything reads it, and RDF-star support was still uneven the last time
// anybody checked.
//
// The plain triple is written either way. Reification alone would mean that
// asking which documents cite which needs a consumer to understand
// reification, which is a bad trade for the common query.
func (w *writer) reify(t *table, row []string, s, p, o string) error {
	has := false
	for i, c := range t.columns {
		if c.role == "" && c.name != "" && row[i] != "" {
			has = true
			break
		}
	}
	if !has {
		return nil
	}
	w.reified++
	// The name is a hash of the triple, so the same edge in two exports gets
	// the same statement IRI and a blank node never has to be matched up.
	sum := sha256.Sum256([]byte(s + "\x00" + p + "\x00" + o))
	stmt := iri(NSStatement + hex.EncodeToString(sum[:12]))
	if err := w.write(stmt, iri(nsRDF+"type"), iri(nsRDF+"Statement")); err != nil {
		return err
	}
	for _, pair := range [][2]string{
		{nsRDF + "subject", s}, {nsRDF + "predicate", iri(p)}, {nsRDF + "object", o},
	} {
		if err := w.write(stmt, iri(pair[0]), pair[1]); err != nil {
			return err
		}
	}
	for i, c := range t.columns {
		if c.role != "" || c.name == "" {
			continue
		}
		if err := w.property(stmt, c, row[i]); err != nil {
			return err
		}
	}
	return nil
}

// edgeTerm is the predicate an aligned relationship type keeps in our own
// namespace.
func edgeTerm(relType string) string { return NSTerm + camel(strings.ToLower(relType)) }

// iri wraps an IRI in the angle brackets N-Triples wants, escaping the
// characters the grammar does not allow inside them.
func iri(s string) string {
	var b strings.Builder
	b.WriteByte('<')
	for _, r := range s {
		switch {
		case r <= 0x20, r == '<', r == '>', r == '"', r == '{', r == '}',
			r == '|', r == '^', r == '`', r == '\\':
			fmt.Fprintf(&b, "%%%02X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('>')
	return b.String()
}

var (
	isoDate     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	isoDateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T`)
)

// literal writes the object of a property triple, with a datatype or a
// language tag when one is known.
//
// The datatype rules are deliberately narrow. A column declared int or float in
// the neo4j header is typed from that declaration, because the export chose it.
// A date is typed only when the value is exactly the shape a date has: the
// corpus contains effective dates recorded as a year alone, and calling one of
// those an xsd:date produces a file that fails validation in a consumer's
// toolchain and tells them our data is worse than it is.
func literal(v string, c column, name string) string {
	switch c.kind {
	case "int":
		return quote(v) + "^^" + iri(nsXSD+"integer")
	case "float":
		return quote(v) + "^^" + iri(nsXSD+"decimal")
	}
	switch {
	case isoDateTime.MatchString(v):
		if _, err := time.Parse(time.RFC3339, v); err == nil {
			return quote(v) + "^^" + iri(nsXSD+"dateTime")
		}
	case isoDate.MatchString(v) && dateish(name):
		if _, err := time.Parse("2006-01-02", v); err == nil {
			return quote(v) + "^^" + iri(nsXSD+"date")
		}
	}
	if tag := language(name); tag != "" {
		return quote(v) + "@" + tag
	}
	return quote(v)
}

// dateish reports whether a column is one that holds dates. A value that merely
// looks like a date in a column that holds something else, say an identifier,
// should stay a string.
func dateish(name string) bool {
	return strings.HasSuffix(name, "_date") || strings.HasSuffix(name, "_from") ||
		strings.HasSuffix(name, "_at") || name == "date" || name == "effective_from"
}

// quote escapes a literal for N-Triples. The text is Vietnamese legal prose and
// it contains newlines, quotation marks and backslashes.
func quote(v string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range v {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
