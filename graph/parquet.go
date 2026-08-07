package graph

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
)

// The Parquet projection.
//
// The neo4j export is the graph in the shape neo4j-admin wants, and that shape
// is unreadable anywhere else: the column names carry import directives, the
// files are gigabytes of uncompressed CSV, and a dataset hub shows them as a
// download link and nothing more. Parquet is the shape everything else wants.
// It is typed, it is a third of the size, pandas and duckdb and polars read it
// without being told anything, and a hub renders it as a table somebody can
// page through before deciding whether to download half a gigabyte.
//
// This converts one into the other rather than projecting the graph a second
// time. A second projection would be a second list of node types, a second set
// of column names, and a second place to forget a layer, which is a mistake this
// codebase has already made three times. The CSV files are the projection. Their
// header rows say what every column is, because neo4j-admin requires them to,
// and that is enough to build a Parquet schema from. A node type added to the
// export appears here without anybody editing this file.

// The suffixes neo4j-admin puts in a CSV header.
//
// The three positional ones name a column rather than type it: :ID is the node
// key, :START_ID and :END_ID are the endpoints of an edge. They are renamed
// rather than dropped, because outside of an import they are ordinary columns
// and "id" reads better than ":ID" in every tool that will open this.
const (
	headerID      = ":ID"
	headerStartID = ":START_ID"
	headerEndID   = ":END_ID"
	headerLabel   = ":LABEL"
	headerType    = ":TYPE"
)

// parquetKind is what a column becomes.
type parquetKind int

const (
	textColumn parquetKind = iota
	intColumn
	floatColumn
	boolColumn
	listColumn
)

type parquetColumn struct {
	name string
	kind parquetKind
	// index is where this column sits in the Parquet schema and csv is where it
	// sits in the file, and they are not the same. parquet.Group is a map and
	// NewSchema orders its fields by name, so the two agree only when the header
	// happens to be alphabetical.
	//
	// The column index carried on a value is a label rather than an address. The
	// writer takes the values of a row in order and hands them to its columns in
	// order, so a row assembled in header order writes every value into the wrong
	// column, and the first one whose type does not match crashes the process
	// rather than being quietly wrong. Rows are assembled in schema order.
	index int
	csv   int
	// endpoint marks a :START_ID column, which is the only thing that says a
	// file holds edges rather than nodes. label marks the :LABEL column, whose
	// values are worth collecting as they go past.
	endpoint bool
	label    bool
}

// ParquetTable is what one CSV file became.
type ParquetTable struct {
	Name string
	// Kind is "nodes" or "relationships", read off the columns rather than off
	// the file name. Every consumer of this dataset needs to know which tables
	// hold the things and which hold the edges between them, and a naming
	// convention would be a convention somebody has to be told.
	Kind string
	// Labels is every distinct node label that appeared, sorted. It is counted
	// rather than asserted, because the number of labels the schema defines and
	// the number the data actually carries are different numbers, and the second
	// one is the one a reader cares about.
	Labels []string
	Rows   int64
	Files  []string
	Bytes  int64
}

const (
	NodeTable         = "nodes"
	RelationshipTable = "relationships"
)

// ToParquet converts every CSV in the neo4j export at dir into Parquet under
// out, laid out the way a dataset hub expects: one directory per table, holding
// shards named for the split they belong to.
//
// shard is the largest number of rows to put in one file. Hubs and query engines
// both do better with a handful of medium files than with one enormous one, and
// a reader that wants a sample can take the first shard rather than the first
// gigabyte.
func ToParquet(dir, out string, shard int) ([]ParquetTable, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.csv"))
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no CSV files in %s, run luatdo export neo4j first", dir)
	}
	sort.Strings(names)

	var tables []ParquetTable
	for _, name := range names {
		table, err := convertCSV(name, out, shard)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(name), err)
		}
		tables = append(tables, table)
	}
	return tables, nil
}

func convertCSV(path, out string, shard int) (ParquetTable, error) {
	table := ParquetTable{Name: strings.TrimSuffix(filepath.Base(path), ".csv")}
	f, err := os.Open(path)
	if err != nil {
		return table, err
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)
	// The provision text is a paragraph of Vietnamese with newlines in it, so a
	// record spans lines and the reader has to be told the field count is fixed
	// rather than inferring it from the first line it can parse.
	r.ReuseRecord = true
	header, err := r.Read()
	if err != nil {
		return table, err
	}
	columns, err := parseParquetHeader(header)
	if err != nil {
		return table, err
	}
	table.Kind = tableKind(columns)
	schema := parquetSchema(table.Name, columns)

	dir := filepath.Join(out, "data", table.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return table, err
	}

	labelColumn := -1
	for _, c := range columns {
		if c.label {
			labelColumn = c.csv
		}
	}
	seen := map[string]bool{}

	w := newShardWriter(dir, schema, shard)
	values := make([]parquet.Value, 0, len(columns)+8)
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return table, err
		}
		if labelColumn >= 0 && labelColumn < len(record) {
			for label := range strings.SplitSeq(record[labelColumn], arrayDelimiter) {
				if label != "" && !seen[label] {
					seen[label] = true
					table.Labels = append(table.Labels, label)
				}
			}
		}
		values = appendRow(values[:0], record, columns)
		if err := w.write(values); err != nil {
			return table, err
		}
		table.Rows++
	}
	sort.Strings(table.Labels)
	files, bytes, err := w.close()
	if err != nil {
		return table, err
	}
	table.Files, table.Bytes = files, bytes
	return table, nil
}

// parseParquetHeader reads a neo4j import header into column definitions.
func parseParquetHeader(header []string) ([]parquetColumn, error) {
	columns := make([]parquetColumn, len(header))
	seen := make(map[string]bool, len(header))
	for i, field := range header {
		c, err := parseParquetColumn(field)
		if err != nil {
			return nil, err
		}
		// A duplicate name would silently lose a column, because the schema is
		// built from a map.
		if seen[c.name] {
			return nil, fmt.Errorf("two columns are both called %q", c.name)
		}
		seen[c.name] = true
		columns[i] = c
	}
	return columns, nil
}

// tableKind decides what a table holds from the columns it has.
//
// An edge file is the one with endpoints, which is the same rule neo4j-admin
// uses when it is handed a file with no other hint about it.
func tableKind(columns []parquetColumn) string {
	for _, c := range columns {
		if c.endpoint {
			return RelationshipTable
		}
	}
	return NodeTable
}

// parseParquetColumn reads one header field.
//
// A field is a name, a colon and a directive, and any of the three can be
// missing. The property columns are written as "title" or "step:int", and the
// structural ones are written as "id:ID" or as a bare ":START_ID" with no name
// at all, so the name has to be defaulted from the directive when it is absent.
func parseParquetColumn(field string) (parquetColumn, error) {
	name, suffix, hasSuffix := strings.Cut(field, ":")
	if !hasSuffix {
		if name == "" {
			return parquetColumn{}, fmt.Errorf("header field %q names no column", field)
		}
		return parquetColumn{name: name}, nil
	}
	// The colon names outside an import mean nothing, so they are given ordinary
	// names here. Anything reading this dataset joins start_id to id and should
	// not have to know how neo4j-admin spells either.
	fallback := func(d string) string {
		if name != "" {
			return name
		}
		return d
	}
	switch ":" + suffix {
	case headerID:
		return parquetColumn{name: fallback("id")}, nil
	case headerStartID:
		return parquetColumn{name: fallback("start_id"), endpoint: true}, nil
	case headerEndID:
		return parquetColumn{name: fallback("end_id")}, nil
	case headerLabel:
		// Labels are a list. Almost every node has one, and the one that has two
		// is exactly the node somebody querying this would want to find.
		return parquetColumn{name: fallback("labels"), kind: listColumn, label: true}, nil
	case headerType:
		return parquetColumn{name: fallback("type")}, nil
	}
	if name == "" {
		return parquetColumn{}, fmt.Errorf("header field %q names no column", field)
	}
	switch suffix {
	case "int":
		return parquetColumn{name: name, kind: intColumn}, nil
	case "float":
		return parquetColumn{name: name, kind: floatColumn}, nil
	case "boolean":
		return parquetColumn{name: name, kind: boolColumn}, nil
	case "string[]":
		return parquetColumn{name: name, kind: listColumn}, nil
	case "string", "":
		return parquetColumn{name: name}, nil
	}
	// Refused rather than treated as text. A type this does not know is a type
	// somebody added to the export, and quietly writing it as a string would
	// produce a dataset where one column is numbers in quotes and nothing says
	// why.
	return parquetColumn{}, fmt.Errorf("header field %q has type %q, which this does not know how to convert", field, suffix)
}

// parquetSchema builds the schema and records where each column landed in it.
func parquetSchema(name string, columns []parquetColumn) *parquet.Schema {
	group := parquet.Group{}
	for _, c := range columns {
		switch c.kind {
		case intColumn:
			group[c.name] = parquet.Optional(parquet.Int(64))
		case floatColumn:
			group[c.name] = parquet.Optional(parquet.Leaf(parquet.DoubleType))
		case boolColumn:
			group[c.name] = parquet.Optional(parquet.Leaf(parquet.BooleanType))
		case listColumn:
			group[c.name] = parquet.Repeated(parquet.String())
		default:
			// Optional, not required, and that is the whole of how an empty CSV
			// field is handled. A missing deadline and a deadline of "" are
			// different things, and every tool that reads this can tell null from
			// the empty string while none of them can tell it from "".
			group[c.name] = parquet.Optional(parquet.String())
		}
	}
	schema := parquet.NewSchema(name, group)
	position := make(map[string]int, len(columns))
	for i, field := range schema.Fields() {
		position[field.Name()] = i
	}
	ordered := make([]parquetColumn, len(columns))
	for i := range columns {
		columns[i].csv = i
		columns[i].index = position[columns[i].name]
		ordered[columns[i].index] = columns[i]
	}
	copy(columns, ordered)
	return schema
}

// appendRow turns one CSV record into Parquet values, in schema order.
//
// columns is already in schema order, and each one remembers where it came from
// in the file, so this walks the row the writer expects and reaches sideways
// into the record for each field.
func appendRow(values []parquet.Value, record []string, columns []parquetColumn) []parquet.Value {
	for _, c := range columns {
		// A short record is a truncated line rather than a reason to stop. The
		// fields that are there are still worth having, and the ones that are not
		// come out null.
		raw := ""
		if c.csv < len(record) {
			raw = record[c.csv]
		}
		values = appendValue(values, raw, c)
	}
	return values
}

func appendValue(values []parquet.Value, raw string, c parquetColumn) []parquet.Value {
	if c.kind == listColumn {
		return appendList(values, raw, c.index)
	}
	// An empty field is null. Definition level 0 on an optional column is how
	// Parquet spells that.
	if raw == "" {
		return append(values, parquet.NullValue().Level(0, 0, c.index))
	}
	switch c.kind {
	case intColumn:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			// Not an error. The export writes what the extraction produced, and
			// a field that should be a number and is not is a fact about the
			// data rather than about this conversion. Null loses less than
			// stopping the whole table would.
			return append(values, parquet.NullValue().Level(0, 0, c.index))
		}
		return append(values, parquet.Int64Value(n).Level(0, 1, c.index))
	case floatColumn:
		x, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return append(values, parquet.NullValue().Level(0, 0, c.index))
		}
		return append(values, parquet.DoubleValue(x).Level(0, 1, c.index))
	case boolColumn:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return append(values, parquet.NullValue().Level(0, 0, c.index))
		}
		return append(values, parquet.BooleanValue(b).Level(0, 1, c.index))
	default:
		return append(values, parquet.ByteArrayValue([]byte(raw)).Level(0, 1, c.index))
	}
}

// appendList splits an array field on the delimiter the export writes.
//
// The first element of a repeated column carries repetition level 0 and every
// element after it carries 1, which is how Parquet says where one row's list
// ends and the next begins. An empty field is an empty list rather than a list
// holding one empty string.
func appendList(values []parquet.Value, raw string, index int) []parquet.Value {
	if raw == "" {
		return append(values, parquet.NullValue().Level(0, 0, index))
	}
	for i, part := range strings.Split(raw, arrayDelimiter) {
		repetition := 1
		if i == 0 {
			repetition = 0
		}
		values = append(values, parquet.ByteArrayValue([]byte(part)).Level(repetition, 1, index))
	}
	return values
}

// shardWriter writes a table as numbered files, starting a new one every shard
// rows.
//
// The files are named for their position out of the total, which is the naming
// every dataset hub and query engine already recognises, and the total is not
// known until the last row is read. So they are written under a temporary name
// and renamed at the end, which costs one rename per file and nothing else.
type shardWriter struct {
	dir    string
	schema *parquet.Schema
	shard  int

	writer  *parquet.GenericWriter[any]
	file    *os.File
	rows    int
	written []string
	pending []parquet.Row
	err     error
}

// Rows are handed to the writer in batches, because a call per row spends more
// time in bookkeeping than in writing.
const parquetBatch = 512

// A row group is the unit Parquet reads and skips whole, so it is also the unit
// this holds in memory while writing. Fifty thousand rows of provision text is a
// few hundred megabytes, which is the largest this should ask of a machine that
// is also running a database.
const parquetRowGroup = 50_000

func newShardWriter(dir string, schema *parquet.Schema, shard int) *shardWriter {
	return &shardWriter{dir: dir, schema: schema, shard: shard}
}

func (w *shardWriter) write(values []parquet.Value) error {
	if w.err != nil {
		return w.err
	}
	if w.writer == nil || (w.shard > 0 && w.rows >= w.shard) {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	// The values are copied because the caller reuses its slice, and a row handed
	// to the batch keeps pointing at that memory until the batch is flushed.
	row := make(parquet.Row, len(values))
	copy(row, values)
	w.pending = append(w.pending, row)
	w.rows++
	if len(w.pending) >= parquetBatch {
		return w.flush()
	}
	return nil
}

func (w *shardWriter) flush() error {
	if len(w.pending) == 0 {
		return nil
	}
	if _, err := w.writer.WriteRows(w.pending); err != nil {
		w.err = err
		return err
	}
	w.pending = w.pending[:0]
	return nil
}

func (w *shardWriter) rotate() error {
	if err := w.finishFile(); err != nil {
		return err
	}
	name := filepath.Join(w.dir, fmt.Sprintf("part-%05d.parquet", len(w.written)))
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	w.file = f
	w.writer = parquet.NewGenericWriter[any](f, w.schema,
		parquet.Compression(&zstd.Codec{}),
		parquet.MaxRowsPerRowGroup(parquetRowGroup),
	)
	w.written = append(w.written, name)
	w.rows = 0
	return nil
}

func (w *shardWriter) finishFile() error {
	if w.writer == nil {
		return nil
	}
	if err := w.flush(); err != nil {
		return err
	}
	if err := w.writer.Close(); err != nil {
		return err
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	w.writer, w.file = nil, nil
	return nil
}

// close finishes the last file and gives every file its final name.
func (w *shardWriter) close() ([]string, int64, error) {
	if err := w.finishFile(); err != nil {
		return nil, 0, err
	}
	// A table with no rows still gets a file. An empty table and a table nobody
	// exported look the same otherwise, and the difference between those two is
	// most of what the graph audit was about.
	if len(w.written) == 0 {
		if err := w.rotate(); err != nil {
			return nil, 0, err
		}
		if err := w.finishFile(); err != nil {
			return nil, 0, err
		}
	}
	var names []string
	var total int64
	for i, temporary := range w.written {
		final := filepath.Join(w.dir, fmt.Sprintf("train-%05d-of-%05d.parquet", i, len(w.written)))
		if err := os.Rename(temporary, final); err != nil {
			return nil, 0, err
		}
		info, err := os.Stat(final)
		if err != nil {
			return nil, 0, err
		}
		total += info.Size()
		names = append(names, filepath.Base(final))
	}
	return names, total, nil
}
