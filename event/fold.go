package event

import (
	"sort"
	"strings"
)

// Thresholds are what an act has to clear before it stops being one sentence and
// starts being something the corpus shows.
//
// The same two numbers as the relation layer, counted the same way and for the
// same reason: forty provisions of one decree are one drafter's habit, so
// documents are counted apart from provisions.
type Thresholds struct {
	MinProvisions int
	MinDocs       int
}

// DefaultThresholds is two provisions in two documents.
var DefaultThresholds = Thresholds{MinProvisions: 2, MinDocs: 2}

func (t Thresholds) floor() Thresholds {
	if t.MinProvisions < 1 {
		t.MinProvisions = 1
	}
	if t.MinDocs < 1 {
		t.MinDocs = 1
	}
	return t
}

// Fold merges every sighting of one act into one event node.
//
// Identity is the act's class and its canonical label, and nothing else, so a
// filing named in a law and the same filing named in the decree that guides it
// land on one node without anybody matching strings afterwards. That is the
// point of the layer and it is also the risk in it: an identifier this greedy
// merges two different acts the moment two drafters reach for one phrase, which
// is why the node carries every quote that fed it and why one provision is never
// enough to make it canonical.
func Fold(in []Occurrence, r *Registry, th Thresholds) []Event {
	th = th.floor()

	byID := map[string]*Event{}
	var order []string
	provisions := map[string]map[string]bool{}
	docs := map[string]map[string]bool{}
	confidence := map[string][]float64{}
	seenEvidence := map[string]map[string]bool{}
	aliases := map[string]map[string]bool{}
	parts := map[string]map[string]*Participant{}
	partOrder := map[string][]string{}

	for _, o := range in {
		if o.EventID == "" {
			continue
		}
		id := o.EventID
		agg := byID[id]
		if agg == nil {
			agg = &Event{ID: id, Class: o.Class, LabelVI: o.LabelVI}
			byID[id] = agg
			order = append(order, id)
			provisions[id] = map[string]bool{}
			docs[id] = map[string]bool{}
			seenEvidence[id] = map[string]bool{}
			aliases[id] = map[string]bool{}
			parts[id] = map[string]*Participant{}
		}
		if o.Definition != "" && agg.Definition == "" {
			agg.Definition = o.Definition
		}
		confidence[id] = append(confidence[id], o.Confidence)

		if w := strings.TrimSpace(o.AsWritten); w != "" && !strings.EqualFold(w, agg.LabelVI) {
			aliases[id][w] = true
		}

		ev := o.Evidence
		fp := ev.ProvisionID + "\x00" + ev.Quote
		if !seenEvidence[id][fp] {
			seenEvidence[id][fp] = true
			agg.Evidence = append(agg.Evidence, ev)
			provisions[id][ev.ProvisionID] = true
			if ev.DocID != "" {
				docs[id][ev.DocID] = true
			}
		}

		for _, p := range o.Participants {
			key := p.Role + "|" + p.ConceptID
			held := parts[id][key]
			if held == nil {
				clone := p
				clone.SupportCount = 0
				parts[id][key] = &clone
				held = &clone
				partOrder[id] = append(partOrder[id], key)
			}
			held.SupportCount++
			if held.LabelVI == "" {
				held.LabelVI = p.LabelVI
			}
			// The phrase kept on a merged participant is the first one read, in
			// provision order. Concatenating them would produce a label no
			// drafter wrote, and picking the longest would reward verbosity.
			if held.AsWritten == "" {
				held.AsWritten = p.AsWritten
			}
		}
	}

	out := make([]Event, 0, len(order))
	for _, id := range order {
		e := *byID[id]
		e.SupportCount = len(provisions[id])
		e.SupportDocs = len(docs[id])
		// The mean rather than the maximum, so volume cannot launder
		// uncertainty into confidence.
		e.Confidence = mean(confidence[id])
		e.Aliases = sortedKeys(aliases[id])
		for _, key := range partOrder[id] {
			e.Participants = append(e.Participants, *parts[id][key])
		}
		SortParticipants(e.Participants)
		sortEvidence(e.Evidence)
		if r != nil {
			e.OntologyVersion = r.Version
		}
		e.Status, e.Why = promoteEvent(e, r, th)
		out = append(out, e)
	}
	SortEvents(out)
	return out
}

// promoteEvent applies the gates in the order a reader wants them reported. An
// act whose class the registry does not hold stays provisional however often it
// is seen, because the class is the part nobody has agreed to yet.
func promoteEvent(e Event, r *Registry, th Thresholds) (string, string) {
	if r == nil || r.Class(e.Class) == nil {
		return StatusProvisional, WhyUnknownClass
	}
	if e.SupportCount < th.MinProvisions || e.SupportDocs < th.MinDocs {
		return StatusProvisional, WhySingleSupport
	}
	return StatusCanonical, ""
}

// FoldChains merges every sighting of one consequence edge.
//
// A chain is folded on both ends and its type, so the same claim made in two
// documents corroborates itself, and a chain read the other way round is a
// different key and stays a separate claim rather than quietly averaging with
// its opposite.
//
// Chains are kept only between events the fold produced. A chain pointing at an
// act nobody extracted cannot be walked, and keeping it would put a dangling
// edge in an exported graph where it reads as a fact.
func FoldChains(in []Chain, events []Event, r *Registry, th Thresholds) []Chain {
	th = th.floor()

	known := map[string]bool{}
	for _, e := range events {
		known[e.ID] = true
	}

	byKey := map[string]*Chain{}
	var order []string
	provisions := map[string]map[string]bool{}
	docs := map[string]map[string]bool{}
	confidence := map[string][]float64{}
	seenEvidence := map[string]map[string]bool{}

	for _, c := range in {
		if len(events) > 0 && (!known[c.FromID] || !known[c.ToID]) {
			continue
		}
		key := c.Key()
		agg := byKey[key]
		if agg == nil {
			clone := c
			clone.Evidence = nil
			clone.SupportCount, clone.SupportDocs = 0, 0
			byKey[key] = &clone
			agg = &clone
			order = append(order, key)
			provisions[key] = map[string]bool{}
			docs[key] = map[string]bool{}
			seenEvidence[key] = map[string]bool{}
		}
		agg.Direction = mergeDirection(agg.Direction, c.Direction)
		if c.OntologyVersion > agg.OntologyVersion {
			agg.OntologyVersion = c.OntologyVersion
		}
		confidence[key] = append(confidence[key], c.Confidence)

		for _, ev := range c.Evidence {
			fp := ev.ProvisionID + "\x00" + ev.Quote
			if seenEvidence[key][fp] {
				continue
			}
			seenEvidence[key][fp] = true
			agg.Evidence = append(agg.Evidence, ev)
			provisions[key][ev.ProvisionID] = true
			if ev.DocID != "" {
				docs[key][ev.DocID] = true
			}
		}
	}

	out := make([]Chain, 0, len(order))
	for _, key := range order {
		c := *byKey[key]
		c.SupportCount = len(provisions[key])
		c.SupportDocs = len(docs[key])
		c.Confidence = mean(confidence[key])
		sortEvidence(c.Evidence)
		if r != nil && c.OntologyVersion == 0 {
			c.OntologyVersion = r.Version
		}
		c.Status, c.Why = promoteChain(c, th)
		out = append(out, c)
	}
	SortChains(out)
	return out
}

// promoteChain reports direction before corroboration, because a chain the blind
// pass read the other way round is wrong in the way that matters: a consequence
// graph with good precision and bad direction walks confidently to the wrong
// consequence, and counting more provisions cannot fix that.
func promoteChain(c Chain, th Thresholds) (string, string) {
	if !ValidChain(c.Type) {
		return StatusProvisional, WhyUnknownClass
	}
	if c.Direction == DirectionFlipped || c.Direction == DirectionDisputed {
		return StatusProvisional, WhyDirectionWrong
	}
	if c.SupportCount < th.MinProvisions || c.SupportDocs < th.MinDocs {
		return StatusProvisional, WhySingleSupport
	}
	return StatusCanonical, ""
}

// mergeDirection combines verdicts across the sightings of one chain. Two
// sightings that read opposite ways are disputed rather than averaged, because
// the disagreement is the finding.
func mergeDirection(a, b string) string {
	if a == b {
		return a
	}
	if a == DirectionUnverified {
		return b
	}
	if b == DirectionUnverified {
		return a
	}
	if a == DirectionUnclear {
		return b
	}
	if b == DirectionUnclear {
		return a
	}
	return DirectionDisputed
}

func sortEvidence(in []Evidence) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].ProvisionID != in[j].ProvisionID {
			return in[i].ProvisionID < in[j].ProvisionID
		}
		return in[i].CharStart < in[j].CharStart
	})
}

func sortedKeys(in map[string]bool) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}
