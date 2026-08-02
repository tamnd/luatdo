package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenExplicitDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "data")
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.Root != root {
		t.Errorf("root = %q, want %q", s.Root, root)
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("root directory not created: %v", err)
	}
}

func TestOpenFromEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LUATDO_DATA", root)
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.Root != root {
		t.Errorf("root = %q, want LUATDO_DATA %q", s.Root, root)
	}
}

func TestWriteFileReplacesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "file.txt")
	if err := WriteFile(path, []byte("first")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(path, []byte("second")); err != nil {
		t.Fatalf("WriteFile overwrite: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "second" {
		t.Errorf("content = %q, want %q", data, "second")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %d entries", len(entries))
	}
}

func TestJSONRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.json")
	in := map[string]int{"điều": 94}
	if err := WriteJSON(path, in); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var out map[string]int
	if err := ReadJSON(path, &out); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if out["điều"] != 94 {
		t.Errorf("round trip = %v", out)
	}
}

func TestHash(t *testing.T) {
	want := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if got := HashBytes([]byte("test")); got != want {
		t.Errorf("HashBytes = %q", got)
	}
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if got != want {
		t.Errorf("HashFile = %q", got)
	}
}

func TestJSONLRoundTripsAndAppends(t *testing.T) {
	type row struct {
		ID   string `json:"id"`
		N    int    `json:"n"`
		Text string `json:"text"`
	}
	path := filepath.Join(t.TempDir(), "nested", "rows.jsonl")
	if err := AppendJSONL(path, []row{{ID: "a", N: 1, Text: "một dòng"}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := AppendJSONL(path, []row{{ID: "b", N: 2, Text: "dòng nữa"}}); err != nil {
		t.Fatalf("append again: %v", err)
	}
	got, err := ReadJSONL[row](path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].Text != "dòng nữa" {
		t.Errorf("rows = %+v, an append only file keeps what was already in it", got)
	}
}

func TestReadJSONLOfAFileNobodyWroteIsNotAnError(t *testing.T) {
	got, err := ReadJSONL[struct{}](filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil || got != nil {
		t.Errorf("got %v and %v, nothing yet is an answer rather than a failure", got, err)
	}
}

func TestReadJSONLNamesTheFileItCouldNotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.jsonl")
	if err := os.WriteFile(path, []byte("{\"id\":\"a\"}\nnot json\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ReadJSONL[struct{}](path)
	if err == nil || !strings.Contains(err.Error(), "broken.jsonl") {
		t.Errorf("error = %v, a parse failure has to say which file", err)
	}
}
