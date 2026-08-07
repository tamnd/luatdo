package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// entry is a file to put in a test archive.
type entry struct {
	name string
	body string
	typ  byte
}

func archive(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: typ}
		if typ == tar.TypeDir {
			h.Size, h.Mode = 0, 0o755
		}
		if typ == tar.TypeSymlink {
			h.Size, h.Linkname = 0, e.body
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func serve(t *testing.T, body []byte) (url, sum string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	h := sha256.Sum256(body)
	return srv.URL + "/luatdo.tar.gz", hex.EncodeToString(h[:])
}

func TestFetchUnpacksAVerifiedArchive(t *testing.T) {
	body := archive(t, []entry{
		{name: "neo4j/", typ: tar.TypeDir},
		{name: "neo4j/import.sh", body: "#!/bin/sh\n"},
		{name: "neo4j/nodes/doc.csv", body: "id:ID\n"},
	})
	url, sum := serve(t, body)
	dir := t.TempDir()

	var last int64
	if err := Fetch(context.Background(), Dataset{URL: url, SHA256: sum, Bytes: int64(len(body))}, dir, func(done, _ int64) {
		last = done
	}); err != nil {
		t.Fatal(err)
	}
	if last != int64(len(body)) {
		t.Errorf("progress finished at %d of %d bytes, so a person watching a long download never sees it reach the end", last, len(body))
	}
	if !HasExport(filepath.Join(dir, "neo4j")) {
		t.Error("the unpacked directory does not look like an export to the loader that has to read it")
	}
	// Nested paths are the ones a naive unpacker drops, because the directory
	// entry for them is not always in the archive.
	got, err := os.ReadFile(filepath.Join(dir, "neo4j", "nodes", "doc.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "id:ID\n" {
		t.Errorf("nested file unpacked as %q", got)
	}
	// The half gigabyte download is written to a temporary file next to the
	// destination, and leaving it there doubles the disk this needs on a host
	// chosen for having just enough.
	names, err := filepath.Glob(filepath.Join(dir, "download-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Errorf("the download temporary file was left behind: %v", names)
	}
}

func TestFetchRefusesAnArchiveThatDoesNotMatchItsChecksum(t *testing.T) {
	body := archive(t, []entry{{name: "neo4j/import.sh", body: "#!/bin/sh\n"}})
	url, _ := serve(t, body)
	dir := t.TempDir()

	err := Fetch(context.Background(), Dataset{URL: url, SHA256: strings.Repeat("0", 64)}, dir, nil)
	if err == nil {
		t.Fatal("a mismatched archive was accepted, and the tool runs a shell script out of this")
	}
	// Nothing unpacked. Verifying after writing the files would leave a store
	// holding most of a graph nobody vouched for.
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Errorf("a rejected download still wrote %d entries into the store", len(names))
	}
}

func TestFetchRefusesAnArchiveWithNoChecksumAtAll(t *testing.T) {
	url, _ := serve(t, archive(t, []entry{{name: "neo4j/import.sh", body: "x"}}))
	if err := Fetch(context.Background(), Dataset{URL: url}, t.TempDir(), nil); err == nil {
		t.Fatal("an archive with no checksum was unpacked, so -url with no -sha256 silently skips the check")
	}
}

func TestFetchReportsAMissingDataset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	err := Fetch(context.Background(), Dataset{URL: srv.URL, SHA256: strings.Repeat("a", 64)}, t.TempDir(), nil)
	if err == nil {
		t.Fatal("a 404 was treated as a download")
	}
	// The status has to be in the message. A checksum mismatch on the body of an
	// error page sends the reader looking for a corrupt upload.
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("the error does not say what the server answered: %v", err)
	}
}

func TestUnpackRefusesEntriesOutsideTheDestination(t *testing.T) {
	// Every one of these is refused on every platform, which is the point. The
	// rooted and drive letter forms were the ones that behaved differently:
	// "/etc/escaped.csv" is not an absolute path on Windows and was quietly
	// rewritten to a file inside the destination, so an archive that Linux
	// refused was accepted there.
	for _, name := range []string{
		"../escaped.csv",
		"neo4j/../../escaped.csv",
		"/etc/escaped.csv",
		`C:\Windows\escaped.csv`,
		`\\server\share\escaped.csv`,
		"",
	} {
		dir := t.TempDir()
		body := archive(t, []entry{{name: name, body: "x"}})
		if err := unpack(bytes.NewReader(body), dir); err == nil {
			t.Errorf("entry %q was unpacked", name)
		}
	}
}

func TestUnpackKeepsFilesThatOnlyLookLikeAnEscape(t *testing.T) {
	// A refusal built on a prefix test rejects this, and then a legitimate export
	// fails to unpack for a reason nobody can act on.
	dir := t.TempDir()
	body := archive(t, []entry{{name: "neo4j/..stray.csv", body: "x"}})
	if err := unpack(bytes.NewReader(body), dir); err != nil {
		t.Fatalf("an ordinary file was refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "neo4j", "..stray.csv")); err != nil {
		t.Error(err)
	}
}

func TestUnpackRefusesAnythingThatIsNotAFileOrDirectory(t *testing.T) {
	body := archive(t, []entry{{name: "neo4j/link", body: "/etc/passwd", typ: tar.TypeSymlink}})
	if err := unpack(bytes.NewReader(body), t.TempDir()); err == nil {
		t.Fatal("a symlink was unpacked, and a link into the host filesystem is what the importer would then read through")
	}
}

func TestHasExportWantsTheScriptRatherThanTheDirectory(t *testing.T) {
	dir := t.TempDir()
	if HasExport(dir) {
		t.Fatal("an empty directory was reported as an export")
	}
	// An interrupted export leaves the directory and some of the CSV files, and
	// loading that produces a graph missing whatever had not been written yet.
	if err := os.WriteFile(filepath.Join(dir, "doc.csv"), []byte("id:ID\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if HasExport(dir) {
		t.Fatal("a part written export was reported as complete")
	}
	if err := os.WriteFile(filepath.Join(dir, "import.sh"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasExport(dir) {
		t.Fatal("a complete export was not recognised")
	}
}

func TestFindRuntimeReportsWhatItLookedFor(t *testing.T) {
	// Nothing here assumes a runtime is installed, because CI runners and the
	// machines this has to work on both come without one.
	if _, err := FindRuntime("definitely-not-a-container-runtime"); err == nil {
		t.Fatal("a runtime that is not installed was accepted")
	}
	rt, err := FindRuntime("")
	if err != nil {
		if !strings.Contains(err.Error(), "podman") || !strings.Contains(err.Error(), "docker") {
			t.Errorf("the error does not name what a person is supposed to install: %v", err)
		}
		return
	}
	if rt.Name != "podman" && rt.Name != "docker" {
		t.Errorf("found runtime %q, which is neither of the two this looks for", rt.Name)
	}
	if rt.Path == "" {
		t.Error("the runtime was found with no path to run it by")
	}
}
