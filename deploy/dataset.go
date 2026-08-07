package deploy

import "os"

// The published graph.
//
// The dataset is versioned and released apart from the code. A corpus grows
// when somebody runs the pipeline over more of it, and the tool changes when
// somebody changes the tool, and tying the two together would mean either
// re-uploading half a gigabyte to publish a one line fix or shipping a tag whose
// data is a copy of the last one. So the code carries the version of the data it
// was built against, and says so.
//
// Hugging Face rather than GitHub releases because this is a dataset. It is
// what the hub is for, the corpus is findable there by people who would never
// look in a Go repository's release page, and the files are served over plain
// HTTPS with no token for a public repository, which is the whole of what Fetch
// needs.
const (
	DatasetVersion = "2026.08.1"
	DatasetRepo    = "open-index/luatdo-graph"
	DatasetURL     = "https://huggingface.co/datasets/" + DatasetRepo + "/resolve/main/luatdo-graph-" + DatasetVersion + ".tar.gz"
	DatasetSHA256  = "41aa424e3e1e805cee634d92119b4e9abe81ff34e976d2f9f9fbbfea194135cf"
	DatasetBytes   = 576243905
)

// PublishedDataset is what luatdo neo4j fetch downloads when told nothing else.
//
// The URL is overridable through the environment as well as by flag, because
// the people most likely to need a different one are running this under a
// scheduler with no command line to edit: a mirror inside a network that cannot
// reach the hub, or a copy of the corpus somebody produced themselves.
func PublishedDataset() Dataset {
	d := Dataset{URL: DatasetURL, SHA256: DatasetSHA256, Bytes: DatasetBytes}
	if u := os.Getenv("LUATDO_DATASET_URL"); u != "" {
		d.URL = u
		// A mirror of a different file needs its own checksum, and carrying the
		// published one over would either reject the mirror or, worse, be set
		// to something that happens to match nothing and never checked.
		d.SHA256 = os.Getenv("LUATDO_DATASET_SHA256")
		d.Bytes = 0
	}
	return d
}
