// Package subject is the subject layer: a shallow hierarchy of legal domains
// and the assignment of documents to them.
//
// The vocabulary has two uses and no others. It is how a reader navigates a
// corpus of a hundred and twenty thousand documents, and it is what a sampler
// stratifies over so a campaign that reads ten thousand provisions reads them
// from the whole corpus rather than from whichever ministry writes the most.
// It is deliberately not an ontology. Nothing downstream reasons over it, no
// concept inherits from it, and a document sitting under the wrong domain
// costs a reader one wrong turn rather than a wrong answer.
//
// Assignment here is lexical: the vocabulary carries hand written cues and a
// document matches the ones that appear in its title, its type and its issuing
// body. That is weaker than the distilled classifier the design calls for, and
// the difference is stated rather than hidden. See Classify.
package subject

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/law"
)

//go:embed vocabulary.json
var vocabularyJSON []byte

// Subject is one domain or subdomain. A subject with no parent is a domain.
type Subject struct {
	ID      string   `json:"id"`
	Parent  string   `json:"parent,omitempty"`
	LabelVI string   `json:"label_vi"`
	LabelEN string   `json:"label_en"`
	Cues    []string `json:"cues,omitempty"`

	// folded is the matchable form of Cues, built once at load.
	folded []cue
}

// IsDomain reports whether the subject is at the top of the hierarchy.
func (s *Subject) IsDomain() bool { return s.Parent == "" }

// cue is one hand written phrase in the form the matcher needs: folded to
// ASCII, lowercased, space delimited on both sides so it can only match whole
// words, and carrying its own length so a six word phrase outweighs a two word
// one without the matcher having to count anything at match time.
type cue struct {
	text  string
	words int
}

// Vocabulary is the loaded subject hierarchy.
type Vocabulary struct {
	Version  string     `json:"version"`
	Note     string     `json:"note"`
	Subjects []*Subject `json:"subjects"`

	byID map[string]*Subject
}

// Load returns the embedded vocabulary. It is parsed on every call rather than
// cached, because it is a hundred and sixty odd records and every caller wants
// its own copy far less often than it wants to be sure nobody else has mutated
// the one it is reading.
func Load() (*Vocabulary, error) {
	var v Vocabulary
	if err := json.Unmarshal(vocabularyJSON, &v); err != nil {
		return nil, fmt.Errorf("parse subject vocabulary: %w", err)
	}
	v.byID = make(map[string]*Subject, len(v.Subjects))
	for _, s := range v.Subjects {
		if s.ID == "" {
			return nil, fmt.Errorf("subject with empty identifier")
		}
		if _, dup := v.byID[s.ID]; dup {
			return nil, fmt.Errorf("duplicate subject %s", s.ID)
		}
		v.byID[s.ID] = s
	}
	for _, s := range v.Subjects {
		if s.Parent != "" && v.byID[s.Parent] == nil {
			return nil, fmt.Errorf("subject %s has unknown parent %s", s.ID, s.Parent)
		}
		if v.byID[s.Parent] != nil && !v.byID[s.Parent].IsDomain() {
			return nil, fmt.Errorf("subject %s hangs off %s, which is not a domain", s.ID, s.Parent)
		}
		s.folded = foldCues(s.Cues)
	}
	return &v, nil
}

// MustLoad is Load for callers that cannot proceed without the vocabulary. The
// file is embedded and validated by a test, so a failure here is a build that
// should never have shipped rather than a condition worth handling.
func MustLoad() *Vocabulary {
	v, err := Load()
	if err != nil {
		panic(err)
	}
	return v
}

// Get returns a subject by identifier.
func (v *Vocabulary) Get(id string) *Subject { return v.byID[id] }

// Domains returns the top level subjects in vocabulary order.
func (v *Vocabulary) Domains() []*Subject {
	var out []*Subject
	for _, s := range v.Subjects {
		if s.IsDomain() {
			out = append(out, s)
		}
	}
	return out
}

// Children returns the subdomains of a domain, in vocabulary order.
func (v *Vocabulary) Children(id string) []*Subject {
	var out []*Subject
	for _, s := range v.Subjects {
		if s.Parent == id {
			out = append(out, s)
		}
	}
	return out
}

func foldCues(raw []string) []cue {
	out := make([]cue, 0, len(raw))
	seen := map[string]bool{}
	for _, c := range raw {
		text := fold(c)
		if text == " " || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, cue{text: text, words: len(strings.Fields(text))})
	}
	return out
}

// fold turns a phrase into the matcher's form: ASCII, lowercase, one space
// between words, and a space at each end. law.Slug already owns the diacritic
// table and the punctuation rules, so this borrows them rather than keeping a
// second copy that could drift. The result of Slug is hyphen delimited, and
// the hyphens become the spaces.
//
// The padding is what makes the match a word match. Vietnamese writes each
// syllable separately, so without it a cue would match inside a syllable: "cha"
// would fire on "chất thải" and "an" on "ban hành". Whole syllable matching
// does not save a cue that is one common syllable, because "hội" really is a
// word of "hội đồng nhân dân"; that is a job for the vocabulary, which does not
// contain such cues, and for the test that checks it does not.
func fold(s string) string {
	return " " + strings.ReplaceAll(law.Slug(s), "-", " ") + " "
}

// Assignment is one document placed under one subject.
type Assignment struct {
	SubjectID  string   `json:"subject_id"`
	Confidence float64  `json:"confidence"`
	Method     string   `json:"method"`
	Matched    []string `json:"matched,omitempty"`
}

// Assignment methods. A lexical assignment matched the subject's own cues. A
// parent assignment is the domain of a subdomain that matched, carried up so a
// reader browsing the top level sees the document without the sampler having to
// walk the tree on every query.
const (
	MethodLexical = "lexical"
	MethodParent  = "parent"
)

// minScore is the number of cue words a subject needs before it is claimed. Two
// is one specific phrase, or two separate words that both point the same way. A
// threshold of one would let a single common word decide a domain, and on
// titles as formulaic as these that is most of the false positives.
const minScore = 2

// maxSubdomains caps how many subdomains one document is filed under. A
// Vietnamese decision routinely touches three fields and genuinely belongs in
// all of them, so the classifier is multi label, but past three the tail is
// noise and a document under nine subjects is under none of them.
const maxSubdomains = 3

// Classify places a document under the subjects its title, type and issuing
// body point at, most confident first.
//
// This is a lexical classifier and not the distilled one the design specifies.
// The design calls for a small multi label model trained on documents labelled
// by a teacher model, which is the right shape for a corpus this size because
// it costs one pass of a cheap model rather than a hundred and twenty thousand
// generative calls. Training it needs a labelled seed set, which needs a
// configured model route, which this environment does not have. What is here
// instead is honest about being weaker: it fires only on hand written phrases,
// it says which phrases it fired on, and it leaves a document unassigned rather
// than guessing. When a route exists the seed labelling can run and the method
// on each assignment changes from lexical to whatever produced it, which is why
// the method is recorded per assignment rather than per run.
func (v *Vocabulary) Classify(doc *law.Document) []Assignment {
	hay := fold(doc.Title + " " + doc.DocType + " " + doc.IssuingBody)

	type hit struct {
		subject *Subject
		score   int
		matched []string
	}
	var subs, doms []hit
	for _, s := range v.Subjects {
		h := hit{subject: s}
		for _, c := range s.folded {
			if strings.Contains(hay, c.text) {
				h.score += c.words
				h.matched = append(h.matched, strings.TrimSpace(c.text))
			}
		}
		if h.score < minScore {
			continue
		}
		if s.IsDomain() {
			doms = append(doms, h)
		} else {
			subs = append(subs, h)
		}
	}

	byScore := func(hits []hit) func(i, j int) bool {
		return func(i, j int) bool {
			if hits[i].score != hits[j].score {
				return hits[i].score > hits[j].score
			}
			return hits[i].subject.ID < hits[j].subject.ID
		}
	}
	sort.Slice(subs, byScore(subs))
	if len(subs) > maxSubdomains {
		subs = subs[:maxSubdomains]
	}

	out := make([]Assignment, 0, len(subs)+len(doms))
	// A domain reached through its own cue outranks the same domain reached
	// through a child, so the direct hits go in first and the parents fill in
	// around them.
	claimed := map[string]bool{}
	sort.Slice(doms, byScore(doms))
	for _, h := range doms {
		claimed[h.subject.ID] = true
		out = append(out, Assignment{
			SubjectID:  h.subject.ID,
			Confidence: confidence(h.score),
			Method:     MethodLexical,
			Matched:    h.matched,
		})
	}
	for _, h := range subs {
		out = append(out, Assignment{
			SubjectID:  h.subject.ID,
			Confidence: confidence(h.score),
			Method:     MethodLexical,
			Matched:    h.matched,
		})
		if h.subject.Parent == "" || claimed[h.subject.Parent] {
			continue
		}
		claimed[h.subject.Parent] = true
		out = append(out, Assignment{
			SubjectID:  h.subject.Parent,
			Confidence: confidence(h.score),
			Method:     MethodParent,
		})
	}
	return out
}

// confidence turns a cue word count into a number between zero and one. The
// curve is score/(score+2), so two words is a half and ten words is five sixths
// and nothing ever reaches certainty, which is the correct claim for a method
// that reads titles.
func confidence(score int) float64 {
	c := float64(score) / float64(score+2)
	return float64(int(c*100+0.5)) / 100
}

// Record is one document's place in the subject layer, as written to disk.
type Record struct {
	DocID    string       `json:"doc_id"`
	DocType  string       `json:"doc_type,omitempty"`
	Subjects []Assignment `json:"subjects,omitempty"`
}

// AssignmentsFile is where the subject stage writes, under the store's subject
// directory. It is one file rather than one file per document: the records are
// a few hundred bytes each and every consumer reads all of them, so a hundred
// and twenty thousand separate files would cost more in directory entries than
// in content.
const AssignmentsFile = "assignments.jsonl"

// Primary returns the subdomain a document is most confidently under, or its
// domain if it matched no subdomain, or the empty string if it matched nothing.
// The sampler strata are built from this, so one document falls in one stratum.
func Primary(r *Record) string {
	for _, a := range r.Subjects {
		if a.Method == MethodParent {
			continue
		}
		if strings.Contains(a.SubjectID, "/") {
			return a.SubjectID
		}
	}
	if len(r.Subjects) > 0 {
		return r.Subjects[0].SubjectID
	}
	return ""
}
