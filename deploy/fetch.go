package deploy

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dataset is a published graph export.
//
// The checksum is not optional. This is a file the tool downloads over the
// network and then hands to a container that runs a shell script out of it, and
// the difference between doing that to the published corpus and doing it to
// whatever a proxy felt like returning is these sixty four characters.
type Dataset struct {
	URL    string
	SHA256 string
	// Bytes is the published size, used to report progress. A download with no
	// expected size still works, it just counts up instead of down.
	Bytes int64
}

// Fetch downloads a dataset, checks it, and unpacks it into dir.
//
// The archive is written to a temporary file and verified before anything is
// unpacked. Verifying while unpacking would be one pass instead of two, and it
// would also mean a corrupt download had already written most of a graph into
// the store by the time the checksum said so.
//
// The unpacking is done here rather than by calling tar, because tar is not on
// a stock Windows install and the standard library has been able to do this
// since before the project started. One implementation, three platforms.
func Fetch(ctx context.Context, d Dataset, dir string, progress func(done, total int64)) error {
	if d.URL == "" {
		return fmt.Errorf("no dataset URL")
	}
	if d.SHA256 == "" {
		return fmt.Errorf("no checksum for %s, refusing to unpack an archive nothing vouches for", d.URL)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "download-*.tar.gz")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	sum, err := download(ctx, d, tmp, progress)
	if err != nil {
		return err
	}
	if sum != strings.ToLower(d.SHA256) {
		return fmt.Errorf("%s has checksum %s and should have %s, so it is not the published dataset", d.URL, sum, d.SHA256)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return unpack(tmp, dir)
}

func download(ctx context.Context, d Dataset, w io.Writer, progress func(done, total int64)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return "", err
	}
	// No timeout on the client. This is half a gigabyte, and a client timeout
	// here is a rule that a slow connection is a broken one.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", d.URL, resp.Status)
	}
	total := d.Bytes
	if total == 0 {
		total = resp.ContentLength
	}

	h := sha256.New()
	c := &counter{total: total, report: progress, last: time.Now()}
	if _, err := io.Copy(io.MultiWriter(w, h, c), resp.Body); err != nil {
		return "", err
	}
	c.done()
	return hex.EncodeToString(h.Sum(nil)), nil
}

// unpack writes the archive's entries under dir.
func unpack(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := safeJoin(dir, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := writeFile(path, tr, h.FileInfo().Mode()); err != nil {
				return err
			}
		default:
			// Symlinks, devices and hard links. A graph export is directories
			// and regular files, so anything else in the archive is either a
			// mistake or somebody trying something, and both are worth stopping
			// for rather than skipping quietly.
			return fmt.Errorf("%s holds a %c entry, which a graph export does not contain", h.Name, h.Typeflag)
		}
	}
}

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// safeJoin resolves an archive entry against the destination and refuses
// anything that lands outside it.
//
// An archive is a list of paths chosen by whoever built it, and "../../../"
// entries are the oldest trick there is. The tool unpacks this into a directory
// the person running it can write to, which is the case where it matters.
func safeJoin(dir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("archive entry %q points outside the destination", name)
	}
	path := filepath.Join(dir, clean)
	// Checked again after joining, because Clean on its own does not catch
	// every shape on every platform.
	rel, err := filepath.Rel(dir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("archive entry %q points outside the destination", name)
	}
	return path, nil
}

// counter reports download progress at a readable rate.
//
// It is throttled to four times a second because the alternative writes a line
// per network read, which on a fast link is thousands of lines a second and
// costs more than the download.
type counter struct {
	done_  int64
	total  int64
	report func(done, total int64)
	last   time.Time
}

func (c *counter) Write(p []byte) (int, error) {
	c.done_ += int64(len(p))
	if c.report != nil && time.Since(c.last) > 250*time.Millisecond {
		c.last = time.Now()
		c.report(c.done_, c.total)
	}
	return len(p), nil
}

func (c *counter) done() {
	if c.report != nil {
		c.report(c.done_, c.total)
	}
}
