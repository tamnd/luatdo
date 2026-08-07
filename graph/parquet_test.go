package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// The conversion is worth testing at the value level rather than the file level,
// because every way it can be wrong is a way that produces a perfectly valid
// Parquet file. A column written under the wrong index, an empty field written
// as "" instead of null, an array written as one string with pipes in it: all of
// those load without complaint and are wrong in a way nobody notices until they
// query for something and get nothing.

// writeExport lays down a CSV export the way graph.Export would.
func writeExport(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// readParquet reads a whole table back as maps keyed by column name.
func readParquet(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	pf, err := parquet.OpenFile(f, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	fields := pf.Schema().Fields()
	var out []map[string]any
	rows := parquet.NewReader(pf) //nolint:staticcheck // the typed readers need a Go type, and this schema is only known at run time
	defer func() { _ = rows.Close() }()
	buf := make([]parquet.Row, 8)
	for {
		n, err := rows.ReadRows(buf)
		for _, row := range buf[:n] {
			m := map[string]any{}
			for _, v := range row {
				name := fields[v.Column()].Name()
				switch {
				case v.IsNull():
					if _, ok := m[name]; !ok {
						m[name] = nil
					}
				case fields[v.Column()].Repeated():
					list, _ := m[name].([]string)
					m[name] = append(list, v.String())
				case v.Kind() == parquet.Int64:
					m[name] = v.Int64()
				case v.Kind() == parquet.Double:
					m[name] = v.Double()
				case v.Kind() == parquet.Boolean:
					m[name] = v.Boolean()
				default:
					m[name] = v.String()
				}
			}
			out = append(out, m)
		}
		if err != nil {
			break
		}
	}
	return out
}

func TestParquetCarriesEveryColumnTypeThroughIntact(t *testing.T) {
	dir := writeExport(t, map[string]string{
		"widgets.csv": "id:ID,title,rank:int,score:float,live:boolean,aliases:string[],:LABEL\n" +
			"w1,First,3,0.5,true,a|b,Widget\n" +
			"w2,,,,,,Widget|Thing\n",
	})
	out := t.TempDir()
	tables, err := ToParquet(dir, out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0].Rows != 2 {
		t.Fatalf("converted %d tables with %v rows, want one table of two rows", len(tables), tables)
	}
	if tables[0].Kind != NodeTable {
		t.Errorf("a table with an :ID column came out as %q", tables[0].Kind)
	}

	rows := readParquet(t, filepath.Join(out, "data", "widgets", tables[0].Files[0]))
	if len(rows) != 2 {
		t.Fatalf("read %d rows back, want 2", len(rows))
	}
	// Every one of these is a column whose Parquet index differs from its CSV
	// index, because the schema orders fields by name. Reading them by name is
	// the point of the assertion.
	full := rows[0]
	for name, want := range map[string]any{
		"id": "w1", "title": "First", "rank": int64(3), "score": 0.5, "live": true,
	} {
		if got := full[name]; got != want {
			t.Errorf("%s is %#v, want %#v", name, got, want)
		}
	}
	if got, want := strings.Join(full["aliases"].([]string), ","), "a,b"; got != want {
		t.Errorf("aliases is %q, want %q, so the array was not split on the delimiter", got, want)
	}
	if got, want := strings.Join(full["labels"].([]string), ","), "Widget"; got != want {
		t.Errorf("labels is %q, want %q", got, want)
	}
	// The row of empty fields. Every one of these has to be null and not the
	// zero value, because a rank of zero and no rank at all are different claims
	// and the CSV cannot tell them apart.
	empty := rows[1]
	for _, name := range []string{"title", "rank", "score", "live", "aliases"} {
		if got, ok := empty[name]; !ok || got != nil {
			t.Errorf("%s on the empty row is %#v, want null", name, got)
		}
	}
	if got, want := strings.Join(rows[1]["labels"].([]string), ","), "Widget,Thing"; got != want {
		t.Errorf("the two label node came back as %q, want %q", got, want)
	}
}

func TestParquetCountsTheLabelsThatAreActuallyThere(t *testing.T) {
	dir := writeExport(t, map[string]string{
		"widgets.csv": "id:ID,:LABEL\nw1,Widget\nw2,Widget|Thing\nw3,\n",
	})
	tables, err := ToParquet(dir, t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	// Sorted, deduplicated, and counted off the rows. The card used to assert a
	// label count from the schema, which was four higher than what the data had,
	// because several layers export as headers and nothing else.
	if got, want := strings.Join(tables[0].Labels, ","), "Thing,Widget"; got != want {
		t.Errorf("labels are %q, want %q", got, want)
	}
}

func TestParquetRenamesTheImportDirectivesAndSpotsEdges(t *testing.T) {
	dir := writeExport(t, map[string]string{
		"links.csv": ":START_ID,:END_ID,:TYPE,weight:float\nw1,w2,POINTS_AT,1.5\n",
	})
	out := t.TempDir()
	tables, err := ToParquet(dir, out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tables[0].Kind != RelationshipTable {
		t.Errorf("a table with endpoints came out as %q", tables[0].Kind)
	}
	rows := readParquet(t, filepath.Join(out, "data", "links", tables[0].Files[0]))
	// The colon names are import directives and mean nothing outside an import.
	// Anything reading this dataset joins start_id to id, and it should not have
	// to know that neo4j-admin spells it with a colon.
	for name, want := range map[string]any{
		"start_id": "w1", "end_id": "w2", "type": "POINTS_AT", "weight": 1.5,
	} {
		if got := rows[0][name]; got != want {
			t.Errorf("%s is %#v, want %#v", name, got, want)
		}
	}
}

func TestParquetShardsAndNamesEveryFileForTheTotal(t *testing.T) {
	body := "id:ID\na\nb\nc\nd\ne\n"
	out := t.TempDir()
	tables, err := ToParquet(writeExport(t, map[string]string{"things.csv": body}), out, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"train-00000-of-00003.parquet",
		"train-00001-of-00003.parquet",
		"train-00002-of-00003.parquet",
	}
	if got := strings.Join(tables[0].Files, " "); got != strings.Join(want, " ") {
		t.Fatalf("shards are %q, want %q", got, strings.Join(want, " "))
	}
	// The count in the name is the total, and the total is not known until the
	// last row is read, so this is the assertion that the rename at the end
	// actually happened. A file left under its temporary name is a file the
	// glob in the dataset card does not match.
	var total int
	for _, name := range tables[0].Files {
		total += len(readParquet(t, filepath.Join(out, "data", "things", name)))
	}
	if total != 5 {
		t.Errorf("the shards hold %d rows between them, want 5", total)
	}
	entries, err := os.ReadDir(filepath.Join(out, "data", "things"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("the table directory holds %d files, want 3, so something was left behind", len(entries))
	}
}

func TestParquetPublishesAnEmptyTableRatherThanSkippingIt(t *testing.T) {
	out := t.TempDir()
	tables, err := ToParquet(writeExport(t, map[string]string{"sanctions.csv": "id:ID,:LABEL\n"}), out, 0)
	if err != nil {
		t.Fatal(err)
	}
	// A layer that came out empty and a layer nobody exported are different
	// facts, and the file list is where that difference is visible. Skipping the
	// table would also break the dataset card, which names a config per table
	// and fails to load entirely if one of them has no files.
	if len(tables[0].Files) != 1 {
		t.Fatalf("an empty table produced %d files, want 1", len(tables[0].Files))
	}
	if rows := readParquet(t, filepath.Join(out, "data", "sanctions", tables[0].Files[0])); len(rows) != 0 {
		t.Errorf("the empty table holds %d rows", len(rows))
	}
}

func TestParquetRefusesAHeaderItDoesNotUnderstand(t *testing.T) {
	// Quietly writing an unknown type as text would produce a dataset where one
	// column is numbers in quotes and nothing anywhere says why.
	_, err := ToParquet(writeExport(t, map[string]string{"x.csv": "id:ID,when:datetime\n"}), t.TempDir(), 0)
	if err == nil || !strings.Contains(err.Error(), "datetime") {
		t.Fatalf("a header with an unknown type gave %v, want a refusal naming the type", err)
	}
}

func TestParquetRefusesTwoColumnsWithOneName(t *testing.T) {
	// The schema is built from a map, so a duplicate would silently drop a
	// column rather than producing a file with two of them.
	_, err := ToParquet(writeExport(t, map[string]string{"x.csv": "id:ID,id\n"}), t.TempDir(), 0)
	if err == nil || !strings.Contains(err.Error(), "two columns") {
		t.Fatalf("a duplicated column name gave %v, want a refusal", err)
	}
}

func TestParquetSaysWhereToLookWhenThereIsNoExport(t *testing.T) {
	_, err := ToParquet(t.TempDir(), t.TempDir(), 0)
	if err == nil || !strings.Contains(err.Error(), "export neo4j") {
		t.Fatalf("converting an empty directory gave %v, want a pointer at the command that fills it", err)
	}
}

func TestParquetKeepsMultilineFieldsWhole(t *testing.T) {
	// The provision text is a paragraph with newlines in it, which is why the
	// import runs with --multiline-fields. A reader that treated a newline as a
	// record boundary would truncate most of the corpus.
	dir := writeExport(t, map[string]string{
		"texts.csv": "id:ID,body\nt1,\"first line\nsecond line\"\n",
	})
	out := t.TempDir()
	tables, err := ToParquet(dir, out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tables[0].Rows != 1 {
		t.Fatalf("a two line field parsed as %d rows", tables[0].Rows)
	}
	rows := readParquet(t, filepath.Join(out, "data", "texts", tables[0].Files[0]))
	if got := rows[0]["body"]; got != "first line\nsecond line" {
		t.Errorf("body is %q, want both lines", got)
	}
}
