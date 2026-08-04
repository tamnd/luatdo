package graph

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/tamnd/luatdo/law"
)

// counters are the projection's countable parts, paired with the query that
// counts them in a live database. Unresolved citations are deliberately absent:
// they are a fact about the store, not a thing the graph holds.
var counters = []struct {
	name  string
	query string
	want  func(Summary) int
}{
	{"documents", "MATCH (d:Document) RETURN count(d) AS n", func(s Summary) int { return s.Documents }},
	{"components", "MATCH (c:Component) RETURN count(c) AS n", func(s Summary) int { return s.Components }},
	// The alias label is counted separately and expected to match the component
	// count exactly. That is the whole promise of shipping it: a query written
	// against the earlier projection sees the same nodes. When the alias is
	// dropped this counter goes with it, and until then a mismatch means a
	// partial merge left some nodes with one label and some with both.
	{"provisions", "MATCH (p:" + law.ProvisionAlias + ") RETURN count(p) AS n", func(s Summary) int { return s.Components }},
	{"text_versions", "MATCH (v:TextVersion) RETURN count(v) AS n", func(s Summary) int { return s.TextVersions }},
	{"terms", "MATCH (t:Term) RETURN count(t) AS n", func(s Summary) int { return s.Terms }},
	{"concepts", "MATCH (c:LegalConcept) RETURN count(c) AS n", func(s Summary) int { return s.Concepts }},
	{"subjects", "MATCH (s:Subject) RETURN count(s) AS n", func(s Summary) int { return s.Subjects }},
	{"norms", "MATCH (n:Norm) RETURN count(n) AS n", func(s Summary) int { return s.Norms }},
	// The norm edges are counted because the merge matches both endpoints by
	// label, and a label that is right for the fixture and wrong for the corpus
	// writes the nodes, finds nothing to attach them to, and reports success. A
	// count of the nodes alone cannot tell that apart from a graph that works.
	{"norm_edges", "MATCH ()-[r:HAS_NORM|HAS_LEGAL_BASIS|HAS_BEARER|HAS_COUNTERPARTY|HAS_OBJECT|HAS_CONDITION|HAS_EXCEPTION|HAS_SANCTION]->() RETURN count(r) AS n", func(s Summary) int { return s.NormEdges }},
	{"contains", "MATCH ()-[r:CONTAINS]->() RETURN count(r) AS n", func(s Summary) int { return s.Contains }},
	{"has_version", "MATCH ()-[r:HAS_VERSION]->() RETURN count(r) AS n", func(s Summary) int { return s.TextVersions }},
	{"cites", "MATCH ()-[r:CITES|AMENDS]->() RETURN count(r) AS n", func(s Summary) int { return s.Cites }},
	{"defines", "MATCH ()-[r:DEFINES]->() RETURN count(r) AS n", func(s Summary) int { return s.Defines }},
	{"mentions", "MATCH ()-[r:MENTIONS]->() RETURN count(r) AS n", func(s Summary) int { return s.Mentions }},
	{"about_subject", "MATCH ()-[r:ABOUT_SUBJECT]->() RETURN count(r) AS n", func(s Summary) int { return s.AboutSubject }},
	{"term_uses", "MATCH (t:TermUse) RETURN count(t) AS n", func(s Summary) int { return s.TermUses }},
	{"merged_concepts", "MATCH (c:Concept) RETURN count(c) AS n", func(s Summary) int { return s.MergedConcepts }},
	{"instance_of", "MATCH ()-[r:INSTANCE_OF]->() RETURN count(r) AS n", func(s Summary) int { return s.InstanceOf }},
	// A difference is asymmetric in the graph and symmetric in life. The store
	// records it once, in the order the reviewer was asked, so the database is
	// expected to hold exactly one edge per decision rather than a pair.
	{"differs_from", "MATCH ()-[r:DIFFERS_FROM]->() RETURN count(r) AS n", func(s Summary) int { return s.DiffersFrom }},
	{"term_use_edges", "MATCH ()-[r:DEFINES_TERM|IN_SCOPE|REFERS_TO]->() RETURN count(r) AS n", func(s Summary) int { return s.TermUseEdges }},
	// The relation types are listed rather than matched with a wildcard, because
	// a wildcard would also count the layer's edges under a name it does not own
	// and report agreement while the rename that keeps them apart had silently
	// stopped being applied.
	{"relations", "MATCH ()-[r:" + strings.Join(RelationTypes(), "|") + "]->() RETURN count(r) AS n", func(s Summary) int { return s.Relations }},
	{"about_concept", "MATCH ()-[r:" + AboutConcept + "]->() RETURN count(r) AS n", func(s Summary) int { return s.AboutConcept }},
	{"events", "MATCH (e:" + EventLabel + ") RETURN count(e) AS n", func(s Summary) int { return s.Events }},
	{"temporal_versions", "MATCH (v:" + TemporalVersionLabel + ") RETURN count(v) AS n", func(s Summary) int { return s.TemporalVersions }},
	{"temporal_edges", "MATCH ()-[r:" + HasTemporalVersion + "|" + Includes + "|" + CausedBy + "|" + Terminates + "|" + ProducesVersion + "]->() RETURN count(r) AS n", func(s Summary) int { return s.TemporalEdges }},
	{"acts", "MATCH (a:" + ActLabel + ") RETURN count(a) AS n", func(s Summary) int { return s.Acts }},
	// The chain types are listed for the same reason the relation types are, and
	// the participant edges are counted apart from them because the two fail
	// differently: a chain goes missing when an act does, and a participant goes
	// missing when the concept layer moves under it.
	{"act_chains", "MATCH ()-[r:" + strings.Join(ActChainTypes(), "|") + "]->() RETURN count(r) AS n", func(s Summary) int { return s.ActChains }},
	{"act_participants", "MATCH ()-[r:" + HasParticipant + "]->() RETURN count(r) AS n", func(s Summary) int { return s.ActParticipants }},
	{"norm_acts", "MATCH ()-[r:" + AboutAct + "]->() RETURN count(r) AS n", func(s Summary) int { return s.NormActs }},
}

// Live counts what the database currently holds.
func Live(ctx context.Context, target Target) (map[string]int, error) {
	driver, err := neo4j.NewDriverWithContext(target.URI, neo4j.BasicAuth(target.User, target.Password, ""))
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", target.URI, err)
	}
	defer func() { _ = driver.Close(ctx) }()
	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: target.Database})
	defer func() { _ = session.Close(ctx) }()

	got := map[string]int{}
	for _, c := range counters {
		value, err := neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) (int64, error) {
			result, err := tx.Run(ctx, c.query, nil)
			if err != nil {
				return 0, err
			}
			record, err := result.Single(ctx)
			if err != nil {
				return 0, err
			}
			n, _ := record.Get("n")
			count, ok := n.(int64)
			if !ok {
				return 0, fmt.Errorf("count %s returned %T", c.name, n)
			}
			return count, nil
		})
		if err != nil {
			return nil, fmt.Errorf("count %s: %w", c.name, err)
		}
		got[c.name] = int(value)
	}
	return got, nil
}

// Drift compares the store's projection against the live counts and reports
// what does not match, one line per counter, sorted so two runs read the same.
//
// The check answers one question: is the database the projection of the store
// it claims to be? A live count that is higher means the database still holds
// something the store dropped, which a merge alone will never clean up, so the
// answer to drift is a full reimport rather than another merge.
func Drift(want Summary, got map[string]int) []string {
	var out []string
	for _, c := range counters {
		expected := c.want(want)
		actual, present := got[c.name]
		switch {
		case !present:
			out = append(out, fmt.Sprintf("%s: not counted", c.name))
		case actual != expected:
			out = append(out, fmt.Sprintf("%s: store %d, database %d, difference %+d", c.name, expected, actual, actual-expected))
		}
	}
	sort.Strings(out)
	return out
}
