package graph

import (
	"sort"
	"strconv"

	"github.com/tamnd/luatdo/event"
)

// The act layer, projected.
//
// The store has held acts, chains and norm slot links since the layer was
// built, and until this file they stopped at the disk. A consequence graph that
// only one command can walk is a consequence graph nobody has, which is the same
// thing that had happened to the relation layer and the temporal layer before
// they were projected.
//
// The label is Act and not Event. Event is taken, by the amendment operation the
// temporal layer writes, and the rule this projection follows is the one stated
// in layers.go: the name goes to whoever shipped it first. Act is also the more
// honest word for what these nodes are. They are act types a drafter named, not
// occurrences in the world, and nothing in the corpus records that a particular
// company filed a particular return on a particular day.
//
// The two layers do share the vn:event: identifier prefix, which is a wart the
// store cannot lose without rewriting files that already exist. They cannot
// collide: an amendment event identifier carries the amending document after the
// prefix, so its next segment is vn, and an act identifier carries a lowercase
// class name, so its next segment is submit or revoke. Nothing writes one
// identifier twice, which is what neo4j-admin refuses an import over.
const (
	// ActLabel is one act type the corpus regulates.
	ActLabel = "Act"
	// HasParticipant joins an act to a concept playing a role in it. The role is
	// a property rather than five relationship types, for the reason
	// ABOUT_CONCEPT gives: almost every question about a party wants the party in
	// whichever role it turns up in.
	HasParticipant = "HAS_PARTICIPANT"
	// AboutAct joins a norm to the act one of its slots names, carrying which
	// slot it was. This is the edge that makes the action slot stop being a dead
	// end, and it is also what lets a penalty be found through the statement that
	// states it rather than by matching one act label against another.
	AboutAct = "ABOUT_ACT"
)

// ActChainTypes lists the act to act relationship types, in the registry's
// order. The merge path needs the list up front because each type gets its own
// statement, the same way RelationTypes is used.
//
// None of the four is renamed. PRECEDES, TRIGGERS, PRECONDITION_OF and PRECLUDES
// are unused by every layer already in the database, which was checked before
// they were named rather than after.
func ActChainTypes() []string {
	out := make([]string, 0, len(event.ChainTypes))
	for _, c := range event.ChainTypes {
		out = append(out, c.ID)
	}
	return out
}

// actNode is one folded act as the projection carries it.
//
// The aliases and the documents ride on the node because the two questions this
// layer is asked first are what else the corpus called this act and how many
// instruments name it, and both would otherwise mean going back to the store.
// One quote and its provision ride along too, for the reason a relation edge
// carries one: a graph is for finding the claim and the store is for arguing
// with it.
type actNode struct {
	ID         string
	Class      string
	LabelVI    string
	Status     string
	Why        string
	Definition string
	Aliases    []string
	Docs       []string

	Support     int
	SupportDocs int
	Confidence  float64

	Quote           string
	ProvisionID     string
	OntologyVersion int
}

// actColumns is the CSV column order, which is also the Bolt property set.
var actColumns = []string{
	"class", "label_vi", "status", "why", "definition", "aliases", "docs",
	"support", "support_docs", "confidence", "quote", "provision_id",
	"ontology_version",
}

var actColumnTypes = map[string]string{
	"aliases": "string[]", "docs": "string[]",
	"support": "int", "support_docs": "int", "confidence": "float",
	"ontology_version": "int",
}

func (a actNode) values() []string {
	return []string{
		a.Class, a.LabelVI, a.Status, a.Why, a.Definition, join(a.Aliases), join(a.Docs),
		strconv.Itoa(a.Support), strconv.Itoa(a.SupportDocs),
		strconv.FormatFloat(a.Confidence, 'f', 2, 64), a.Quote, a.ProvisionID,
		strconv.Itoa(a.OntologyVersion),
	}
}

// fields is the Bolt form. It is built from values so a column added to one
// path cannot go missing from the other, and then the four properties that are
// not strings are put back as themselves. Bolt carries a list as a list and a
// number as a number, and the CSV path packs both into text only because that
// file format has no other way to say it.
func (a actNode) fields() map[string]any {
	out := map[string]any{"id": a.ID}
	values := a.values()
	for i, name := range actColumns {
		out[name] = values[i]
	}
	out["aliases"], out["docs"] = asAny(a.Aliases), asAny(a.Docs)
	out["support"], out["support_docs"] = a.Support, a.SupportDocs
	out["confidence"], out["ontology_version"] = a.Confidence, a.OntologyVersion
	return out
}

// chainEdge is one act to act edge.
type chainEdge struct {
	From, To, Type string
	Status         string
	Why            string
	Support        int
	SupportDocs    int
	Confidence     float64
	Direction      string
	Quote          string
	ProvisionID    string
}

// participantEdge is one concept playing one role in one act.
type participantEdge struct {
	ActID     string
	ConceptID string
	Role      string
	AsWritten string
	Support   int
}

// normActEdge is one norm slot pointing at an act.
type normActEdge struct {
	NormID      string
	ActID       string
	Slot        string
	ProvisionID string
}

// actIDs is the set of acts this export writes, which is what every edge into
// the layer has to land in.
func actIDs(in Input) map[string]bool {
	out := make(map[string]bool, len(in.Acts))
	for i := range in.Acts {
		if in.Acts[i].ID != "" {
			out[in.Acts[i].ID] = true
		}
	}
	return out
}

// eachAct visits every folded act, provisional ones included.
//
// The relation layer exports canonical edges only and this layer does not, which
// is a difference worth stating rather than leaving as an inconsistency. A
// provisional relation is a claim about the vocabulary, and the registry keeps
// it either way, so holding it back costs a reviewer nothing. A provisional
// chain is one provision's sentence about two acts, and on this corpus most
// chains are stated once, so a canonical only projection would be an empty
// consequence graph that looks like a working one. Every act and every chain
// carries its status, its support counts and a quote instead, and the three
// shipped queries print all of them, so nothing can be read as settled by
// accident.
func eachAct(in Input, visit func(actNode) error) error {
	for i := range in.Acts {
		e := &in.Acts[i]
		if e.ID == "" {
			continue
		}
		node := actNode{
			ID: e.ID, Class: e.Class, LabelVI: e.LabelVI, Status: e.Status, Why: e.Why,
			Definition: e.Definition, Aliases: e.Aliases, Docs: actDocs(e),
			Support: e.SupportCount, SupportDocs: e.SupportDocs, Confidence: e.Confidence,
			OntologyVersion: e.OntologyVersion,
		}
		if len(e.Evidence) > 0 {
			node.Quote, node.ProvisionID = e.Evidence[0].Quote, e.Evidence[0].ProvisionID
		}
		if err := visit(node); err != nil {
			return err
		}
	}
	return nil
}

// actDocs is the instruments one act was seen in, sorted and without repeats.
func actDocs(e *event.Event) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(e.Evidence))
	for _, ev := range e.Evidence {
		if ev.DocID == "" || seen[ev.DocID] {
			continue
		}
		seen[ev.DocID] = true
		out = append(out, ev.DocID)
	}
	sort.Strings(out)
	return out
}

// eachActChain visits the chains whose two ends this export wrote.
//
// A chain into an act the fold did not keep is dropped here rather than found
// later, for the importer's contract: neo4j-admin refuses an entire import over
// one relationship row naming a node it does not have.
func eachActChain(in Input, visit func(chainEdge) error) error {
	if len(in.Chains) == 0 {
		return nil
	}
	acts := actIDs(in)
	for i := range in.Chains {
		c := &in.Chains[i]
		if !acts[c.FromID] || !acts[c.ToID] {
			continue
		}
		row := chainEdge{
			From: c.FromID, To: c.ToID, Type: c.Type, Status: c.Status, Why: c.Why,
			Support: c.SupportCount, SupportDocs: c.SupportDocs,
			Confidence: c.Confidence, Direction: c.Direction,
		}
		if len(c.Evidence) > 0 {
			row.Quote, row.ProvisionID = c.Evidence[0].Quote, c.Evidence[0].ProvisionID
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return nil
}

// eachActParticipant visits every party edge whose concept the merge produced.
//
// A participant naming a concept this export did not write is skipped, and the
// count that goes missing is the honest one. The layer records a participant
// only when it resolved against the concept layer, so a skipped edge here means
// the concept layer moved under it, which is a thing that happens between
// milestones and should cost one edge rather than the whole import.
func eachActParticipant(in Input, visit func(participantEdge) error) error {
	concepts := mergedConcepts(in)
	if len(concepts) == 0 {
		return nil
	}
	for i := range in.Acts {
		e := &in.Acts[i]
		if e.ID == "" {
			continue
		}
		for _, p := range e.Participants {
			if !concepts[p.ConceptID] {
				continue
			}
			if err := visit(participantEdge{
				ActID: e.ID, ConceptID: p.ConceptID, Role: p.Role,
				AsWritten: p.AsWritten, Support: p.SupportCount,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// eachNormAct visits the norm slot links, checked against both layers.
//
// The order is fixed here rather than taken from the file, because the links
// come from as many sighting files as there are documents and two machines
// reading a directory are not promised the same order.
func eachNormAct(in Input, visit func(normActEdge) error) error {
	if len(in.NormActs) == 0 {
		return nil
	}
	acts := actIDs(in)
	norms := make(map[string]bool, len(in.Statements))
	for i := range in.Statements {
		norms[in.Statements[i].ID] = true
	}
	links := make([]event.Link, len(in.NormActs))
	copy(links, in.NormActs)
	sort.Slice(links, func(i, j int) bool {
		a, b := links[i], links[j]
		if a.StatementID != b.StatementID {
			return a.StatementID < b.StatementID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.EventID < b.EventID
	})
	for _, l := range links {
		if !acts[l.EventID] || !norms[l.StatementID] {
			continue
		}
		if err := visit(normActEdge{
			NormID: l.StatementID, ActID: l.EventID, Slot: l.Kind, ProvisionID: l.ProvisionID,
		}); err != nil {
			return err
		}
	}
	return nil
}
