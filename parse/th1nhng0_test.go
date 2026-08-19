package parse

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// writeParquet lays out one config the way a fetched revision does.
func writeParquet[T any](t *testing.T, revisionDir, config string, rows []T) {
	t.Helper()
	path := th1nhng0Path(revisionDir, config)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := parquet.WriteFile(path, rows); err != nil {
		t.Fatalf("write %s: %v", config, err)
	}
}

// meta is three documents: two with a usable official number, one whose
// number has no year and so can never have a stable identifier.
func meta() []th1nhng0Meta {
	return []th1nhng0Meta{
		{ID: "1001", Title: "Bộ luật Lao động", SoKyHieu: "45/2019/QH14", LoaiVanBan: "Bộ luật", NgayHieuLuc: "01/01/2021",
			NgayBanHanh: "20/11/2019", NgayHetHieu: "01/07/2026", TinhTrang: "Hết hiệu lực toàn bộ"},
		{ID: "1002", Title: "Luật Doanh nghiệp", SoKyHieu: "59/2020/QH14", LoaiVanBan: "Luật", NgayHieuLuc: "01/01/2021"},
		{ID: "1003", Title: "Công văn không số", SoKyHieu: "khong-so", LoaiVanBan: "Công văn"},
	}
}

func collect(t *testing.T, revisionDir string) (map[string]Input, Th1nhng0Stats) {
	t.Helper()
	got := map[string]Input{}
	stats, err := EachTh1nhng0(revisionDir, "abc123", func(in Input) error {
		got[in.OfficialNumber] = in
		return nil
	})
	if err != nil {
		t.Fatalf("EachTh1nhng0: %v", err)
	}
	return got, stats
}

func TestEachTh1nhng0MetadataOnly(t *testing.T) {
	dir := t.TempDir()
	writeParquet(t, dir, "metadata", meta())

	got, stats := collect(t, dir)
	if len(got) != 2 || stats.Metadata != 3 || stats.Content != 0 || stats.Unnumbered != 1 {
		t.Fatalf("inputs = %d, stats = %+v", len(got), stats)
	}
	in := got["45/2019/QH14"]
	if !in.MetadataOnly || in.Content != "" {
		t.Errorf("input = %+v, without the content config there is no text", in)
	}
	if in.Title != "Bộ luật Lao động" || in.DocType != "Bộ luật" || in.EffectiveFrom != "01/01/2021" {
		t.Errorf("input = %+v", in)
	}
	if in.SignedOn != "20/11/2019" || in.ExpiredOn != "01/07/2026" || in.ForceStatus != "Hết hiệu lực toàn bộ" {
		t.Errorf("what the source says about force did not come through: %+v", in)
	}
	if in.Source != "th1nhng0" || in.SourceRef != "abc123" {
		t.Errorf("provenance = %s at %s", in.Source, in.SourceRef)
	}
	if in.SourceURL != "https://vbpl.vn/pages/portal.aspx?ItemID=1001" {
		t.Errorf("source url = %q, it must point back at the official page", in.SourceURL)
	}

	// A document with no text is still a node, because the official citation
	// graph points at it.
	doc, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Status != "metadata" || len(doc.Provisions) != 0 {
		t.Errorf("document = %s with %d provisions", doc.Status, len(doc.Provisions))
	}
}

func TestEachTh1nhng0WithContent(t *testing.T) {
	dir := t.TempDir()
	writeParquet(t, dir, "metadata", meta())
	writeParquet(t, dir, "content", []th1nhng0Content{
		{ID: "1001", ContentHTML: "<p>Điều 1. Phạm vi</p><p>Điều 2. Hiệu lực</p>"},
		{ID: "1003", ContentHTML: "<p>Điều 1. Không có số hiệu</p>"},
		{ID: "9999", ContentHTML: "<p>Không có trong metadata</p>"},
	})

	got, stats := collect(t, dir)
	if stats.Content != 1 || stats.Unnumbered != 1 {
		t.Errorf("stats = %+v, only one row has both text and a usable number", stats)
	}
	if len(got) != 2 {
		t.Fatalf("inputs = %d, want the text row and the metadata only row", len(got))
	}
	withText := got["45/2019/QH14"]
	if withText.MetadataOnly || withText.Content != "Điều 1. Phạm vi\n\nĐiều 2. Hiệu lực" {
		t.Errorf("input = %+v, the HTML must arrive rendered", withText)
	}
	if !got["59/2020/QH14"].MetadataOnly {
		t.Error("a document the content config does not carry is metadata only, not absent")
	}

	doc, err := Parse(withText)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Status != "parsed" || len(doc.Provisions) != 2 {
		t.Errorf("document = %s with %d provisions, %q", doc.Status, len(doc.Provisions), doc.Quarantine)
	}
}

// TestEachTh1nhng0LocalDocuments covers what the breadth corpus is actually
// made of. Two thirds of it is issued by provinces, where the number alone is
// not an identity, and the rows that are not documents at all outnumber the
// laws.
func TestEachTh1nhng0LocalDocuments(t *testing.T) {
	dir := t.TempDir()
	writeParquet(t, dir, "metadata", []th1nhng0Meta{
		{ID: "2001", Title: "Quyết định Long An", SoKyHieu: "01/2024/QĐ-UBND", LoaiVanBan: "Quyết định", CoQuan: "UBND tỉnh Long An"},
		{ID: "2002", Title: "Quyết định Lạng Sơn", SoKyHieu: "01/2024/QĐ-UBND", LoaiVanBan: "Quyết định", CoQuan: "UBND Tỉnh Lạng Sơn"},
		{ID: "2003", Title: "Quyết định Long An", SoKyHieu: "01/2024/QĐ-UBND", LoaiVanBan: "Quyết định", CoQuan: "UBND tỉnh Long An"},
		{ID: "2004", Title: "Quyết định không rõ cơ quan", SoKyHieu: "02/2024/QĐ-UBND", LoaiVanBan: "Quyết định"},
		{ID: "2005", Title: "Labor Code", SoKyHieu: "45/2019/QH14", LoaiVanBan: translationType, CoQuan: "Quốc hội"},
		{ID: "2006", Title: "Bộ luật Lao động", SoKyHieu: "45/2019/QH14", LoaiVanBan: "Bộ luật", CoQuan: "Quốc hội"},
	})

	ids := map[string]string{}
	stats, err := EachTh1nhng0(dir, "abc123", func(in Input) error {
		doc, err := Parse(in)
		if err != nil {
			return err
		}
		ids[doc.ID] = in.SourceURL
		return nil
	})
	if err != nil {
		t.Fatalf("EachTh1nhng0: %v", err)
	}
	if stats.Translation != 1 || stats.Unattributed != 1 || stats.Duplicate != 1 || stats.Unnumbered != 0 {
		t.Errorf("stats = %+v", stats)
	}
	if len(ids) != 3 {
		t.Fatalf("documents = %v, want the two provinces and the law", ids)
	}
	for _, id := range []string{
		"vn:law:2024:01-2024-qd-ubnd:ubnd-tinh-long-an",
		"vn:law:2024:01-2024-qd-ubnd:ubnd-tinh-lang-son",
		"vn:law:2019:45-2019-qh14",
	} {
		if _, ok := ids[id]; !ok {
			t.Errorf("document %s missing from %v", id, ids)
		}
	}
	// The lowest source row wins the identifier, so parsing a pinned revision
	// twice keeps the same document rather than whichever row arrived first.
	if got := ids["vn:law:2024:01-2024-qd-ubnd:ubnd-tinh-long-an"]; got != "https://vbpl.vn/pages/portal.aspx?ItemID=2001" {
		t.Errorf("Long An came from %q, want the lower of the two duplicate rows", got)
	}
	// The translation carries the number of the law it translates, so taking it
	// as a document would have cost the law itself.
	if ids["vn:law:2019:45-2019-qh14"] != "https://vbpl.vn/pages/portal.aspx?ItemID=2006" {
		t.Error("the Vietnamese document must win over its English translation")
	}
}

func TestEachTh1nhng0MissingMetadata(t *testing.T) {
	if _, err := EachTh1nhng0(t.TempDir(), "abc123", func(Input) error { return nil }); err == nil {
		t.Error("a revision with no metadata config must fail rather than report an empty corpus")
	}
}

// TestTh1nhng0RelationDirection uses the pair the corpus actually carries for
// the two Labour Codes. Reading either row forwards would put an arrow in the
// graph saying the 2012 code repealed the 2019 one.
func TestTh1nhng0RelationDirection(t *testing.T) {
	dir := t.TempDir()
	writeParquet(t, dir, "metadata", []th1nhng0Meta{
		{ID: "27615", Title: "Bộ luật Lao động 2012", SoKyHieu: "10/2012/QH13", LoaiVanBan: "Bộ luật", CoQuan: "Quốc hội"},
		{ID: "139264", Title: "Bộ luật Lao động 2019", SoKyHieu: "45/2019/QH14", LoaiVanBan: "Bộ luật", CoQuan: "Quốc hội"},
	})
	writeParquet(t, dir, "relationships", []th1nhng0Rel{
		// On the 2019 page the 2012 code is the expired document.
		{DocID: "139264", OtherDocID: "27615", Relationship: "Văn bản hết hiệu lực"},
		// On the 2012 page the 2019 code is the document that expired it.
		{DocID: "27615", OtherDocID: "139264", Relationship: "Văn bản quy định hết hiệu lực"},
	})

	got, _, err := Th1nhng0Relations(dir)
	if err != nil {
		t.Fatalf("Th1nhng0Relations: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("relations = %+v, both rows describe one repeal", got)
	}
	if got[0].FromDoc != "vn:law:2019:45-2019-qh14" || got[0].ToDoc != "vn:law:2012:10-2012-qh13" || got[0].Kind != "amends" {
		t.Errorf("relation = %+v, the 2019 code repealed the 2012 one", got[0])
	}
}

func TestTh1nhng0Relations(t *testing.T) {
	dir := t.TempDir()
	writeParquet(t, dir, "metadata", meta())
	writeParquet(t, dir, "relationships", []th1nhng0Rel{
		{DocID: "1002", OtherDocID: "1001", Relationship: "Căn cứ"},
		{DocID: "1002", OtherDocID: "1001", Relationship: "Căn cứ"},
		{DocID: "1001", OtherDocID: "1002", Relationship: "Sửa đổi, bổ sung"},
		// The same amendment seen from the other side. It must fold into the
		// edge above rather than become an arrow pointing back.
		{DocID: "1002", OtherDocID: "1001", Relationship: "Văn bản sửa đổi"},
		{DocID: "1001", OtherDocID: "1003", Relationship: "Dẫn chiếu"},
		{DocID: "1001", OtherDocID: "9999", Relationship: "Dẫn chiếu"},
		{DocID: "1001", OtherDocID: "1001", Relationship: "Dẫn chiếu"},
		{DocID: "1002", OtherDocID: "1001", Relationship: "Nhãn chưa biết"},
	})

	got, dropped, err := Th1nhng0Relations(dir)
	if err != nil {
		t.Fatalf("Th1nhng0Relations: %v", err)
	}
	if dropped != 3 {
		t.Errorf("dropped = %d, want the unnumbered target, the unknown target, and the self link", dropped)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].FromDoc < got[j].FromDoc })
	if len(got) != 2 {
		t.Fatalf("relations = %+v, want one edge per pair and kind", got)
	}
	if got[0].FromDoc != "vn:law:2019:45-2019-qh14" || got[0].ToDoc != "vn:law:2020:59-2020-qh14" {
		t.Errorf("relation = %+v, identifiers must be rewritten from official numbers", got[0])
	}
	if got[0].Kind != "amends" || got[0].Label != "Sửa đổi, bổ sung" {
		t.Errorf("relation = %+v, the amending label projects as an amends edge and is kept verbatim", got[0])
	}
	if got[1].FromDoc != "vn:law:2020:59-2020-qh14" || got[1].Kind != "cites" {
		t.Errorf("relation = %+v, want the citation and the unknown label folded into one", got[1])
	}
}
