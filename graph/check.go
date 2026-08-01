package graph

import (
	"context"
	"fmt"
	"sort"

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
