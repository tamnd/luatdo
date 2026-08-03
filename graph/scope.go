package graph

import (
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/relation"
	"github.com/tamnd/luatdo/temporal"
)

// Restrict cuts a projection down to a set of documents.
//
// The reason to have this is that the whole corpus is not the unit anybody
// works with. A person asking about labour law wants the labour campaign, and a
// dump of everything is both slower to load and harder to read a query result
// out of. A campaign scoped dump is small enough to hand somebody on a memory
// stick, and the queries that come with it are the same queries.
//
// The rule throughout is that a thing survives when everything it points at
// survives. An edge with one end outside the dump is not a smaller edge, it is
// a row naming a node that does not exist, and neo4j-admin refuses an entire
// import over one of those. So the cut cascades: drop a document, drop its
// components, drop the citations into and out of them, drop the term uses
// scoped to it, drop the concepts nothing in the dump reaches any more, and drop
// the relation edges between concepts that are now gone.
//
// Registry and Vocabulary are not cut. They are the designed vocabulary rather
// than corpus data, they are small, and a dump whose class hierarchy had holes
// in it wherever a campaign happened not to use a class would be a worse
// artefact than one carrying a few unused classes.
func Restrict(in Input, keep map[string]bool) Input {
	out := in

	components := map[string]bool{}
	out.Docs = nil
	for _, d := range in.Docs {
		if !keep[d.ID] {
			continue
		}
		out.Docs = append(out.Docs, d)
		for _, p := range d.Provisions {
			components[p.ID] = true
		}
	}

	out.Links = nil
	for _, l := range in.Links {
		// Both ends, and the provision the citation was found in. A link whose
		// source provision is outside the dump has nothing to hang off.
		if keep[l.FromDoc] && keep[l.ToDoc] && (l.FromProvision == "" || components[l.FromProvision]) {
			out.Links = append(out.Links, l)
		}
	}

	out.Definitions = nil
	for _, d := range in.Definitions {
		if inDump(d.DocID, d.ProvisionID, keep, components) {
			out.Definitions = append(out.Definitions, d)
		}
	}

	out.Mentions = nil
	for _, m := range in.Mentions {
		if inDump(m.DocID, m.ProvisionID, keep, components) {
			out.Mentions = append(out.Mentions, m)
		}
	}

	out.Statements = nil
	for _, r := range in.Statements {
		if inDump(r.DocID, r.ProvisionID, keep, components) {
			out.Statements = append(out.Statements, r)
		}
	}

	out.Subjects = nil
	for _, r := range in.Subjects {
		if keep[r.DocID] {
			out.Subjects = append(out.Subjects, r)
		}
	}

	// A conflict needs both of its norms. Keeping one whose other half is
	// outside the dump would put a pair in the graph that a reader can only see
	// one side of, which is worse than not shipping it.
	out.Conflicts = nil
	for _, f := range in.Conflicts {
		if f.A == nil || f.B == nil {
			continue
		}
		if inDump(f.A.DocID, f.A.ProvisionID, keep, components) && inDump(f.B.DocID, f.B.ProvisionID, keep, components) {
			out.Conflicts = append(out.Conflicts, f)
		}
	}

	out.Layer, out.Relations = restrictConcepts(in, out.Statements, keep)
	out.Temporal = restrictTemporal(in.Temporal, keep)
	return out
}

// inDump answers whether a record belongs to a document in the dump.
//
// It asks the document identifier first and falls back to the component,
// because not every record in the store carries a document identifier. The two
// agree wherever both are present, and the component set is the one the export
// actually writes nodes for, so the fallback is the stricter of the two.
func inDump(docID, componentID string, keep, components map[string]bool) bool {
	if docID != "" {
		return keep[docID]
	}
	return components[componentID]
}

// restrictConcepts cuts the concept layer and the relation edges to what the
// remaining documents still reach.
//
// A concept survives when a surviving term use was merged into it or a
// surviving norm is about it. Nothing else keeps one: a concept whose every
// mention was in a document the campaign excluded is a node with no edges, and
// a dump full of those is a dump whose concept count means nothing.
func restrictConcepts(in Input, statements []norm.Record, keep map[string]bool) (*concept.Layer, []relation.Edge) {
	if in.Layer == nil {
		return nil, nil
	}
	layer := &concept.Layer{}
	uses := map[string]bool{}
	for _, t := range in.Layer.TermUses {
		if !keep[t.DocID] {
			continue
		}
		layer.TermUses = append(layer.TermUses, t)
		uses[t.ID] = true
	}

	reached := map[string]bool{}
	for _, m := range in.Layer.Memberships {
		if !uses[m.TermUseID] {
			continue
		}
		layer.Memberships = append(layer.Memberships, m)
		reached[m.ConceptID] = true
	}
	for _, r := range statements {
		s := r.Statement
		for _, ref := range []*norm.Ref{s.Bearer, s.Counterparty, &s.Action, s.Object} {
			if ref != nil && ref.ConceptID != "" {
				reached[ref.ConceptID] = true
			}
		}
	}
	for _, c := range in.Layer.Concepts {
		if reached[c.ID] {
			layer.Concepts = append(layer.Concepts, c)
		}
	}
	// A difference is a statement about two term uses at once, and half of one is
	// not a weaker claim, it is a different claim nobody made.
	for _, d := range in.Layer.Differences {
		if uses[d.FromID] && uses[d.ToID] {
			layer.Differences = append(layer.Differences, d)
		}
	}

	var edges []relation.Edge
	for _, e := range in.Relations {
		if reached[e.FromID] && reached[e.ToID] {
			edges = append(edges, e)
		}
	}
	return layer, edges
}

// restrictTemporal cuts the version graph to the remaining documents.
//
// An event survives when it still opens or closes a version that is in the
// dump. An amendment whose every effect landed on a document the campaign
// excluded did not happen as far as this dump is concerned, and an event node
// with no edges tells a reader nothing except that the dump is partial, which
// the document count already told them.
func restrictTemporal(in *temporal.Layer, keep map[string]bool) *temporal.Layer {
	if in == nil {
		return nil
	}
	out := &temporal.Layer{}
	versions := map[string]bool{}
	for _, v := range in.Versions {
		if keep[v.DocID] {
			versions[v.ID] = true
		}
	}
	for _, v := range in.Versions {
		if !versions[v.ID] {
			continue
		}
		v.Children = surviving(v.Children, versions)
		out.Versions = append(out.Versions, v)
	}
	for _, e := range in.Events {
		produces := surviving(e.Produces, versions)
		terminates := surviving(e.Terminates, versions)
		if len(produces) == 0 && len(terminates) == 0 {
			continue
		}
		e.Produces, e.Terminates = produces, terminates
		out.Events = append(out.Events, e)
	}
	return out
}

func surviving(ids []string, keep map[string]bool) []string {
	var out []string
	for _, id := range ids {
		if keep[id] {
			out = append(out, id)
		}
	}
	return out
}
