package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/temporal"
)

func init() {
	commands = append(commands,
		command{"temporal", "read amending instructions, build the version graph, and query it at a date", cmdTemporal},
	)
}

func cmdTemporal(args []string) error {
	fs := flag.NewFlagSet("temporal", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	limit := fs.Int("limit", 0, "stop after this many provisions, 0 for all")
	only := fs.String("doc", "", "one amending instrument, for a trial run before a campaign")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	date := fs.String("date", "", "the date a query is asked at, YYYY-MM-DD")
	before := fs.String("before", "", "the earlier date, for a two date comparison")
	days := fs.Int("days", 365, "how short a life counts as short, for question 17")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	dir := s.Temporal()

	switch sub {
	case "prompt":
		return temporalPrompt(s, arg(rest, 0))
	case "read":
		return temporalRead(s, dir, *only, *limit, *corrections)
	case "build":
		return temporalBuild(s, dir)
	case "check":
		return temporalCheck(dir)
	case "verify":
		return temporalVerify(s, dir)
	case "ask":
		return temporalAsk(dir, arg(rest, 0), arg(rest, 1), *before, *date, *days)
	default:
		return fmt.Errorf("usage: luatdo temporal prompt|read|build|check|verify|ask")
	}
}

// amendingLinks returns the document to document amendment graph pass L1 built,
// both ways round: which instruments amend what, and which instruments were read
// as amending anything at all.
func amendingLinks(s *store.Store) (map[string][]string, error) {
	links, err := loadLinks(s)
	if err != nil {
		return nil, err
	}
	seen := map[string]map[string]bool{}
	for _, l := range links {
		if l.Kind != "amends" || l.ToDoc == "" || l.FromDoc == "" {
			continue
		}
		if seen[l.FromDoc] == nil {
			seen[l.FromDoc] = map[string]bool{}
		}
		seen[l.FromDoc][l.ToDoc] = true
	}
	out := map[string][]string{}
	for from, tos := range seen {
		for to := range tos {
			out[from] = append(out[from], to)
		}
		sort.Strings(out[from])
	}
	return out, nil
}

// amendingProvisions returns the provisions of one instrument worth asking
// about. The filter is deliberately loose: it costs a model call to be told an
// instruction is not there, and it costs a missing amendment to skip one that
// is.
func amendingProvisions(doc *law.Document) []*law.Provision {
	var out []*law.Provision
	for _, p := range coverage.Extractable(doc) {
		if amendingWords(p.Text) {
			out = append(out, p)
		}
	}
	return out
}

// amendingWords is the cheap filter in front of the model.
var amendingWords = func() func(string) bool {
	words := []string{
		"sửa đổi", "bổ sung", "bãi bỏ", "hủy bỏ", "huỷ bỏ", "thay thế",
		"hết hiệu lực", "ngưng hiệu lực", "tiếp tục hiệu lực", "hợp nhất",
	}
	return func(text string) bool {
		lower := strings.ToLower(text)
		for _, w := range words {
			if strings.Contains(lower, w) {
				return true
			}
		}
		return false
	}
}()

// temporalPrompt prints the exact prompt for one provision and calls nothing.
func temporalPrompt(s *store.Store, provisionID string) error {
	if provisionID == "" {
		return fmt.Errorf("usage: luatdo temporal prompt <provision-id>")
	}
	doc, err := loadDoc(s, provisionID)
	if err != nil {
		return err
	}
	text := provisionTexts(doc)[provisionID]
	if text == "" {
		return fmt.Errorf("provision %s has no extractable text", provisionID)
	}
	r := &temporal.Reader{}
	fmt.Println(r.Instructions())
	fmt.Println(temporal.Prompt(provisionID, text))
	if !amendingWords(text) {
		fmt.Println("this provision holds none of the amending words, so no model would be called for it")
	}
	return nil
}

// temporalRead runs the amending instruction pass, one file of operations per
// instrument. An instrument that fails leaves no file, which is what puts it
// back in the queue next time.
func temporalRead(s *store.Store, dir, only string, limit, corrections int) error {
	amends, err := amendingLinks(s)
	if err != nil {
		return err
	}
	if len(amends) == 0 {
		return fmt.Errorf("no amendment links, run luatdo cite first")
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	r := &temporal.Reader{Completer: eng.completer, Model: eng.model, MaxCorrections: corrections}
	fmt.Printf("temporal: reading with %s from %s\n", eng.model, eng.source)

	instruments := make([]string, 0, len(amends))
	for id := range amends {
		if only != "" && id != only {
			continue
		}
		instruments = append(instruments, id)
	}
	sort.Strings(instruments)

	var usage api.Usage
	read, asked, skipped, ops, failed := 0, 0, 0, 0, 0
	ctx := context.Background()
	for _, id := range instruments {
		if limit > 0 && asked >= limit {
			break
		}
		doc, derr := loadDoc(s, id)
		if derr != nil {
			// An instrument with metadata only has no instruction to read. That
			// is coverage rather than failure, and it is counted as coverage.
			skipped++
			continue
		}
		provisions := amendingProvisions(doc)
		var found []temporal.Operation
		cut := false
		for i := range provisions {
			if limit > 0 && asked >= limit {
				cut = true
				break
			}
			asked++
			started := time.Now()
			got, u, rerr := r.Read(ctx, doc.ID, provisions[i].ID, provisions[i].Text, law.ISODate(doc.EffectiveFrom))
			usage = addAPIUsage(usage, u)
			// A provision takes a reasoning model minutes, and a pass that says
			// nothing for an hour looks exactly like a hang.
			fmt.Fprintf(os.Stderr, "  %-60s %d operations, %s\n",
				provisions[i].ID, len(got), time.Since(started).Round(time.Second))
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "  %s: %v\n", provisions[i].ID, rerr)
				failed++
				continue
			}
			found = append(found, got...)
		}
		if cut {
			// A limit stopped this instrument part way, so it was not read and it
			// leaves no file. Writing one would take the rest of it out of the
			// queue forever.
			break
		}
		read++
		ops += len(found)
		if werr := temporal.WriteOperations(dir, doc.ID, found); werr != nil {
			return werr
		}
	}
	fmt.Printf("read %d instruments, asked about %d provisions, skipped %d instruments with no content\n", read, asked, skipped)
	fmt.Printf("%d operations, %d provisions the model could not get right\n", ops, failed)
	fmt.Printf("usage %d input, %d output, %d total tokens\n", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	reportRoutes(eng)
	fmt.Println("nothing is versioned yet, run luatdo temporal build")
	return nil
}

// temporalBuild resolves the read operations against the corpus and applies them
// in date order. Everything here is deterministic: the model read the sentences
// and code builds the graph.
func temporalBuild(s *store.Store, dir string) error {
	ops, err := temporal.AllOperations(dir)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		return fmt.Errorf("no operations to build from, run luatdo temporal read first")
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	c := temporal.NewCorpus(docs)
	amends, err := amendingLinks(s)
	if err != nil {
		return err
	}

	resolved := temporal.Resolve(ops, c, amends)
	layer, ties := temporal.Build(c, resolved)
	if err := temporal.WriteLayer(dir, layer); err != nil {
		return err
	}
	counts := temporal.Count(layer)
	report := temporal.Check(layer)

	fmt.Printf("built %d versions of %d components from %d operations\n",
		counts.Versions, counts.Components, len(ops))
	fmt.Printf("%d events, %d components amended at least once, %d with nothing in force, %d suspended\n",
		counts.Events, counts.Amended, counts.Repealed, counts.Suspended)
	fmt.Printf("%d events with no date, excluded from every point in time query\n", counts.Undated)
	fmt.Printf("%d operations quarantined and applied to nothing\n", counts.Quarantined)
	for _, reason := range sortedKeys(counts.ByReason) {
		fmt.Printf("  %-20s %d\n", reason, counts.ByReason[reason])
	}
	for _, kind := range sortedKeys(counts.ByKind) {
		fmt.Printf("  %-20s %d\n", kind, counts.ByKind[kind])
	}
	for _, tie := range ties {
		fmt.Printf("tie: %s\n", tie)
	}
	fmt.Print(report.String())

	versioned := map[string]bool{}
	for _, v := range layer.Versions {
		versioned[v.DocID] = true
	}
	refused := refusedDocs(layer)
	if refused > 0 {
		fmt.Printf("%d documents were not versioned at all, because the parse repeats component identifiers or the date cannot be read\n", refused)
	}
	summary := temporal.Summary{
		Instruments: countInstruments(ops), Operations: len(ops),
		Applied: counts.Events, Quarantined: counts.Quarantined, Undated: counts.Undated,
		Versioned: len(versioned), Refused: refused, Versions: counts.Versions, Components: counts.Components,
		Ties: ties, Reasons: counts.ByReason, Kinds: counts.ByKind, Problems: len(report.Problems),
	}
	return temporal.WriteSummary(dir, summary)
}

// refusedDocs counts the documents the build refused to version. They are worth
// their own number rather than a line in the reason table, because a refused
// document is a hole in coverage that no amount of reading will fill: the fix
// is in the parser, not in the model.
func refusedDocs(layer *temporal.Layer) int {
	docs := map[string]bool{}
	for _, q := range layer.Quarantined {
		switch q.Quarantine {
		case temporal.QuarantineCollidingParse, temporal.QuarantineUndatedDocument:
			docs[q.TargetDoc] = true
		}
	}
	return len(docs)
}

func countInstruments(ops []temporal.Operation) int {
	seen := map[string]bool{}
	for _, op := range ops {
		seen[op.AmendingDoc] = true
	}
	return len(seen)
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedKeysBool is sortedKeys for a set. Verification walks the versioned
// documents, and a report that lists them in map order is a report nobody can
// diff against yesterday's.
func sortedKeysBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// temporalCheck runs the invariants over the built layer and prints what broke.
// It prints rather than fails, because the violations are the map of where the
// reading is weak and a command that refuses to speak teaches nobody anything.
func temporalCheck(dir string) error {
	layer, err := temporal.ReadLayer(dir)
	if err != nil {
		return err
	}
	if len(layer.Versions) == 0 {
		return fmt.Errorf("nothing is versioned, run luatdo temporal build first")
	}
	report := temporal.Check(layer)
	fmt.Print(report.String())
	for i, p := range report.Problems {
		if i >= 40 {
			fmt.Printf("and %d more\n", len(report.Problems)-40)
			break
		}
		fmt.Printf("  %s\n", p.String())
	}
	if len(layer.Quarantined) > 0 {
		fmt.Printf("\n%d operations were quarantined and changed nothing:\n", len(layer.Quarantined))
		for i, q := range layer.Quarantined {
			if i >= 20 {
				fmt.Printf("  and %d more\n", len(layer.Quarantined)-20)
				break
			}
			fmt.Printf("  %-20s %s %s\n", q.Quarantine, q.AmendingDoc, q.TargetRef)
		}
	}
	return nil
}

// temporalVerify is invariant 9: wherever a văn bản hợp nhất exists, the
// computed text at the consolidation date must match it. This is the only check
// that can prove the layer wrong rather than merely inconsistent.
func temporalVerify(s *store.Store, dir string) error {
	layer, err := temporal.ReadLayer(dir)
	if err != nil {
		return err
	}
	if len(layer.Versions) == 0 {
		return fmt.Errorf("nothing is versioned, run luatdo temporal build first")
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	of, err := consolidationLinks(s)
	if err != nil {
		return err
	}
	view := temporal.NewView(layer)
	versioned := map[string]bool{}
	for _, v := range layer.Versions {
		versioned[v.DocID] = true
	}

	var matches []temporal.Match
	checked := 0
	for _, d := range docs {
		if !temporal.IsConsolidated(d) {
			continue
		}
		checked++
		for _, target := range sortedKeysBool(versioned) {
			if !consolidates(d, target, docs, of) {
				continue
			}
			date := law.ISODate(d.EffectiveFrom)
			if date == "" {
				// A consolidated text is a restatement rather than a new rule, so
				// the corpus often carries no commencement for it. The day the
				// instrument last changed is the day it was consolidated at.
				date = view.LastChange(target)
				fmt.Printf("%s states no date, comparing at %s, the day %s last changed\n", d.ID, date, target)
			}
			m := temporal.Compare(view, target, d, date)
			matches = append(matches, m)
			fmt.Print(m.String())
		}
	}
	fmt.Printf("%d consolidated texts in the corpus, %d compared against a version graph\n", checked, len(matches))
	if len(matches) == 0 {
		fmt.Println("no consolidated text lines up with anything versioned, so invariant 9 is untested rather than passing")
		return nil
	}
	agreed, compared := 0, 0
	for _, m := range matches {
		agreed += m.Agreed
		compared += m.Compared
	}
	fmt.Printf("%d components compared, %d agreed\n", compared, agreed)

	summary, err := temporal.ReadSummary(dir)
	if err != nil || summary == nil {
		return err
	}
	summary.Consolidated = matches
	return temporal.WriteSummary(dir, *summary)
}

// consolidates reports whether a consolidated text is a consolidation of one
// instrument. The title of a văn bản hợp nhất names the instrument it
// consolidates, and where it does not, the citation graph does.
//
// The title is the weaker of the two signals and it fails on exactly the shape
// the drafters use most. Văn bản hợp nhất số 68/2026/VBHN-NĐ-BCT consolidates
// Nghị định số 72/2025/NĐ-CP, and its title repeats the subject of that decree
// word for word while naming neither its number nor the word Nghị định. So the
// dataset relation is tried first: a Hợp nhất edge from the consolidated text to
// the instrument is the drafter saying which instrument this is.
func consolidates(consolidated *law.Document, target string, docs []*law.Document, of map[string][]string) bool {
	for _, id := range of[consolidated.ID] {
		if id == target {
			return true
		}
	}
	for _, d := range docs {
		if d.ID != target {
			continue
		}
		title := strings.ToLower(consolidated.Title)
		return strings.Contains(title, strings.ToLower(d.Title)) ||
			strings.Contains(strings.ToUpper(consolidated.Title), strings.ToUpper(d.OfficialNumber))
	}
	return false
}

// consolidationLinks returns, for each consolidated text, the instruments it
// consolidates according to the dataset. The label rather than the kind is what
// carries this: a Hợp nhất relation is recorded as a citation, because
// consolidating an instrument changes nothing about it.
func consolidationLinks(s *store.Store) (map[string][]string, error) {
	links, err := loadLinks(s)
	if err != nil {
		return nil, err
	}
	seen := map[string]map[string]bool{}
	for _, l := range links {
		if l.ToDoc == "" || l.FromDoc == "" {
			continue
		}
		if !strings.Contains(strings.ToLower(l.Snippet), "hợp nhất") {
			continue
		}
		if seen[l.FromDoc] == nil {
			seen[l.FromDoc] = map[string]bool{}
		}
		seen[l.FromDoc][l.ToDoc] = true
	}
	out := map[string][]string{}
	for from, tos := range seen {
		for to := range tos {
			out[from] = append(out[from], to)
		}
		sort.Strings(out[from])
	}
	return out, nil
}

// temporalAsk runs competency questions 16, 17 and 18 over the version graph
// alone, with no text read at query time. Every one of them takes a date.
func temporalAsk(dir, question, subject, before, date string, days int) error {
	layer, err := temporal.ReadLayer(dir)
	if err != nil {
		return err
	}
	if len(layer.Versions) == 0 {
		return fmt.Errorf("nothing is versioned, run luatdo temporal build first")
	}
	v := temporal.NewView(layer)

	switch question {
	case "16":
		if subject == "" || date == "" {
			return fmt.Errorf("usage: luatdo temporal ask 16 <component-id> -before <date> -date <date>")
		}
		if before == "" {
			return fmt.Errorf("question 16 compares two dates, pass the earlier one with -before")
		}
		got := v.AskWhatItSaid(subject, before, date)
		fmt.Printf("%s\n\n", got.ComponentID)
		printAt(before, got.EarlyText, got.EarlyForce)
		printAt(date, got.LateText, got.LateForce)
		if !got.Changed {
			fmt.Println("the text is the same on both dates")
		}
		for _, e := range got.Events {
			fmt.Printf("  %s  %-12s %s\n", e.Date, e.Kind, e.CausedByDoc)
			if e.Instruction != "" {
				fmt.Printf("    %s\n", e.Instruction)
			}
		}
	case "17":
		got := v.AskShortLived(days)
		fmt.Printf("%d versions were in force for less than %d days before being replaced\n", len(got), days)
		for i, sl := range got {
			if i >= 50 {
				fmt.Printf("and %d more\n", len(got)-50)
				break
			}
			fmt.Printf("  %4d days  %-50s %s to %s, ended by %s\n",
				sl.Days, sl.ComponentID, sl.From, sl.To, orUnknown(sl.EndedByDoc))
		}
	case "18":
		if subject == "" {
			return fmt.Errorf("usage: luatdo temporal ask 18 <component-id>")
		}
		got := v.AskHistory(subject)
		if len(got) == 0 {
			return fmt.Errorf("%s has no versions, so it was either never read or is not a component", subject)
		}
		fmt.Printf("%s has %d versions\n", subject, len(got))
		for _, step := range got {
			fmt.Printf("  v%-2d %s to %-10s %-12s %-12s %s\n", step.Seq, step.From,
				orOpenDate(step.To), step.Force, step.EventKind, orUnknown(step.CausedBy))
			if step.Instruction != "" {
				fmt.Printf("      %s\n", step.Instruction)
			}
		}
	default:
		return fmt.Errorf("usage: luatdo temporal ask 16|17|18")
	}
	if undated := v.UndatedEvents(); len(undated) > 0 {
		fmt.Printf("\n%d amendments have no date and are in none of this answer\n", len(undated))
	}
	return nil
}

func printAt(date, text, force string) {
	if text == "" {
		fmt.Printf("on %s there was no version in force\n\n", date)
		return
	}
	fmt.Printf("on %s (%s)\n%s\n\n", date, force, text)
}

func orUnknown(s string) string {
	if s == "" {
		return "an event that names no instrument"
	}
	return s
}

func orOpenDate(s string) string {
	if s == "" {
		return "now"
	}
	return s
}
