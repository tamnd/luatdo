package eval

import "fmt"

// The 23 competency questions, and what a system has to be able to represent
// before it can be asked one.
//
// They are here as data rather than as prose in a specification because two
// checklist items depend on running the same list twice: once against this
// project's graph and once against a baseline, and a list that lives in a
// document gets quietly trimmed to the questions the system already answers.
//
// Needs is the point of the whole table. A question is not answered worse by a
// system that lacks the layer it rests on, it is not expressible at all, and
// that distinction is the argument for building the layers. A search index can
// rank provisions by similarity to "deadline shorter than five working days"
// and will return provisions containing those words, which is not an answer to
// question 12 and cannot be scored as a bad one.
type Question struct {
	N      int      `json:"n"`
	Text   string   `json:"text"`
	Needs  []string `json:"needs"`
	Family string   `json:"family"`
}

// The layers a question can rest on. Citation and structure are what the
// document layer gives; the rest are the layers this project adds.
const (
	LayerStructure = "structure" // parsed documents, provisions, stable identifiers
	LayerCitation  = "citation"  // resolved citations and amendment links
	LayerConcept   = "concept"   // concepts with identity, definitions, scopes
	LayerRelation  = "relation"  // concept to concept relations
	LayerNorm      = "norm"      // structured statements with bearers and conditions
	LayerTemporal  = "temporal"  // validity intervals and amendment events
)

// Questions is the list from the problem statement, unabridged. The families
// group them the way that document does.
var Questions = []Question{
	{1, "Which documents does 45/2019/QH14 amend, transitively?", []string{LayerCitation}, "citation"},
	{2, "Which currently in force documents cite a document that has been repealed?", []string{LayerCitation, LayerTemporal}, "citation"},
	{3, "Which provincial decisions cite a central decree issued after them?", []string{LayerCitation, LayerStructure}, "citation"},

	{4, "Where is \"người lao động\" defined, and do the Labour Code and the Social Insurance Law define it the same way?", []string{LayerConcept}, "concept"},
	{5, "List every term whose definition differs between two instruments that are both in force.", []string{LayerConcept, LayerTemporal}, "concept"},
	{6, "Which concepts have no definition anywhere in the corpus but are used in more than 100 provisions?", []string{LayerConcept}, "concept"},
	{7, "Show the hierarchy under \"cơ quan nhà nước\" as the corpus actually uses it, and flag where the corpus is inconsistent about it.", []string{LayerConcept, LayerRelation}, "concept"},
	{8, "Which defined terms were introduced by an amendment rather than by the original instrument?", []string{LayerConcept, LayerTemporal}, "concept"},

	{9, "What duties does the Labour Code place on employers, and which of them carry a sanction?", []string{LayerNorm, LayerConcept}, "norm"},
	{10, "Which duties in the corpus have no identified bearer, and are they drafting defects or extraction failures?", []string{LayerNorm}, "norm"},
	{11, "What must a business do to obtain a construction permit, as a sequence of procedural steps with their document requirements and deadlines?", []string{LayerNorm, LayerConcept}, "norm"},
	{12, "Every deadline shorter than 5 working days, with the actor who must meet it.", []string{LayerNorm}, "norm"},
	{13, "Which prohibitions have no corresponding sanction anywhere in the corpus?", []string{LayerNorm, LayerConcept}, "norm"},
	{14, "For a given duty, what conditions must hold and what exceptions release the bearer from it?", []string{LayerNorm}, "norm"},
	{15, "Which norms name a \"cơ quan có thẩm quyền\" that no provision in the corpus ever resolves?", []string{LayerNorm, LayerConcept}, "norm"},

	{16, "What did article 94 of the Labour Code require on 2020-01-01, and on 2023-01-01?", []string{LayerNorm, LayerTemporal}, "temporal"},
	{17, "Which norms were in force for less than a year before being amended?", []string{LayerNorm, LayerTemporal}, "temporal"},
	{18, "Show the amendment history of one provision as a chain of events with the instrument that caused each.", []string{LayerCitation, LayerTemporal}, "temporal"},

	{19, "Two provisions both regulate the same concept and impose incompatible modalities on the same actor for the same action. Find them.", []string{LayerNorm, LayerConcept}, "conflict"},
	{20, "A concept was redefined by a later instrument. Which earlier norms mentioning it are now potentially affected, and which of them were never amended?", []string{LayerConcept, LayerNorm, LayerTemporal}, "conflict"},

	{21, "What must exist before a construction permit can be issued, following only concept level prerequisite relations and not the text?", []string{LayerConcept, LayerRelation}, "inference"},
	{22, "Which concepts are required by a norm in one instrument and defined in another, and are those two instruments connected in the citation graph at all?", []string{LayerConcept, LayerNorm, LayerCitation}, "inference"},
	{23, "Which concept pairs does the corpus treat as alternatives, in the sense that a provision offering one always offers the other?", []string{LayerConcept, LayerRelation}, "inference"},
}

// Question returns one by number.
func Ask(n int) (Question, error) {
	for _, q := range Questions {
		if q.N == n {
			return q, nil
		}
	}
	return Question{}, fmt.Errorf("there is no competency question %d, the list runs 1 to %d", n, len(Questions))
}

// Expressible reports whether a system with these layers can state the question
// at all, and names the first layer it lacks if it cannot.
//
// This is deliberately not a score. A system missing the concept layer does not
// answer question 6 badly, it has no way to say what a concept is, so there is
// nothing to compare against the right answer. Reporting that as a low score
// would put it on the same axis as a system that tried and got it wrong.
func (q Question) Expressible(has ...string) (bool, string) {
	for _, need := range q.Needs {
		found := false
		for _, h := range has {
			if h == need {
				found = true
				break
			}
		}
		if !found {
			return false, need
		}
	}
	return true, ""
}

// Coverage counts how many of the 23 a set of layers can express, grouped by
// family so a report can say which kinds of question are out of reach rather
// than only how many.
func Coverage(has ...string) (expressible int, byFamily map[string]Accuracy) {
	byFamily = map[string]Accuracy{}
	for _, q := range Questions {
		ok, _ := q.Expressible(has...)
		a := byFamily[q.Family]
		a.Observe(ok)
		byFamily[q.Family] = a
		if ok {
			expressible++
		}
	}
	return expressible, byFamily
}
