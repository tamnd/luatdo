package event

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// The event layer is stored the way the relation layer is: one file of raw
// sightings per document, plus derived files rewritten whole.
//
// Per document because extraction is a long job against a metered service, and a
// document that fails has to leave no artifact, which is what puts it back in
// the queue next time. Derived files rewritten whole because folding is cheap
// and a stale fold is worse than no fold, since it looks like a result somebody
// computed.

// File names inside the event directory.
const (
	SightingPrefix = "ev_"
	// EventsFile holds the folded act nodes.
	EventsFile = "events.jsonl"
	// ChainsFile holds the folded act to act edges.
	ChainsFile = "chains.jsonl"
	// LinksFile holds the norm slots that point at an act.
	LinksFile = "links.jsonl"
	// ProposalsFile holds the act classes the model invented, with their
	// definitions and their evidence. It is the review queue.
	ProposalsFile = "proposals.jsonl"
	// RegistryFile pins the class vocabulary the run cited.
	RegistryFile = "registry.json"
	SummaryFile  = "summary.json"
)

// Sighting is one document's raw read, kept whole so a fold can be recomputed
// without calling the model again.
type Sighting struct {
	DocID       string       `json:"doc_id"`
	Occurrences []Occurrence `json:"occurrences,omitempty"`
	Chains      []Chain      `json:"chains,omitempty"`
	Links       []Link       `json:"links,omitempty"`
}

// SightingPath is where one document's raw read lives.
func SightingPath(dir, docID string) string {
	return filepath.Join(dir, SightingPrefix+law.FileName(docID))
}

// WriteSighting replaces one document's raw read. A document read and found to
// name no act writes a file saying so, rather than no file, because deleting it
// would put the document back in the queue forever.
func WriteSighting(dir string, s Sighting) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(SightingPath(dir, s.DocID), append(data, '\n'), 0o644)
}

// ReadSighting returns one document's raw read. A document nobody read is not an
// error, and comes back as an empty sighting.
func ReadSighting(dir, docID string) (Sighting, error) {
	data, err := os.ReadFile(SightingPath(dir, docID))
	if err != nil {
		if os.IsNotExist(err) {
			return Sighting{DocID: docID}, nil
		}
		return Sighting{}, err
	}
	var s Sighting
	if err := json.Unmarshal(data, &s); err != nil {
		return Sighting{}, fmt.Errorf("read %s: %w", SightingPath(dir, docID), err)
	}
	return s, nil
}

// EachSighting visits every document's raw read, in file name order so a fold is
// the same on two machines. It reads only sighting files, because one directory
// holds the sightings, the folded layer and the review queue, and a derived file
// unmarshalled as a sighting would be read as silence rather than as an error.
func EachSighting(dir string, visit func(s Sighting) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), SightingPrefix) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		var s Sighting
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := visit(s); err != nil {
			return err
		}
	}
	return nil
}

// WriteEvents replaces the folded act nodes.
func WriteEvents(dir string, in []Event) error { return replaceJSONL(dir, EventsFile, in) }

// ReadEvents returns the folded act nodes.
func ReadEvents(dir string) ([]Event, error) { return readJSONL[Event](filepath.Join(dir, EventsFile)) }

// WriteChains replaces the folded act to act edges.
func WriteChains(dir string, in []Chain) error { return replaceJSONL(dir, ChainsFile, in) }

// ReadChains returns the folded act to act edges.
func ReadChains(dir string) ([]Chain, error) { return readJSONL[Chain](filepath.Join(dir, ChainsFile)) }

// WriteLinks replaces the norm to act links.
func WriteLinks(dir string, in []Link) error { return replaceJSONL(dir, LinksFile, in) }

// ReadLinks returns the norm to act links.
func ReadLinks(dir string) ([]Link, error) { return readJSONL[Link](filepath.Join(dir, LinksFile)) }

// WriteProposals replaces the review queue.
func WriteProposals(dir string, in []Proposal) error { return replaceJSONL(dir, ProposalsFile, in) }

// ReadProposals returns the review queue.
func ReadProposals(dir string) ([]Proposal, error) {
	return readJSONL[Proposal](filepath.Join(dir, ProposalsFile))
}

// WriteRegistry pins the class vocabulary a run cited, so a layer built last
// month can still say which definitions its canonical events were matched
// against.
func WriteRegistry(dir string, r *Registry) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, RegistryFile), append(data, '\n'), 0o644)
}

// ReadRegistry loads the pinned vocabulary, falling back to the seed when a
// store has never run the pass.
func ReadRegistry(dir string) (*Registry, error) {
	data, err := os.ReadFile(filepath.Join(dir, RegistryFile))
	if err != nil {
		if os.IsNotExist(err) {
			return SeedRegistry(1), nil
		}
		return nil, err
	}
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("read %s: %w", RegistryFile, err)
	}
	return &r, nil
}

// Proposal is one act class the model invented, folded across every provision
// that used it. It is the tail of the distribution and the reason the registry
// can stay small: an act type Vietnamese statutes really do name shows up here
// with a count and a definition, and gets into the registry because somebody
// read it rather than because a model was confident.
type Proposal struct {
	Class      string `json:"class"`
	Definition string `json:"definition"`
	// AsWritten collects the wordings, deduplicated and sorted, so a reviewer
	// can see the spread the class was invented over.
	AsWritten []string   `json:"as_written,omitempty"`
	Labels    []string   `json:"labels,omitempty"`
	Instances int        `json:"instances"`
	Docs      int        `json:"docs"`
	Examples  []Evidence `json:"examples,omitempty"`
}

// MaxExamples is how many quotes a proposal carries into the review queue. A
// reviewer reads a handful, and a file holding all ninety is a file nobody
// opens.
const MaxExamples = 5

// Propose folds the events whose class the registry does not hold into one
// proposal per class.
func Propose(in []Event, r *Registry) []Proposal {
	byClass := map[string]*Proposal{}
	docs := map[string]map[string]bool{}
	written := map[string]map[string]bool{}
	labels := map[string]map[string]bool{}

	for _, e := range in {
		if r != nil && r.Class(e.Class) != nil {
			continue
		}
		p := byClass[e.Class]
		if p == nil {
			p = &Proposal{Class: e.Class, Definition: e.Definition}
			byClass[e.Class] = p
			docs[e.Class] = map[string]bool{}
			written[e.Class] = map[string]bool{}
			labels[e.Class] = map[string]bool{}
		}
		if p.Definition == "" {
			p.Definition = e.Definition
		}
		labels[e.Class][e.LabelVI] = true
		for _, ev := range e.Evidence {
			p.Instances++
			if ev.DocID != "" {
				docs[e.Class][ev.DocID] = true
			}
			if w := strings.TrimSpace(ev.AsWritten); w != "" {
				written[e.Class][w] = true
			}
			if len(p.Examples) < MaxExamples {
				p.Examples = append(p.Examples, ev)
			}
		}
	}

	out := make([]Proposal, 0, len(byClass))
	for class, p := range byClass {
		p.Docs = len(docs[class])
		p.AsWritten = sortedKeys(written[class])
		p.Labels = sortedKeys(labels[class])
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Instances != out[j].Instances {
			return out[i].Instances > out[j].Instances
		}
		return out[i].Class < out[j].Class
	})
	return out
}

// Counts is what the layer holds, in the shape a person checks a run with.
type Counts struct {
	Events            int            `json:"events"`
	EventsCanonical   int            `json:"events_canonical"`
	EventsProvisional int            `json:"events_provisional"`
	UnknownClasses    int            `json:"unknown_classes"`
	Participants      int            `json:"participants"`
	Chains            int            `json:"chains"`
	ChainsCanonical   int            `json:"chains_canonical"`
	Links             int            `json:"links"`
	ActionLinks       int            `json:"action_links"`
	SanctionLinks     int            `json:"sanction_links"`
	ByClass           map[string]int `json:"by_class,omitempty"`
	ByChainType       map[string]int `json:"by_chain_type,omitempty"`
	ByRole            map[string]int `json:"by_role,omitempty"`
}

// Tally summarises a folded layer.
func Tally(events []Event, chains []Chain, links []Link, r *Registry) Counts {
	c := Counts{
		ByClass:     map[string]int{},
		ByChainType: map[string]int{},
		ByRole:      map[string]int{},
	}
	classes := map[string]bool{}
	for _, e := range events {
		c.Events++
		c.ByClass[e.Class]++
		if r != nil && r.Class(e.Class) == nil && !classes[e.Class] {
			classes[e.Class] = true
			c.UnknownClasses++
		}
		if e.Status == StatusCanonical {
			c.EventsCanonical++
		} else {
			c.EventsProvisional++
		}
		for _, p := range e.Participants {
			c.Participants++
			c.ByRole[p.Role]++
		}
	}
	for _, ch := range chains {
		c.Chains++
		c.ByChainType[ch.Type]++
		if ch.Status == StatusCanonical {
			c.ChainsCanonical++
		}
	}
	for _, l := range links {
		c.Links++
		switch l.Kind {
		case LinkAction:
			c.ActionLinks++
		case LinkSanction:
			c.SanctionLinks++
		}
	}
	return c
}

// String prints the counts with the denominators a reader needs.
func (c Counts) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "events         %d, %d canonical, %d provisional, %d invented classes\n",
		c.Events, c.EventsCanonical, c.EventsProvisional, c.UnknownClasses)
	fmt.Fprintf(&b, "participants   %d\n", c.Participants)
	fmt.Fprintf(&b, "chains         %d, %d canonical\n", c.Chains, c.ChainsCanonical)
	fmt.Fprintf(&b, "norm links     %d, %d actions, %d sanctions\n", c.Links, c.ActionLinks, c.SanctionLinks)
	for _, class := range sortedCounts(c.ByClass) {
		fmt.Fprintf(&b, "  %-14s %d\n", class, c.ByClass[class])
	}
	return b.String()
}

func sortedCounts(in map[string]int) []string {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if in[keys[i]] != in[keys[j]] {
			return in[keys[i]] > in[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

// Summary is what one run produced.
type Summary struct {
	Documents  int            `json:"documents"`
	Provisions int            `json:"provisions_read"`
	Counts     Counts         `json:"counts"`
	Direction  DirectionScore `json:"direction"`
}

// WriteSummary records the run.
func WriteSummary(dir string, s Summary) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, SummaryFile), append(data, '\n'), 0o644)
}

// ReadSummary returns the last run's numbers, or nil.
func ReadSummary(dir string) (*Summary, error) {
	data, err := os.ReadFile(filepath.Join(dir, SummaryFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s Summary
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("read %s: %w", SummaryFile, err)
	}
	return &s, nil
}

// replaceJSONL rewrites a derived file whole, and removes it when there is
// nothing to write. An empty derived file sitting on disk pretends to be a
// result somebody computed.
func replaceJSONL[T any](dir, name string, rows []T) error {
	path := filepath.Join(dir, name)
	if len(rows) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row T
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		out = append(out, row)
	}
	return out, scanner.Err()
}
