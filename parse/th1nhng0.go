package parse

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/parquet-go/parquet-go"
	"github.com/tamnd/luatdo/law"
)

// th1nhng0Meta is the subset of the metadata config luatdo uses. The column
// names are the Vietnamese ones vbpl.vn publishes.
type th1nhng0Meta struct {
	ID          string `parquet:"id"`
	Title       string `parquet:"title"`
	SoKyHieu    string `parquet:"so_ky_hieu"`
	LoaiVanBan  string `parquet:"loai_van_ban"`
	NgayHieuLuc string `parquet:"ngay_co_hieu_luc"`
	CoQuan      string `parquet:"co_quan_ban_hanh"`
}

// translationType marks a row that is an English rendering of another document
// in the corpus. It carries the number of the document it translates, so it is
// not a document of its own and is counted rather than parsed.
const translationType = "Bản dịch văn bản"

// th1nhng0Content is the full text config, 3.5 GB of HTML, so it is streamed
// row by row and never held whole.
type th1nhng0Content struct {
	ID          string `parquet:"id"`
	ContentHTML string `parquet:"content_html"`
}

// th1nhng0Rel is one edge of the official relationship graph. These are
// vbpl.vn's own links between documents, not anything a model inferred, which
// is the reason this corpus is worth the size.
type th1nhng0Rel struct {
	DocID        string `parquet:"doc_id"`
	OtherDocID   string `parquet:"other_doc_id"`
	Relationship string `parquet:"relationship"`
}

// Th1nhng0Stats counts what one pass over the corpus saw. Everything the pass
// refuses is counted under the reason it was refused, because a corpus this
// size is only trustworthy if the documents it does not yield are visible.
type Th1nhng0Stats struct {
	Metadata     int // rows in the metadata config
	Content      int // rows joined with full text
	Unnumbered   int // official number has no year, so no stable identifier
	Unattributed int // local number with no issuing body, so no identity
	Translation  int // English rendering of a document already in the corpus
	Duplicate    int // row landing on an identifier another row already owns
}

// th1nhng0Path locates one config inside a fetched revision. A config that was
// not fetched is simply absent, and the loader works with what is there:
// metadata alone yields document nodes, metadata plus content yields parsed
// provisions.
func th1nhng0Path(revisionDir, config string) string {
	return filepath.Join(revisionDir, "data", config+".parquet")
}

// EachTh1nhng0 streams the corpus into parser inputs, calling visit once per
// document. Metadata is small enough to hold in memory and is loaded first;
// content is streamed against it. Without the content config every document
// still arrives, marked metadata only, because the citation graph needs the
// nodes its edges point at whether or not the text has been downloaded.
func EachTh1nhng0(revisionDir, revision string, visit func(Input) error) (Th1nhng0Stats, error) {
	meta, err := loadTh1nhng0Meta(revisionDir)
	if err != nil {
		return Th1nhng0Stats{}, err
	}
	owners, stats := th1nhng0Owners(meta)

	input := func(m th1nhng0Meta, content string) Input {
		return Input{
			OfficialNumber: m.SoKyHieu,
			IssuingBody:    m.CoQuan,
			Title:          m.Title,
			DocType:        m.LoaiVanBan,
			Content:        content,
			Source:         "th1nhng0",
			SourceRef:      revision,
			SourceURL:      "https://vbpl.vn/pages/portal.aspx?ItemID=" + m.ID,
			EffectiveFrom:  m.NgayHieuLuc,
			MetadataOnly:   content == "",
		}
	}

	contentPath := th1nhng0Path(revisionDir, "content")
	if _, err := os.Stat(contentPath); err != nil {
		for row := range owners {
			if err := visit(input(meta[row], "")); err != nil {
				return stats, err
			}
		}
		return stats, nil
	}

	seen := map[string]bool{}
	if err := eachParquetRow(contentPath, func(row th1nhng0Content) error {
		if !owners[row.ID] {
			return nil
		}
		seen[row.ID] = true
		stats.Content++
		return visit(input(meta[row.ID], HTMLText(row.ContentHTML)))
	}); err != nil {
		return stats, err
	}
	for row := range owners {
		if seen[row] {
			continue
		}
		if err := visit(input(meta[row], "")); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

// th1nhng0Owners picks the one metadata row that stands for each document and
// counts every row it turned down. The corpus has real duplicates, whole rows
// repeated up to eight times, and it has documents that only differ by the
// province that issued them, which is what the identifier now carries. Where
// rows still land on one identifier the lowest source row wins, so a rebuild
// of a pinned revision picks the same row every time.
func th1nhng0Owners(meta map[string]th1nhng0Meta) (map[string]bool, Th1nhng0Stats) {
	stats := Th1nhng0Stats{Metadata: len(meta)}
	owner := make(map[string]string, len(meta))
	for row, m := range meta {
		if m.LoaiVanBan == translationType {
			stats.Translation++
			continue
		}
		if _, err := law.DocID(m.SoKyHieu); err != nil {
			stats.Unnumbered++
			continue
		}
		id, err := law.DocIDIn(m.SoKyHieu, m.CoQuan)
		if err != nil {
			stats.Unattributed++
			continue
		}
		if held, taken := owner[id]; !taken || row < held {
			owner[id] = row
		}
	}
	stats.Duplicate = len(meta) - stats.Translation - stats.Unnumbered - stats.Unattributed - len(owner)
	owners := make(map[string]bool, len(owner))
	for _, row := range owner {
		owners[row] = true
	}
	return owners, stats
}

func loadTh1nhng0Meta(revisionDir string) (map[string]th1nhng0Meta, error) {
	path := th1nhng0Path(revisionDir, "metadata")
	rows, err := parquet.ReadFile[th1nhng0Meta](path)
	if err != nil {
		return nil, fmt.Errorf("read th1nhng0 metadata: %w", err)
	}
	out := make(map[string]th1nhng0Meta, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}

// Relation is one edge of the official relationship graph, with both the
// label vbpl.vn published and the edge kind luatdo projects it as. The raw
// label is kept because the vocabulary is richer than the graph's two kinds
// and a later milestone may well want the rest of it.
type Relation struct {
	FromDoc string `json:"from_doc"`
	ToDoc   string `json:"to_doc"`
	Kind    string `json:"kind"` // cites or amends
	Label   string `json:"label"`
}

// relationKind is how one vbpl.vn label projects into the graph: the edge kind,
// and whether the row runs the other way round.
type relationKind struct {
	kind    string
	inbound bool
}

// relationKinds is the vbpl.vn vocabulary, all 31 labels of it. A label is the
// heading the other document appears under on this document's page, so half of
// them describe the other document as the actor and run backwards: on the page
// of the 2019 Labour Code the 2012 one appears under "Văn bản hết hiệu lực",
// the expired document, while on the 2012 page the 2019 one appears under "Văn
// bản quy định hết hiệu lực", the document that expired it. Both rows are the
// same edge, and reading one of them forwards is how the graph ends up with an
// arrow saying the old code repealed the new one.
//
// The directions here were checked against documents whose history is known
// rather than inferred from the wording alone.
var relationKinds = map[string]relationKind{
	// The document acts on the other one.
	"Thay thế":                              {kind: "amends"},
	"Bãi bỏ":                                {kind: "amends"},
	"Sửa đổi, bổ sung":                      {kind: "amends"},
	"Đính chính":                            {kind: "amends"},
	"Tạm ngưng hiệu lực":                    {kind: "amends"},
	"Đình chỉ thi hành":                     {kind: "amends"},
	"Văn bản hết hiệu lực":                  {kind: "amends"},
	"Văn bản bị hết hiệu lực 1 phần":        {kind: "amends"},
	"Văn bản được sửa đổi":                  {kind: "amends"},
	"Văn bản được bổ sung":                  {kind: "amends"},
	"Văn bản bị đình chỉ":                   {kind: "amends"},
	"Văn bản bị đình chỉ 1 phần":            {kind: "amends"},
	"Căn cứ":                                {kind: "cites"},
	"Dẫn chiếu":                             {kind: "cites"},
	"Quy định chi tiết, hướng dẫn thi hành": {kind: "cites"},
	"Hướng dẫn áp dụng":                     {kind: "cites"},
	"Giải thích":                            {kind: "cites"},
	"Công bố":                               {kind: "cites"},
	"Bản dịch":                              {kind: "cites"},
	"Hợp nhất":                              {kind: "cites"},
	"Văn bản căn cứ":                        {kind: "cites"},
	"Văn bản dẫn chiếu":                     {kind: "cites"},
	"Văn bản được HD, QĐ chi tiết":          {kind: "cites"},
	"Văn bản liên quan khác":                {kind: "cites"},

	// The other document acts on this one, so the edge runs the other way.
	"Văn bản quy định hết hiệu lực":        {kind: "amends", inbound: true},
	"Văn bản quy định hết hiệu lực 1 phần": {kind: "amends", inbound: true},
	"Văn bản sửa đổi":                      {kind: "amends", inbound: true},
	"Văn bản bổ sung":                      {kind: "amends", inbound: true},
	"Văn bản đình chỉ":                     {kind: "amends", inbound: true},
	"Văn bản đình chỉ 1 phần":              {kind: "amends", inbound: true},
	"Văn bản HD, QĐ chi tiết":              {kind: "cites", inbound: true},
}

// Th1nhng0Relations reads the official relationship graph and rewrites it in
// luatdo identifiers, one edge per pair however many rows describe it. Rows
// naming a document the metadata config does not carry, or whose official
// number has no year and so has no stable identifier, are dropped and counted
// rather than guessed at.
func Th1nhng0Relations(revisionDir string) ([]Relation, int, error) {
	meta, err := loadTh1nhng0Meta(revisionDir)
	if err != nil {
		return nil, 0, err
	}
	// A row that lost the identifier to a duplicate still maps onto it, because
	// an edge drawn from the losing row is an edge of the same document. Only a
	// row with no identity at all is dropped.
	ids := make(map[string]string, len(meta))
	for row, m := range meta {
		if m.LoaiVanBan == translationType {
			continue
		}
		if docID, err := law.DocIDIn(m.SoKyHieu, m.CoQuan); err == nil {
			ids[row] = docID
		}
	}
	var out []Relation
	dropped := 0
	seen := map[string]bool{}
	if err := eachParquetRow(th1nhng0Path(revisionDir, "relationships"), func(row th1nhng0Rel) error {
		from, okFrom := ids[row.DocID]
		to, okTo := ids[row.OtherDocID]
		if !okFrom || !okTo || from == to {
			dropped++
			return nil
		}
		// A label the vocabulary does not have is a citation and keeps its
		// direction. That is the weakest claim available, and the label rides
		// along on the edge so a revision that adds one is visible.
		projection := relationKinds[strings.TrimSpace(row.Relationship)]
		kind := projection.kind
		if kind == "" {
			kind = "cites"
		}
		if projection.inbound {
			from, to = to, from
		}
		key := from + "|" + to + "|" + kind
		if seen[key] {
			return nil
		}
		seen[key] = true
		out = append(out, Relation{FromDoc: from, ToDoc: to, Kind: kind, Label: row.Relationship})
		return nil
	}); err != nil {
		return nil, 0, err
	}
	return out, dropped, nil
}

// eachParquetRow streams a parquet file one row at a time, so a file larger
// than memory costs nothing more than a file larger than memory.
func eachParquetRow[T any](path string, visit func(T) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	reader := parquet.NewGenericReader[T](f)
	defer func() { _ = reader.Close() }()
	buf := make([]T, 64)
	for {
		n, err := reader.Read(buf)
		for i := range n {
			if verr := visit(buf[i]); verr != nil {
				return verr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read %s: %w", path, err)
		}
	}
}
