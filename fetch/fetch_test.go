package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tamnd/luatdo/store"
)

func testServer(t *testing.T, downloads *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/datasets/test/repo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sha":"abc123"}`))
	})
	mux.HandleFunc("GET /datasets/test/repo/resolve/abc123/data/corpus.txt", func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		_, _ = w.Write([]byte("nội dung"))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestFetchPinsAndVerifies(t *testing.T) {
	var downloads atomic.Int64
	server := testServer(t, &downloads)
	client := &Client{BaseURL: server.URL}
	ds := Dataset{Name: "test", Repo: "test/repo", Files: []string{"data/corpus.txt"}}
	rawDir := t.TempDir()

	manifest, err := client.Fetch(context.Background(), rawDir, ds, "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if manifest.Revision != "abc123" {
		t.Errorf("revision = %q, want resolved sha", manifest.Revision)
	}
	local := filepath.Join(rawDir, "test", "abc123", "data", "corpus.txt")
	data, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("downloaded file: %v", err)
	}
	if string(data) != "nội dung" {
		t.Errorf("content = %q", data)
	}
	file := manifest.Files["data/corpus.txt"]
	if file.SHA256 != store.HashBytes(data) {
		t.Errorf("manifest hash %q does not match content", file.SHA256)
	}
	if file.Size != int64(len(data)) {
		t.Errorf("manifest size = %d, want %d", file.Size, len(data))
	}

	if _, err := client.Fetch(context.Background(), rawDir, ds, "abc123"); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if got := downloads.Load(); got != 1 {
		t.Errorf("downloads = %d, want 1, second fetch must reuse the verified file", got)
	}
}

func TestFetchRejectsEmptyFileList(t *testing.T) {
	client := &Client{BaseURL: "http://unused.invalid"}
	if _, err := client.Fetch(context.Background(), t.TempDir(), Dataset{Name: "x", Repo: "x/y"}, "r"); err == nil {
		t.Fatal("Fetch with no file list should fail")
	}
}

func TestLatest(t *testing.T) {
	var downloads atomic.Int64
	server := testServer(t, &downloads)
	client := &Client{BaseURL: server.URL}
	ds := Dataset{Name: "test", Repo: "test/repo", Files: []string{"data/corpus.txt"}}
	rawDir := t.TempDir()
	if _, err := client.Fetch(context.Background(), rawDir, ds, ""); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	dir, manifest, err := Latest(rawDir, "test")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if manifest.Revision != "abc123" {
		t.Errorf("latest revision = %q", manifest.Revision)
	}
	if dir != filepath.Join(rawDir, "test", "abc123") {
		t.Errorf("latest dir = %q", dir)
	}
	if _, _, err := Latest(rawDir, "missing"); err == nil {
		t.Error("Latest on an unfetched dataset should fail")
	}
}
