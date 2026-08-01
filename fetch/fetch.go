// Package fetch downloads pinned dataset revisions into the immutable raw store.
//
// A dataset is fetched at an exact revision, verified by content hash, and
// never modified afterwards. Fetching the same revision twice verifies hashes
// and does nothing else. A new revision lands in its own directory, so the
// history of what the pipeline has seen is preserved.
package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/luatdo/store"
)

// Dataset describes one upstream dataset and the files luatdo uses from it.
//
// A dataset published as several configs is fetched one config at a time,
// because they differ in size by two orders of magnitude and the small ones
// are useful long before the big one has finished downloading.
type Dataset struct {
	Name    string              // local name, the directory under raw/
	Repo    string              // Hugging Face dataset repository
	Files   []string            // repository paths to download
	Configs map[string][]string // named subsets, when the dataset has them
}

// Datasets is the registry of known sources.
var Datasets = map[string]Dataset{
	"uts_vlc": {
		Name: "uts_vlc",
		Repo: "undertheseanlp/UTS_VLC",
		Files: []string{
			"data/2026-00000-of-00001.parquet",
			"metadata/2026_sources.json",
		},
	},
	"th1nhng0": {
		Name: "th1nhng0",
		Repo: "th1nhng0/vietnamese-legal-documents",
		// metadata and relationships come first: together they are under
		// 150 MB and they already carry the document nodes and the official
		// citation graph. content is 3.5 GB of HTML and only feeds parsing.
		Configs: map[string][]string{
			"metadata":      {"data/metadata.parquet"},
			"relationships": {"data/relationships.parquet"},
			"content":       {"data/content.parquet"},
		},
	},
}

// Config names the configs of a dataset in fetch order.
func (d Dataset) Config(name string) (Dataset, error) {
	if len(d.Configs) == 0 {
		if name != "" {
			return d, fmt.Errorf("dataset %s has no configs", d.Name)
		}
		return d, nil
	}
	if name == "" {
		return d, fmt.Errorf("dataset %s needs --config, one of: %s", d.Name, strings.Join(d.ConfigNames(), ", "))
	}
	files, ok := d.Configs[name]
	if !ok {
		return d, fmt.Errorf("dataset %s has no config %q, one of: %s", d.Name, name, strings.Join(d.ConfigNames(), ", "))
	}
	d.Files = files
	return d, nil
}

// ConfigNames lists the configs in a stable order.
func (d Dataset) ConfigNames() []string {
	names := make([]string, 0, len(d.Configs))
	for name := range d.Configs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Manifest records what one fetch wrote, for verification and provenance.
type Manifest struct {
	Dataset   string            `json:"dataset"`
	Repo      string            `json:"repo"`
	Revision  string            `json:"revision"`
	FetchedAt time.Time         `json:"fetched_at"`
	Files     map[string]File   `json:"files"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// File is one verified download.
type File struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Client talks to the Hugging Face hub. BaseURL is overridable for tests.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return "https://huggingface.co"
}

func (c *Client) http() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

// Resolve returns the current commit sha of the dataset repository, so a
// fetch without an explicit revision is still pinned to an exact commit.
func (c *Client) Resolve(ctx context.Context, repo string) (string, error) {
	url := c.base() + "/api/datasets/" + repo
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", repo, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve %s: %s", repo, resp.Status)
	}
	var decoded struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode %s: %w", repo, err)
	}
	if decoded.SHA == "" {
		return "", fmt.Errorf("resolve %s: no sha in response", repo)
	}
	return decoded.SHA, nil
}

// Fetch downloads the dataset at revision into rawDir and writes a manifest.
// An empty revision resolves to the current commit. Files already present
// with a matching hash are kept.
func (c *Client) Fetch(ctx context.Context, rawDir string, ds Dataset, revision string) (*Manifest, error) {
	if len(ds.Files) == 0 {
		return nil, fmt.Errorf("dataset %s has no file list yet", ds.Name)
	}
	if revision == "" {
		sha, err := c.Resolve(ctx, ds.Repo)
		if err != nil {
			return nil, err
		}
		revision = sha
	}
	dir := filepath.Join(rawDir, ds.Name, revision)
	manifestPath := filepath.Join(dir, "manifest.json")

	manifest := &Manifest{
		Dataset:   ds.Name,
		Repo:      ds.Repo,
		Revision:  revision,
		FetchedAt: time.Now().UTC(),
		Files:     map[string]File{},
	}
	var existing Manifest
	if err := store.ReadJSON(manifestPath, &existing); err == nil {
		manifest.Files = existing.Files
	}

	for _, path := range ds.Files {
		local := filepath.Join(dir, filepath.FromSlash(path))
		if known, ok := manifest.Files[path]; ok {
			sum, err := store.HashFile(local)
			if err == nil && sum == known.SHA256 {
				continue
			}
		}
		size, sum, err := c.download(ctx, ds.Repo, revision, path, local)
		if err != nil {
			return nil, err
		}
		manifest.Files[path] = File{Size: size, SHA256: sum}
	}
	if err := store.WriteJSON(manifestPath, manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (c *Client) download(ctx context.Context, repo, revision, path, local string) (int64, string, error) {
	url := c.base() + "/datasets/" + repo + "/resolve/" + revision + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("download %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("download %s: %s", path, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return 0, "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(local), ".download*")
	if err != nil {
		return 0, "", err
	}
	name := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(name)
	}()
	size, err := io.Copy(tmp, resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("download %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return 0, "", err
	}
	if err := os.Rename(name, local); err != nil {
		return 0, "", err
	}
	sum, err := store.HashFile(local)
	if err != nil {
		return 0, "", err
	}
	return size, sum, nil
}

// Latest returns the most recently fetched revision directory of a dataset.
func Latest(rawDir, dataset string) (string, *Manifest, error) {
	base := filepath.Join(rawDir, dataset)
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", nil, fmt.Errorf("no fetched revisions of %s, run luatdo fetch %s first", dataset, dataset)
	}
	var best string
	var bestManifest *Manifest
	var bestTime time.Time
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var m Manifest
		if err := store.ReadJSON(filepath.Join(base, e.Name(), "manifest.json"), &m); err != nil {
			continue
		}
		if best == "" || m.FetchedAt.After(bestTime) {
			best, bestManifest, bestTime = filepath.Join(base, e.Name()), &m, m.FetchedAt
		}
	}
	if best == "" {
		return "", nil, fmt.Errorf("no complete fetch of %s found under %s", dataset, base)
	}
	return best, bestManifest, nil
}
