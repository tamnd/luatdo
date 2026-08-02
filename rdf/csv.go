package rdf

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

// Reading the dump back.
//
// The neo4j header conventions carry enough type information to do this
// generically: id:ID names the identifier column, :LABEL the labels, :START_ID
// and :END_ID the endpoints, :TYPE the relationship type, and a name:int or
// name:float or name:string[] suffix the datatype of a property. So this reads
// whatever the export wrote rather than a list of files kept in step with it,
// and a layer added to the projection reaches the RDF without anybody
// remembering to come here.

// ArrayDelimiter is the character the export separates array elements and
// multiple labels with. It has to agree with the one the export passes to
// neo4j-admin, and the reason it is not a semicolon is that Vietnamese legal
// text contains semicolons.
const ArrayDelimiter = "|"

// column is one CSV column with its neo4j meaning stripped off the name.
type column struct {
	name string // the property name, empty for the structural columns
	kind string // "", "int", "float", "string[]"
	role string // "id", "label", "start", "end", "type", or "" for a property
}

func parseHeader(header []string) []column {
	out := make([]column, len(header))
	for i, h := range header {
		switch h {
		case ":LABEL":
			out[i] = column{role: "label"}
			continue
		case ":START_ID":
			out[i] = column{role: "start"}
			continue
		case ":END_ID":
			out[i] = column{role: "end"}
			continue
		case ":TYPE":
			out[i] = column{role: "type"}
			continue
		}
		name, kind, found := strings.Cut(h, ":")
		c := column{name: name}
		if found {
			if kind == "ID" {
				c.role = "id"
			} else {
				c.kind = kind
			}
		}
		out[i] = c
	}
	return out
}

// table is one CSV file read back into rows keyed by what the header said each
// column was.
type table struct {
	name    string
	columns []column
	rows    [][]string
}

// readTable reads one CSV file. A file with a header and no rows is normal: a
// corpus with no temporal layer still gets an events.csv, and a reader that
// treated that as an error would make an empty layer indistinguishable from a
// broken one.
func readTable(path, name string) (*table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err == io.EOF {
		return &table{name: name}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	t := &table{name: name, columns: parseHeader(header)}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if len(row) != len(t.columns) {
			return nil, fmt.Errorf("%s: a row has %d fields and the header has %d", name, len(row), len(t.columns))
		}
		t.rows = append(t.rows, row)
	}
	return t, nil
}

// kindOf reports whether this table holds nodes or relationships, by what its
// header declared rather than by its file name.
func (t *table) kindOf() string {
	for _, c := range t.columns {
		switch c.role {
		case "id":
			return "nodes"
		case "start":
			return "relationships"
		}
	}
	return ""
}

func (t *table) find(role string) int {
	for i, c := range t.columns {
		if c.role == role {
			return i
		}
	}
	return -1
}

// values splits an array valued cell, and returns a single element for
// everything else.
func values(cell, kind string) []string {
	if kind == "string[]" {
		var out []string
		for _, v := range strings.Split(cell, ArrayDelimiter) {
			if v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	return []string{cell}
}
