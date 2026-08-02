package concept

import (
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

// Aggregation is what the corpus does with one candidate concept, counted
// rather than judged.
//
// The counts are kept apart on purpose. Four hundred sightings inside one
// provincial decision is one drafter's habit. Four hundred sightings across
// twelve instruments and six subject domains is a concept the corpus operates
// on. A single number cannot tell those apart, so there are four.
type Aggregation struct {
	Key      string `json:"key"`
	LabelVI  string `json:"label_vi"`
	Kind     string `json:"kind"`
	Sighted  int    `json:"sighted"`
	InDocs   int    `json:"in_documents"`
	InScopes int    `json:"in_scopes"`
	InSubs   int    `json:"in_subdomains"`
	// DefinedSomewhere is true when any instrument in the corpus defines this
	// concept. It is the field competency question 6 turns on, and it is
	// computed from the definitions rather than asked of a model.
	DefinedSomewhere bool `json:"defined_somewhere"`
	// InNormSlot marks a concept that fills a norm role somewhere: a bearer, a
	// beneficiary, the object of a duty. A concept a duty is borne by matters
	// at two sightings, so this bypasses the frequency threshold.
	InNormSlot bool `json:"in_norm_slot"`
	// Surfaces are the spellings seen, most frequent first. They are kept
	// because a working definition has to be written under the label the corpus
	// actually uses, not under whichever spelling happened to sort first.
	Surfaces []string `json:"surfaces"`
	// Provisions are the sightings, in corpus order, capped. The cap is stated
	// here rather than applied silently downstream: a working definition is
	// derived from the first few of these and needs to know it saw a sample.
	Provisions []string `json:"provisions"`
	// Kinds is every kind the readings assigned, with counts. Disagreement is
	// kept rather than resolved by majority, because a phrase two readings
	// filed under two different kinds is usually two concepts.
	Kinds map[string]int `json:"kinds"`
}

// MaxProvisionsPerConcept caps the sighting list kept per candidate. A concept
// used in forty thousand provisions does not need forty thousand identifiers
// stored to be promoted, and the working definition pass reads at most a
// handful of them anyway.
const MaxProvisionsPerConcept = 200

// Aggregate groups candidate sightings deterministically. Nothing here is a
// judgement: it normalises, groups, counts, and sorts. The judgement is the
// threshold, and it lives in Promote where it can be argued with.
//
// defined is the set of folded labels the corpus defines anywhere, subdomain
// maps a document to its subject subdomains, and normSlots is the set of folded
// labels that fill a norm role. All three are optional; an empty map means the
// signal is unavailable, which is different from the signal being negative and
// is why Promote reports which rule fired.
func Aggregate(sightings []Sighting, defined map[string]bool, subdomain map[string][]string, normSlots map[string]bool) []Aggregation {
	type acc struct {
		agg      Aggregation
		docs     map[string]bool
		scopes   map[string]bool
		subs     map[string]bool
		surfaces map[string]int
	}
	byKey := map[string]*acc{}

	for i := range sightings {
		s := &sightings[i]
		for j := range s.Candidates {
			c := &s.Candidates[j]
			key := c.Key()
			if key == "" {
				continue
			}
			a := byKey[key]
			if a == nil {
				a = &acc{
					agg:      Aggregation{Key: key, Kinds: map[string]int{}},
					docs:     map[string]bool{},
					scopes:   map[string]bool{},
					subs:     map[string]bool{},
					surfaces: map[string]int{},
				}
				byKey[key] = a
			}
			a.agg.Sighted++
			a.agg.Kinds[c.Kind]++
			a.surfaces[c.LabelVI]++
			a.docs[c.DocID] = true
			a.scopes[scopeOfProvision(c.ProvisionID)] = true
			for _, sub := range subdomain[c.DocID] {
				a.subs[sub] = true
			}
			if len(a.agg.Provisions) < MaxProvisionsPerConcept {
				a.agg.Provisions = append(a.agg.Provisions, c.ProvisionID)
			}
		}
	}

	out := make([]Aggregation, 0, len(byKey))
	for key, a := range byKey {
		a.agg.InDocs = len(a.docs)
		a.agg.InScopes = len(a.scopes)
		a.agg.InSubs = len(a.subs)
		a.agg.DefinedSomewhere = defined[key]
		a.agg.InNormSlot = normSlots[key]
		a.agg.Surfaces = rank(a.surfaces)
		a.agg.LabelVI = a.agg.Surfaces[0]
		a.agg.Kind = dominant(a.agg.Kinds)
		out = append(out, a.agg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].InDocs != out[j].InDocs {
			return out[i].InDocs > out[j].InDocs
		}
		if out[i].Sighted != out[j].Sighted {
			return out[i].Sighted > out[j].Sighted
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// scopeOfProvision recovers the instrument a provision belongs to from its
// identifier. An annex carries its own scope, and a concept used all over one
// annex is still one instrument's usage.
func scopeOfProvision(provisionID string) string {
	if i := strings.Index(provisionID, ":annex-"); i >= 0 {
		if j := strings.Index(provisionID[i+1:], ":"); j >= 0 {
			return provisionID[:i+1+j]
		}
		return provisionID
	}
	// The earliest structural part wins, not the first one in the list. A
	// provision identified as chapter-2:article-3 has to cut at the chapter, and
	// checking the parts in list order would cut at the article and file every
	// chapter of one law as a separate instrument.
	cut := len(provisionID)
	for _, part := range []string{":article-", ":clause-", ":point-", ":chapter-", ":section-"} {
		if i := strings.Index(provisionID, part); i >= 0 && i < cut {
			cut = i
		}
	}
	return provisionID[:cut]
}

func rank(counts map[string]int) []string {
	out := make([]string, 0, len(counts))
	for s := range counts {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if counts[out[i]] != counts[out[j]] {
			return counts[out[i]] > counts[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

func dominant(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	return rank(counts)[0]
}

// Thresholds is the promotion rule, in one value, so it can be printed with
// the numbers it produced and changed without hunting for constants.
type Thresholds struct {
	// MinDocuments is the number of distinct documents a concept must be seen
	// in. Documents rather than sightings, because one document repeating a
	// phrase forty times is one document's opinion.
	MinDocuments int `json:"min_documents"`
	// MinScopes is the number of distinct instruments. A concept used by one
	// instrument is that instrument's vocabulary and belongs to it, not to the
	// corpus.
	MinScopes int `json:"min_scopes"`
	// MinConfidence drops candidates the reader itself was unsure about before
	// they are counted at all.
	MinConfidence float64 `json:"min_confidence"`
}

// DefaultThresholds is the starting rule. Three documents and two instruments
// is deliberately low: this pass is measured on pooled recall, a candidate that
// is never promoted is never offered to anyone, and the cost of a wrong
// promotion is one node with a low count that any query can filter out.
var DefaultThresholds = Thresholds{MinDocuments: 3, MinScopes: 2, MinConfidence: 0.5}

// Promotion is a decision to make a candidate a node, with the rule that fired.
// The rule is stored because two concepts promoted by different rules are
// different kinds of claim, and a reviewer looking at a norm slot promotion
// with two sightings should be able to see that is why it is there.
type Promotion struct {
	Key     string `json:"key"`
	LabelVI string `json:"label_vi"`
	Kind    string `json:"kind"`
	Rule    string `json:"rule"` // frequency or norm_slot
	InDocs  int    `json:"in_documents"`
	Sighted int    `json:"sighted"`
}

// The rules that can promote a candidate.
const (
	RuleFrequency = "frequency"
	RuleNormSlot  = "norm_slot"
)

// Promote applies the thresholds and returns what crosses them, in the order
// Aggregate produced, which is most used first.
//
// Concepts the corpus defines somewhere are not promoted here. They are pass
// B's output and already have a TermUse scoped to the instrument that defined
// them, and minting a second node from usage would split the concept in half.
func Promote(aggs []Aggregation, t Thresholds) []Promotion {
	var out []Promotion
	for i := range aggs {
		a := &aggs[i]
		if a.DefinedSomewhere {
			continue
		}
		switch {
		case a.InNormSlot:
			out = append(out, promotion(a, RuleNormSlot))
		case a.InDocs >= t.MinDocuments && a.InScopes >= t.MinScopes:
			out = append(out, promotion(a, RuleFrequency))
		}
	}
	return out
}

func promotion(a *Aggregation, rule string) Promotion {
	return Promotion{
		Key: a.Key, LabelVI: a.LabelVI, Kind: a.Kind, Rule: rule,
		InDocs: a.InDocs, Sighted: a.Sighted,
	}
}

// UndefinedScope is the scope identifier a promoted concept is filed under.
//
// A TermUse identity is (scope, term) and a promoted concept has no defining
// instrument, so it has no scope in the ordinary sense. Giving it one of the
// documents that used it would be a lie about where it came from, and giving it
// an empty scope would collide it with anything else that lacked one. It gets
// its own namespace instead, and the fact that it is not scoped to an
// instrument is then visible in the identifier itself.
const UndefinedScope = "vn:usage"

// PromoteToTermUses turns promotions into nodes with origin undefined_usage.
//
// The origin is the fence. Everything downstream that asks whether a term is
// defined gets a different answer for these, the norm layer refuses them as
// evidence, and the graph never lets one pass for a statutory definition.
func PromoteToTermUses(promotions []Promotion, aggs []Aggregation) []TermUse {
	byKey := map[string]*Aggregation{}
	for i := range aggs {
		byKey[aggs[i].Key] = &aggs[i]
	}
	out := make([]TermUse, 0, len(promotions))
	for _, p := range promotions {
		a := byKey[p.Key]
		if a == nil {
			continue
		}
		t := TermUse{
			ID:      TermUseID(UndefinedScope, p.LabelVI),
			LabelVI: p.LabelVI,
			ScopeID: UndefinedScope,
			Kind:    p.Kind,
			Origin:  OriginUndefinedUsage,
			Quote:   p.LabelVI,
		}
		if len(a.Provisions) > 0 {
			t.DocID = docOfProvision(a.Provisions[0])
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func docOfProvision(provisionID string) string {
	scope := scopeOfProvision(provisionID)
	if i := strings.Index(scope, ":annex-"); i >= 0 {
		return scope[:i]
	}
	return scope
}

// DefinedLabels folds every label the corpus defines into the set that
// Aggregate takes. It is built from term uses rather than from a model, so the
// answer to "is this defined anywhere" is a fact about the corpus and not a
// recollection.
func DefinedLabels(terms []TermUse) map[string]bool {
	out := map[string]bool{}
	for i := range terms {
		if terms[i].Origin == OriginUndefinedUsage {
			continue
		}
		out[law.Slug(terms[i].LabelVI)] = true
		for _, a := range terms[i].Aliases {
			out[law.Slug(a)] = true
		}
	}
	return out
}

// LabelIndex maps a slugged label to the concept it belongs to, so a layer
// outside this package can attach a concept identifier to a phrase it read
// somewhere else.
//
// Only the same relation is indexed. A term use filed as broader or narrower
// than a concept is related to it and is not it, and folding those into the
// index would make two different things answer to one identifier, which is the
// merge by string match this package exists to refuse.
//
// A slug two concepts both claim is dropped rather than decided. That is the
// DIFFERS_FROM case: one phrase, two meanings, and picking either one here
// would silently pick a reading of the law.
func LabelIndex(terms []TermUse, memberships []Membership) map[string]string {
	byID := make(map[string]*TermUse, len(terms))
	for i := range terms {
		byID[terms[i].ID] = &terms[i]
	}
	out, ambiguous := map[string]string{}, map[string]bool{}
	claim := func(label, conceptID string) {
		slug := law.Slug(label)
		if slug == "" || ambiguous[slug] {
			return
		}
		if seen, ok := out[slug]; ok && seen != conceptID {
			delete(out, slug)
			ambiguous[slug] = true
			return
		}
		out[slug] = conceptID
	}
	for _, m := range memberships {
		t := byID[m.TermUseID]
		if t == nil || m.Relation != RelationSame {
			continue
		}
		claim(t.LabelVI, m.ConceptID)
		for _, a := range t.Aliases {
			claim(a, m.ConceptID)
		}
	}
	return out
}
