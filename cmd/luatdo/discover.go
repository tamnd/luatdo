package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/distill"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/subject"
	"github.com/tamnd/luatdo/term"
)

func init() {
	commands = append(commands,
		command{"discover", "find the concepts nobody defined, and tag the corpus for them", cmdDiscover},
	)
}

func cmdDiscover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	n := fs.Int("n", 500, "documents the teacher reads")
	seed := fs.String("seed", "m8", "sampling seed, so a draw is reproducible")
	limit := fs.Int("limit", 0, "stop after this many provisions, 0 for all")
	epochs := fs.Int("epochs", 5, "training passes for the student")
	holdout := fs.Float64("holdout", 0.2, "share of teacher output held out of training")
	threshold := fs.Int("threshold", 100, "provisions a concept must be used in to answer question 6")
	minDocs := fs.Int("min-documents", concept.DefaultThresholds.MinDocuments, "documents a concept must appear in to be promoted")
	minScopes := fs.Int("min-scopes", concept.DefaultThresholds.MinScopes, "instruments a concept must appear in to be promoted")
	source := fs.String("source", distill.SourceTeacher, "what the student learns from, teacher or gold")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}

	switch sub {
	case "sample":
		return discoverSample(s, *n, *seed)
	case "prompt":
		return discoverPrompt(s, arg(rest, 0))
	case "read":
		return discoverRead(s, *seed, *n, *limit)
	case "aggregate":
		return discoverAggregate(s, concept.Thresholds{
			MinDocuments:  *minDocs,
			MinScopes:     *minScopes,
			MinConfidence: concept.DefaultThresholds.MinConfidence,
		})
	case "define":
		return discoverDefine(s, *limit)
	case "train":
		return discoverTrain(s, *source, *epochs, *holdout)
	case "tag":
		return discoverTag(s, *limit)
	case "score":
		return discoverScore(s, *holdout)
	case "link":
		return discoverLink(s, *limit)
	case "compare":
		return discoverCompare(s, *threshold)
	default:
		return fmt.Errorf("usage: luatdo discover sample|prompt|read|aggregate|define|train|tag|score|link|compare")
	}
}

// teachable reports whether a provision is worth a model call. A chapter
// heading has no text, and a provision of a dozen bytes is a numbering artefact
// rather than a rule.
func teachable(p *law.Provision) bool {
	return (p.Kind == "clause" || p.Kind == "point" || p.Kind == "article") && len(strings.TrimSpace(p.Text)) >= 40
}

// discoverSample draws the documents the teacher reads. The draw is stratified
// by document type and ranked by a hash of the seed, the same way every other
// sampler in this project works, so no machine in the fleet needs a random
// source to agree with another.
func discoverSample(s *store.Store, n int, seed string) error {
	picked, byType, total, err := teacherSample(s, n, seed)
	if err != nil {
		return err
	}
	fmt.Printf("drew %d of %d documents across %d types, seed %q\n", len(picked), total, len(byType), seed)
	for _, t := range sortedTypes(byType) {
		fmt.Printf("  %-30s %d\n", t, byType[t])
	}
	return nil
}

func sortedTypes(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for t := range m {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// teacherSample returns the document identifiers drawn, the count per type, and
// the corpus size. It is a function rather than inline so that read and sample
// draw exactly the same documents, which is the only reason the sample command
// is worth having.
func teacherSample(s *store.Store, n int, seed string) (map[string]bool, map[string]int, int, error) {
	type candidate struct {
		id      string
		docType string
		rank    uint64
	}
	byType := map[string][]candidate{}
	total := 0
	if err := eachDoc(s, func(doc *law.Document) error {
		has := false
		for i := range doc.Provisions {
			if teachable(&doc.Provisions[i]) {
				has = true
				break
			}
		}
		if !has {
			return nil
		}
		sum := sha256.Sum256([]byte(seed + "\x00" + doc.ID))
		byType[doc.DocType] = append(byType[doc.DocType], candidate{
			id: doc.ID, docType: doc.DocType, rank: binary.BigEndian.Uint64(sum[:8]),
		})
		total++
		return nil
	}); err != nil {
		return nil, nil, 0, err
	}
	if total == 0 {
		return nil, nil, 0, fmt.Errorf("no documents with readable provisions, run luatdo parse first")
	}

	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)

	picked := map[string]bool{}
	counts := map[string]int{}
	// One per type, then proportional. A teacher sample that is all provincial
	// land decisions teaches a student about provincial land decisions.
	quota := map[string]int{}
	left := n
	for _, t := range types {
		if left == 0 {
			break
		}
		quota[t] = 1
		left--
	}
	for _, t := range types {
		quota[t] += min(left*len(byType[t])/total, len(byType[t])-quota[t])
	}
	for _, t := range types {
		group := byType[t]
		sort.Slice(group, func(i, j int) bool {
			if group[i].rank != group[j].rank {
				return group[i].rank < group[j].rank
			}
			return group[i].id < group[j].id
		})
		for _, c := range group[:min(quota[t], len(group))] {
			picked[c.id] = true
			counts[t]++
		}
	}
	return picked, counts, total, nil
}

func discoverPrompt(s *store.Store, provisionID string) error {
	if provisionID == "" {
		return fmt.Errorf("usage: luatdo discover prompt <provision-id>")
	}
	found := false
	err := eachDoc(s, func(doc *law.Document) error {
		for i := range doc.Provisions {
			if doc.Provisions[i].ID != provisionID {
				continue
			}
			found = true
			d := &concept.Discoverer{}
			fmt.Println(d.Instructions())
			fmt.Println(concept.DiscoveryPrompt(doc, &doc.Provisions[i]))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("provision %s not found", provisionID)
	}
	return nil
}

func discoverRead(s *store.Store, seed string, n, limit int) error {
	picked, _, _, err := teacherSample(s, n, seed)
	if err != nil {
		return err
	}
	completer, model, err := completerFromEnv()
	if err != nil {
		return fmt.Errorf("the discovery pass needs a model: %w", err)
	}
	d := &concept.Discoverer{Completer: completer, Model: model, MaxCorrections: 2}

	var usage api.Usage
	docs, provisions, found, failed, calls := 0, 0, 0, 0, 0
	err = eachDoc(s, func(doc *law.Document) error {
		if !picked[doc.ID] {
			return nil
		}
		if limit > 0 && provisions >= limit {
			return nil
		}
		docs++
		var sightings []concept.Sighting
		for i := range doc.Provisions {
			p := &doc.Provisions[i]
			if !teachable(p) {
				continue
			}
			if limit > 0 && provisions >= limit {
				break
			}
			provisions++
			sight, err := d.Discover(context.Background(), doc, p)
			if sight != nil {
				usage = addAPIUsage(usage, sight.Usage)
				calls += len(sight.Attempts)
				sightings = append(sightings, *sight)
				if sight.Err != "" {
					failed++
				}
				found += len(sight.Candidates)
			}
			if err != nil {
				return err
			}
		}
		return concept.WriteSightings(s.Concepts(), sightings)
	})
	fmt.Printf("read %d documents, %d provisions, %d candidate concepts, %d provisions the model could not get right, %d calls\n",
		docs, provisions, found, failed, calls)
	fmt.Printf("usage %d input, %d output, %d total tokens\n", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	return err
}

func discoverAggregate(s *store.Store, t concept.Thresholds) error {
	var sightings []concept.Sighting
	if err := concept.EachSighting(s.Concepts(), func(ss []concept.Sighting) error {
		sightings = append(sightings, ss...)
		return nil
	}); err != nil {
		return err
	}
	if len(sightings) == 0 {
		return fmt.Errorf("nothing discovered yet, run luatdo discover read first")
	}

	terms, err := loadTermUses(s)
	if err != nil {
		return err
	}
	defined := concept.DefinedLabels(terms)

	// Norm slots are not available until the norm layer has run, and an empty
	// map means the signal is missing rather than negative. The count is
	// printed so a promotion set that used only frequency says so.
	aggs := concept.Aggregate(sightings, defined, loadSubdomains(s), nil)
	if err := concept.WriteAggregations(s.Concepts(), aggs); err != nil {
		return err
	}
	promotions := concept.Promote(aggs, t)
	if err := concept.WritePromotions(s.Concepts(), promotions); err != nil {
		return err
	}

	byRule := map[string]int{}
	for _, p := range promotions {
		byRule[p.Rule]++
	}
	definedAlready := 0
	for i := range aggs {
		if aggs[i].DefinedSomewhere {
			definedAlready++
		}
	}
	fmt.Printf("%d sightings over %d distinct concepts\n", countCandidates(sightings), len(aggs))
	fmt.Printf("%d already defined somewhere in the corpus, so not promoted from usage\n", definedAlready)
	fmt.Printf("%d promoted: %d by frequency, %d by norm slot\n",
		len(promotions), byRule[concept.RuleFrequency], byRule[concept.RuleNormSlot])
	fmt.Printf("thresholds: %d documents, %d instruments\n", t.MinDocuments, t.MinScopes)
	if byRule[concept.RuleNormSlot] == 0 {
		fmt.Println("no norm slot signal available, so every promotion here is a frequency promotion")
	}
	return nil
}

func countCandidates(ss []concept.Sighting) int {
	n := 0
	for i := range ss {
		n += len(ss[i].Candidates)
	}
	return n
}

// loadSubdomains reads the subject stage's output. A store where that stage has
// not run yet returns an empty map rather than an error: the subject signal is
// one of four and the scorer treats a missing one as absent rather than as
// negative, so the linker still works without it.
func loadSubdomains(s *store.Store) map[string][]string {
	records, err := subject.ReadRecords(filepath.Join(s.Subject(), subject.AssignmentsFile))
	if err != nil {
		return nil
	}
	out := map[string][]string{}
	for i := range records {
		for _, a := range records[i].Subjects {
			out[records[i].DocID] = append(out[records[i].DocID], a.SubjectID)
		}
	}
	return out
}

func discoverDefine(s *store.Store, limit int) error {
	aggs, err := concept.ReadAggregations(s.Concepts())
	if err != nil {
		return err
	}
	promotions, err := concept.ReadPromotions(s.Concepts())
	if err != nil {
		return err
	}
	if len(promotions) == 0 {
		return fmt.Errorf("nothing promoted, run luatdo discover aggregate first")
	}
	completer, model, err := completerFromEnv()
	if err != nil {
		return fmt.Errorf("writing working definitions needs a model: %w", err)
	}
	definer := &concept.Definer{Completer: completer, Model: model, MaxCorrections: 2}

	terms := concept.PromoteToTermUses(promotions, aggs)
	byKey := map[string]*concept.Aggregation{}
	for i := range aggs {
		byKey[aggs[i].Key] = &aggs[i]
	}

	// The provisions the working definitions are written from, loaded once.
	wanted := map[string]bool{}
	for i := range terms {
		a := byKey[law.Slug(terms[i].LabelVI)]
		if a == nil {
			continue
		}
		for j, id := range a.Provisions {
			if j >= concept.DefaultMaxProvisions {
				break
			}
			wanted[id] = true
		}
	}
	texts, hashes, err := loadProvisionTexts(s, wanted)
	if err != nil {
		return err
	}

	var usage api.Usage
	var out []concept.WorkingDefinition
	written, declined := 0, 0
	for i := range terms {
		if limit > 0 && i >= limit {
			break
		}
		a := byKey[law.Slug(terms[i].LabelVI)]
		if a == nil {
			continue
		}
		w, u, err := definer.Define(context.Background(), &terms[i], a, texts, hashes)
		usage = addAPIUsage(usage, u)
		if err != nil {
			return err
		}
		if w == nil {
			declined++
			continue
		}
		out = append(out, *w)
		written++
	}
	if problems := concept.CheckWorking(out, terms); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "  "+p)
		}
		return fmt.Errorf("%d working definitions broke the fence between a statutory definition and ours", len(problems))
	}
	if err := concept.WriteWorkingDefinitions(s.Concepts(), out); err != nil {
		return err
	}
	fmt.Printf("%d working definitions written, %d concepts the provisions did not settle\n", written, declined)
	fmt.Println("none of these is a statutory definition and none of them is evidence for a norm")
	fmt.Printf("usage %d input, %d output, %d total tokens\n", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	return nil
}

// loadProvisionTexts pulls the text and hash of a named set of provisions.
func loadProvisionTexts(s *store.Store, wanted map[string]bool) (texts, hashes map[string]string, err error) {
	texts, hashes = map[string]string{}, map[string]string{}
	if len(wanted) == 0 {
		return texts, hashes, nil
	}
	err = eachDoc(s, func(doc *law.Document) error {
		for i := range doc.Provisions {
			p := &doc.Provisions[i]
			if wanted[p.ID] {
				texts[p.ID] = p.Text
				hashes[p.ID] = p.TextHash
			}
		}
		return nil
	})
	return texts, hashes, err
}

// teacherExamples turns the stored sightings into training data.
func teacherExamples(s *store.Store) ([]distill.Example, error) {
	byProvision := map[string]*distill.Example{}
	var order []string
	if err := concept.EachSighting(s.Concepts(), func(ss []concept.Sighting) error {
		for i := range ss {
			sight := &ss[i]
			e := byProvision[sight.ProvisionID]
			if e == nil {
				e = &distill.Example{ProvisionID: sight.ProvisionID, Source: distill.SourceTeacher}
				byProvision[sight.ProvisionID] = e
				order = append(order, sight.ProvisionID)
			}
			for _, c := range sight.Candidates {
				e.Spans = append(e.Spans, distill.Span{
					Text: c.LabelVI, Start: c.CharStart, End: c.CharEnd, Kind: c.Kind,
				})
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(order) == 0 {
		return nil, nil
	}

	// The spans carry offsets into the provision, so the text has to come back
	// from the corpus rather than from the sighting: storing it twice would let
	// the two drift and make an offset mean two different things.
	wanted := map[string]bool{}
	for _, id := range order {
		wanted[id] = true
	}
	texts, _, err := loadProvisionTexts(s, wanted)
	if err != nil {
		return nil, err
	}
	var out []distill.Example
	for _, id := range order {
		e := byProvision[id]
		text, ok := texts[id]
		if !ok {
			continue
		}
		e.Text = text
		// A candidate's offsets point at the quote, not at the label, so the
		// label's own span is recovered here. A label the provision no longer
		// contains is dropped rather than trained on at the wrong offset.
		var spans []distill.Span
		for _, sp := range e.Spans {
			if i := strings.Index(text, sp.Text); i >= 0 {
				spans = append(spans, distill.Span{Text: sp.Text, Start: i, End: i + len(sp.Text), Kind: sp.Kind})
			}
		}
		e.Spans = spans
		out = append(out, *e)
	}
	return out, nil
}

// discoverTrain fits the student.
//
// The source is a flag because the teacher pass needs a model and no routes are
// configured anywhere in the fleet yet. Training on the gold set instead is a
// smaller and different thing, and it is worth having: it runs the whole chain
// end to end today and it says something real, since the gold set was annotated
// by hand before any of this existed. It is not a substitute for distillation.
// Two hundred clauses is not 1.9 million provisions, and the gold set annotates
// definition clauses, where the teacher reads ordinary ones.
func discoverTrain(s *store.Store, source string, epochs int, holdout float64) error {
	var examples []distill.Example
	var err error
	switch source {
	case distill.SourceTeacher:
		examples, err = teacherExamples(s)
		if err == nil && len(examples) == 0 {
			return fmt.Errorf("no teacher output to learn from, run luatdo discover read first, or train from the gold set with -source gold")
		}
	case distill.SourceGold:
		examples, err = goldExamples(s)
		if err == nil && len(examples) == 0 {
			return fmt.Errorf("no gold set, annotate one with tools/gold/annotate.py first")
		}
	default:
		return fmt.Errorf("source %q is not %s or %s", source, distill.SourceTeacher, distill.SourceGold)
	}
	if err != nil {
		return err
	}
	train, test := distill.Split(examples, holdout)
	if len(train) == 0 {
		return fmt.Errorf("the holdout of %.2f left nothing to train on", holdout)
	}
	tagger := distill.Train(train, epochs)
	tagger.TeacherHash = distill.Fingerprint(examples)
	tagger.Source = source
	fmt.Printf("learning from the %s\n", source)

	f, err := os.Create(filepath.Join(s.Concepts(), concept.TaggerFile))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := tagger.Write(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	fmt.Printf("trained on %d provisions, %d held out, %d epochs, %d features, %d phrases known\n",
		len(train), len(test), epochs, len(tagger.Weights), len(tagger.Gazetteer))
	fmt.Printf("training set fingerprint %s\n", tagger.TeacherHash)
	if len(test) > 0 {
		fmt.Print(distill.Evaluate(tagger, test, source))
	}
	return nil
}

func loadTagger(s *store.Store) (*distill.Tagger, error) {
	f, err := os.Open(filepath.Join(s.Concepts(), concept.TaggerFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no student tagger, run luatdo discover train first")
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return distill.Read(f)
}

// discoverTag runs the student over the corpus. This is the breadth pass, and
// the whole reason the student exists: the teacher read a sample of documents
// and this reads all of them.
func discoverTag(s *store.Store, limit int) error {
	tagger, err := loadTagger(s)
	if err != nil {
		return err
	}
	docs, provisions, spans := 0, 0, 0
	err = eachDoc(s, func(doc *law.Document) error {
		if limit > 0 && provisions >= limit {
			return nil
		}
		var sightings []concept.Sighting
		for i := range doc.Provisions {
			p := &doc.Provisions[i]
			if !teachable(p) {
				continue
			}
			if limit > 0 && provisions >= limit {
				break
			}
			provisions++
			tagged := tagger.Tag(p.Text)
			if len(tagged) == 0 {
				continue
			}
			sight := concept.Sighting{
				ProvisionID: p.ID, DocID: doc.ID, TextHash: p.TextHash, Model: "student",
			}
			for _, sp := range tagged {
				kind := sp.Kind
				if kind == "" {
					// The student never guesses a kind it was not taught. A
					// candidate with no kind is filed as other, and the share of
					// those is the measurement that says whether the teacher
					// sample was broad enough.
					kind = concept.KindOther
				}
				sight.Candidates = append(sight.Candidates, concept.Candidate{
					LabelVI: sp.Text, Kind: kind, Quote: sp.Text,
					CharStart: sp.Start, CharEnd: sp.End,
					Shows: "tagged by the student, no reading", Confidence: 0.5,
					ProvisionID: p.ID, DocID: doc.ID, Model: "student",
				})
				spans++
			}
			sightings = append(sightings, sight)
		}
		if len(sightings) == 0 {
			return nil
		}
		docs++
		return concept.WriteSightings(s.Concepts(), sightings)
	})
	fmt.Printf("tagged %d provisions in %d documents, %d spans\n", provisions, docs, spans)
	return err
}

// discoverScore measures the student twice, against two different things,
// because they are two different questions. Agreement with the teacher says the
// student copied what the model did. Accuracy on the gold set says whether
// either of them was right.
//
// Whichever set the student was trained on is scored on its held out part only.
// The model file records what it learned from so that this holds even when the
// person running the command has forgotten, and the holdout has to be the one
// training used or the two splits will not line up.
func discoverScore(s *store.Store, holdout float64) error {
	tagger, err := loadTagger(s)
	if err != nil {
		return err
	}
	if tagger.Source == "" {
		fmt.Println("this model does not say what it was trained on, so a number below may be measured on its own training set")
	}

	examples, err := teacherExamples(s)
	if err != nil {
		return err
	}
	if len(examples) > 0 {
		examples = heldOutIfTrainedOn(examples, tagger.Source, distill.SourceTeacher, holdout)
	}
	if len(examples) > 0 {
		fmt.Print(distill.Evaluate(tagger, examples, distill.SourceTeacher))
		fmt.Println("           this is agreement with the teacher, not correctness")
	}

	gold, err := goldExamples(s)
	if err != nil {
		return err
	}
	if len(gold) > 0 {
		gold = heldOutIfTrainedOn(gold, tagger.Source, distill.SourceGold, holdout)
	}
	if len(gold) == 0 {
		fmt.Println("against gold     no gold set to score on, so nothing here says whether either of them is right")
		return nil
	}
	fmt.Print(distill.Evaluate(tagger, gold, distill.SourceGold))
	fmt.Println("           this is correctness, measured on clauses annotated by hand")
	return nil
}

// heldOutIfTrainedOn cuts a scoring set down to the part the student never saw,
// and leaves it whole when the student was fitted to something else. A student
// scored on its own training data reports a number that only says it has enough
// weights to memorise, which is the easiest mistake to make here and the hardest
// to notice afterwards.
func heldOutIfTrainedOn(examples []distill.Example, trainedOn, set string, holdout float64) []distill.Example {
	if trainedOn != set {
		return examples
	}
	_, test := distill.Split(examples, holdout)
	return test
}

// goldExamples turns the hand annotated gold set into span examples. The gold
// set annotates definition clauses, so the spans in it are the defined terms,
// which is a harder target than the teacher's: a definition clause names its
// term once and mentions several others.
func goldExamples(s *store.Store) ([]distill.Example, error) {
	gold, err := concept.ReadGold(s.Concepts())
	if err != nil {
		return nil, err
	}
	var out []distill.Example
	for _, g := range gold {
		e := distill.Example{ProvisionID: g.UnitID, Text: g.Text, Source: distill.SourceGold}
		for _, t := range g.Terms {
			i := strings.Index(g.Text, t.LabelVI)
			if i < 0 {
				continue
			}
			e.Spans = append(e.Spans, distill.Span{
				Text: t.LabelVI, Start: i, End: i + len(t.LabelVI), Kind: t.Kind,
			})
		}
		out = append(out, e)
	}
	return out, nil
}

func discoverLink(s *store.Store, limit int) error {
	terms, err := loadTermUses(s)
	if err != nil {
		return err
	}
	if len(terms) == 0 {
		return fmt.Errorf("no term uses to link to, run luatdo concepts read first")
	}
	ix := concept.NewIndex(terms)
	corpus, err := loadLinkCorpus(s)
	if err != nil {
		return err
	}

	docs, mentions := 0, 0
	byMethod := map[string]int{}
	err = eachDoc(s, func(doc *law.Document) error {
		if limit > 0 && docs >= limit {
			return nil
		}
		report := &concept.MentionReport{DocID: doc.ID}
		for i := range doc.Provisions {
			p := &doc.Provisions[i]
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			for _, m := range ix.Scan(doc.ID, p.ID, p.Text) {
				ix.Resolve(&m, doc.ID, corpus, doc.EffectiveFrom)
				byMethod[m.Method]++
				report.Mentions = append(report.Mentions, m)
				mentions++
			}
		}
		if len(report.Mentions) == 0 {
			return nil
		}
		docs++
		concept.Summarize(report)
		return concept.WriteMentions(s.Concepts(), report)
	})
	fmt.Printf("%d mentions in %d documents\n", mentions, docs)
	for _, m := range []string{concept.MethodInScope, concept.MethodScored, concept.MethodAdjudicated, concept.MethodUnresolved} {
		fmt.Printf("  %-14s %d\n", m, byMethod[m])
	}
	fmt.Println("an unresolved mention is correct output, a confidently wrong link is a defect")
	return err
}

// loadLinkCorpus assembles the free context the mention scorer runs on. Every
// piece of it was computed by an earlier pass, which is the point: the strongest
// signal in the whole linker costs nothing here because pass L1 already built
// the citation graph.
//
// Implements is left empty. The corpus records citations and amendments, and a
// circular implementing a decree is a third relation nothing extracts yet, so
// the hierarchy signal is unavailable rather than negative. The link command
// prints how many mentions were decided without it.
func loadLinkCorpus(s *store.Store) (concept.Corpus, error) {
	c := concept.Corpus{
		Cites:         map[string]map[string]bool{},
		Implements:    map[string]string{},
		EffectiveFrom: map[string]string{},
		Subdomains:    loadSubdomains(s),
	}
	links, err := loadLinks(s)
	if err != nil {
		return c, err
	}
	for _, l := range links {
		if l.ToDoc == "" {
			continue
		}
		if c.Cites[l.FromDoc] == nil {
			c.Cites[l.FromDoc] = map[string]bool{}
		}
		c.Cites[l.FromDoc][l.ToDoc] = true
	}
	err = eachDoc(s, func(doc *law.Document) error {
		c.EffectiveFrom[doc.ID] = doc.EffectiveFrom
		return nil
	})
	return c, err
}

func discoverCompare(s *store.Store, threshold int) error {
	baseline, err := grammarBaseline(s)
	if err != nil {
		return err
	}
	terms, err := loadTermUses(s)
	if err != nil {
		return err
	}
	promotions, err := concept.ReadPromotions(s.Concepts())
	if err != nil {
		return err
	}
	aggs, err := concept.ReadAggregations(s.Concepts())
	if err != nil {
		return err
	}
	terms = append(terms, concept.PromoteToTermUses(promotions, aggs)...)

	fmt.Print(concept.Compare(baseline, terms, aggs, threshold))
	return nil
}

// grammarBaseline rebuilds the design this project replaced, from the same
// corpus, so the comparison is between two readings of one thing rather than
// between a measurement and a memory.
func grammarBaseline(s *store.Store) (concept.Baseline, error) {
	b := concept.Baseline{Labels: map[string]bool{}}
	docs := map[string]bool{}
	err := eachDoc(s, func(doc *law.Document) error {
		defs := term.Extract(doc)
		if len(defs) == 0 {
			return nil
		}
		docs[doc.ID] = true
		for _, d := range defs {
			b.Labels[law.Slug(d.Term)] = true
		}
		return nil
	})
	b.Terms = len(b.Labels)
	b.Documents = len(docs)
	return b, err
}
