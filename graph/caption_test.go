package graph

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
)

// captionRule finds the property each label is captioned with. The grass file
// is the only place that says it, so the test reads it there rather than
// keeping a second list that can disagree with the first.
var captionRule = regexp.MustCompile(`node\.([A-Za-z_][A-Za-z0-9_]*) \{[^}]*caption: '\{([a-z_]+)\}'`)

// captionProperties is label to the property that names it in the browser.
func captionProperties() map[string]string {
	out := map[string]string{}
	for _, m := range captionRule.FindAllStringSubmatch(Style, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// eachExportedNode visits every node row an export wrote, as a label, the
// header of the file it came from and the row itself.
func eachExportedNode(t *testing.T, dir string, visit func(file, label string, header, row []string)) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".csv" {
			continue
		}
		rows := readCSV(t, filepath.Join(dir, e.Name()))
		if len(rows) < 2 {
			continue
		}
		labelAt := slices.Index(rows[0], ":LABEL")
		if labelAt < 0 {
			continue
		}
		for _, row := range rows[1:] {
			for _, label := range strings.Split(row[labelAt], arrayDelimiter) {
				visit(e.Name(), label, rows[0], row)
			}
		}
	}
}

// field reads a value by header name, allowing for the type suffix the importer
// wants on a column that is not a string. It reports whether the column is
// there at all, because a caption naming a column nothing writes and a caption
// naming an empty one are different faults.
func field(header, row []string, name string) (string, bool) {
	for i, h := range header {
		if h == name || strings.HasPrefix(h, name+":") {
			if i < len(row) {
				return row[i], true
			}
			return "", true
		}
	}
	return "", false
}

func TestEveryExportedNodeCarriesItsCaption(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, competencyFixture()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	captions := captionProperties()
	// A node whose caption property is missing or empty draws as a circle with
	// nothing written in it, which a reader cannot tell apart from a rendering
	// fault. The style file was checked for a rule per label already; this is
	// the other half, that the rule names something the data has.
	// One complaint per label, because a fault in the style file is a fault in
	// every row of that label and a hundred identical lines say nothing the
	// first one did not. The count of blank rows goes in the message so the cap
	// is not silent.
	blank, missing := map[string]int{}, map[string]string{}
	eachExportedNode(t, dir, func(file, label string, header, row []string) {
		property, styled := captions[label]
		if !styled {
			return
		}
		switch value, ok := field(header, row, property); {
		case !ok:
			missing[label] = file + " has no " + property + " column"
		case strings.TrimSpace(value) == "":
			blank[label]++
		}
	})
	for label, why := range missing {
		t.Errorf("the style file captions %s with a property it does not carry: %s", label, why)
	}
	for label, n := range blank {
		t.Errorf("%d %s rows have an empty caption, so those nodes render blank", n, label)
	}
}

// undatedAndDefinition is one document whose commencement date the source never
// recorded, holding a norm that states no action. Both are the shapes that used
// to render as an empty circle: 21,678 wordings and 2 norms in the corpus.
func undatedAndDefinition() Input {
	doc := &law.Document{
		ID: "vn:law:2010:46-2010-qh12", OfficialNumber: "46/2010/QH12",
		Title: "Luật Ngân hàng Nhà nước Việt Nam", DocType: "law",
		SignedOn: "16/06/2010", Status: "parsed",
		Provisions: []law.Provision{{
			ID: "vn:law:2010:46-2010-qh12:article-6", Kind: "article", Number: "6",
			Heading: "Giải thích từ ngữ", Text: "Tiền tệ là phương tiện thanh toán.",
			TextHash: "0011223344556677aabb", Position: 1,
		}},
	}
	statement := norm.Record{
		ID: "vn:norm:def01", DocID: doc.ID, ProvisionID: doc.Provisions[0].ID,
		Statement: norm.Statement{
			Type:   "definition",
			Object: &norm.Ref{Text: "tiền tệ"},
			Evidence: norm.Evidence{
				Quote: "Tiền tệ là phương tiện thanh toán.", Start: 0, End: 34,
			},
			Confidence: 0.9,
		},
		Status: "verified", Model: "test-model", OntologyVersion: 1,
	}
	return Input{Docs: []*law.Document{doc}, Statements: []norm.Record{statement}, Registry: ontology.Seed()}
}

func TestAWordingWithNoDateStillHasACaption(t *testing.T) {
	dir := t.TempDir()
	if err := Export(dir, undatedAndDefinition()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	versions := readCSV(t, filepath.Join(dir, "text_versions.csv"))
	if len(versions) != 2 {
		t.Fatalf("text_versions.csv rows = %d, want header plus 1", len(versions))
	}
	from, _ := field(versions[0], versions[1], "from_date")
	caption, _ := field(versions[0], versions[1], "caption")
	if from != "" {
		t.Errorf("from_date = %q, and the source gave a signing date rather than a commencement date", from)
	}
	if caption == "" {
		t.Error("the wording renders as an empty circle")
	}
	norms := readCSV(t, filepath.Join(dir, "norms.csv"))
	if len(norms) != 2 {
		t.Fatalf("norms.csv rows = %d, want header plus 1", len(norms))
	}
	action, _ := field(norms[0], norms[1], "action")
	normCap, _ := field(norms[0], norms[1], "caption")
	if action != "" {
		t.Errorf("action = %q, a definition states none", action)
	}
	if normCap != "tiền tệ" {
		t.Errorf("the definition is captioned %q, want what it defines", normCap)
	}
}

func TestExportedDatesSort(t *testing.T) {
	dir := t.TempDir()
	in := undatedAndDefinition()
	in.Docs[0].EffectiveFrom = "01/01/2011"
	in.Docs[0].ExpiredOn = "01/07/2024"
	in.Docs[0].ForceStatus = "Hết hiệu lực toàn bộ"
	if err := Export(dir, in); err != nil {
		t.Fatalf("Export: %v", err)
	}
	docs := readCSV(t, filepath.Join(dir, "documents.csv"))
	get := func(name string) string {
		v, ok := field(docs[0], docs[1], name)
		if !ok {
			t.Fatalf("documents.csv has no column %q, header is %v", name, docs[0])
		}
		return v
	}
	// The source writes 17/08/2007, which sorts by day of the month. A graph
	// that compares dates as text answers "in force on this date" wrongly and
	// looks finished doing it.
	if get("effective_from") != "2011-01-01" || get("signed_on") != "2010-06-16" || get("expired_on") != "2024-07-01" {
		t.Errorf("dates came out %q, %q, %q", get("effective_from"), get("signed_on"), get("expired_on"))
	}
	if get("force_status") != "Hết hiệu lực toàn bộ" {
		t.Errorf("force_status = %q, and it is kept in the source's own words", get("force_status"))
	}
	versions := readCSV(t, filepath.Join(dir, "text_versions.csv"))
	from, _ := field(versions[0], versions[1], "from_date")
	to, _ := field(versions[0], versions[1], "to_date")
	if from != "2011-01-01" || to != "2024-07-01" {
		t.Errorf("the wording runs %q to %q", from, to)
	}
}
