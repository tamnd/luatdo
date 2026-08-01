package parse

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/parquet-go/parquet-go"
	"github.com/tamnd/luatdo/store"
)

// utsRow mirrors one row of the UTS_VLC 2026 parquet split.
type utsRow struct {
	ID            string `parquet:"id"`
	Filename      string `parquet:"filename"`
	Title         string `parquet:"title"`
	Type          string `parquet:"type"`
	Content       string `parquet:"content"`
	ContentLength int64  `parquet:"content_length"`
}

// utsSource mirrors one entry of metadata/2026_sources.json.
type utsSource struct {
	ID      string `json:"id"`
	URLVBPL string `json:"url_vbpl"`
}

// LoadUTSVLC reads a fetched UTS_VLC revision directory into parser inputs.
func LoadUTSVLC(revisionDir, revision string) ([]Input, error) {
	rows, err := parquet.ReadFile[utsRow](filepath.Join(revisionDir, "data", "2026-00000-of-00001.parquet"))
	if err != nil {
		return nil, fmt.Errorf("read UTS_VLC parquet: %w", err)
	}

	urls := map[string]string{}
	sourcesPath := filepath.Join(revisionDir, "metadata", "2026_sources.json")
	if _, err := os.Stat(sourcesPath); err == nil {
		var sources []utsSource
		if err := store.ReadJSON(sourcesPath, &sources); err != nil {
			return nil, fmt.Errorf("read UTS_VLC sources: %w", err)
		}
		for _, s := range sources {
			urls[s.ID] = s.URLVBPL
		}
	}

	inputs := make([]Input, 0, len(rows))
	for _, r := range rows {
		inputs = append(inputs, Input{
			OfficialNumber: r.ID,
			Title:          r.Title,
			DocType:        r.Type,
			Content:        r.Content,
			Source:         "uts_vlc",
			SourceRef:      revision,
			SourceURL:      urls[r.ID],
		})
	}
	return inputs, nil
}
