package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/concept"
	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/store"
)

func init() {
	commands = append(commands,
		command{"eval", "run the metric suite, validate the judge, and check the release gates", cmdEval},
	)
}

func cmdEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	n := fs.Int("n", 60, "items to draw for the judge sample")
	rejectShare := fs.Int("rejected", 50, "percent of the judge sample drawn from the gate's rejections")
	seed := fs.String("seed", "m12", "sample seed, so the same corpus draws the same items anywhere")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	switch sub {
	case "baselines":
		fmt.Print(eval.Compare(eval.Baselines...))
		return nil
	case "ablations":
		fmt.Print(eval.AblationReport())
		return nil
	}

	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	switch sub {
	case "suite", "":
		return evalSuite(s, arg(rest, 0))
	case "judge":
		return evalJudge(s, arg(rest, 0), *n, *rejectShare, *seed)
	case "gates":
		return evalGates(s, arg(rest, 0))
	default:
		return fmt.Errorf("usage: luatdo eval suite|judge sample|judge score|judge rejudge|gates|baselines|ablations")
	}
}

// evalSuite prints one table per layer. Every number carries the sample it came
// from, which is the whole reason these tables are built by eval rather than
// printed at each call site.
func evalSuite(s *store.Store, scopeName string) error {
	scope := "the whole store"
	if scopeName != "" {
		scope = scopeName
	}
	records, err := loadTrusted(s)
	if err != nil {
		return err
	}
	jobs, err := loadNormJobs(s)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// The norm layer. The evidence check is the cheapest correct measurement in
	// the project: no annotation, no judge, just the quote sliced back out of
	// the provision it claims to come from.
	norms := eval.NewTable("norms", scope)
	var evidence eval.Count
	var bearers eval.Count
	for i := range records {
		st := &records[i].Statement
		evidence.Observe(true, st.Evidence.Quote != "" && st.Evidence.End > st.Evidence.Start)
		if norm.NeedsBearer(st.Type) {
			bearers.Observe(true, st.Bearer != nil && st.Bearer.ClassID != "")
		}
	}
	norms.Count("evidence-offsets", evidence)
	norms.Count("bearer-placement", bearers)

	var judged, rejected int
	for _, job := range jobs {
		for _, r := range job.Records {
			switch r.Status {
			case norm.StatusVerified:
				judged++
			case norm.StatusRejected:
				judged++
				rejected++
			}
		}
	}
	norms.Rate("gate-acceptance", eval.Accuracy{Right: judged - rejected, Of: judged})
	norms.Note("the acceptance rate is the judge's own output and is not evidence about the judge, see luatdo eval judge")
	fmt.Print(norms)

	// The judge, if anybody has labelled a sample. A suite that silently omits
	// this table reads as though the judge were validated.
	agreement, err := judgeAgreement(s)
	if err != nil {
		return err
	}
	fmt.Println()
	if agreement == nil {
		fmt.Println("judge         not validated, no labelled sample in this store, run luatdo eval judge sample")
		fmt.Println("              every precision figure above was produced by an instrument nobody has checked")
		return nil
	}
	fmt.Print(agreement)
	if agreement.Thin() {
		fmt.Printf("              %d items is a thin sample and the kappa above has a wide interval\n", agreement.Pairs)
	}
	return nil
}

// evalJudge draws the blind sample or scores the labels a person filled in.
func evalJudge(s *store.Store, action string, n, rejectShare int, seed string) error {
	dir := s.Eval()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	labelPath, keyPath := filepath.Join(dir, eval.LabelFile), filepath.Join(dir, eval.KeyFile)

	switch action {
	case "sample":
		items, keys, err := judgeItems(s)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return fmt.Errorf("no judged statements in this store, run luatdo norms first")
		}
		gotItems, gotKeys := eval.Sample(items, keys, n, rejectShare, seed)
		if err := eval.WriteItems(labelPath, gotItems); err != nil {
			return err
		}
		if err := eval.WriteKeys(keyPath, gotKeys); err != nil {
			return err
		}
		drawn := 0
		for _, k := range gotKeys {
			if k.Stratum == "rejected" {
				drawn++
			}
		}
		fmt.Printf("wrote %d items to %s, %d of them from the gate's rejections\n", len(gotItems), labelPath, drawn)
		fmt.Println("fill the human field with entailed, not_entailed or unsure, and why when you disagree")
		fmt.Println("the judge's verdict is withheld on purpose, it is in", keyPath)
		return nil
	case "score":
		labels, err := eval.ReadItems(labelPath)
		if err != nil {
			return err
		}
		keys, err := eval.ReadKeys(keyPath)
		if err != nil {
			return err
		}
		score(labels, keys)
		fmt.Printf("rejections    %s\n", eval.RejectionCheck(labels, keys))
		fmt.Println("              a false positive here is a correct statement the gate deleted")
		// The rerun, when one exists, is printed under the same labels rather
		// than in place of the first score. A prompt fix that is only ever shown
		// by its own new number is indistinguishable from one that was tuned
		// until the number came out.
		rerun, err := eval.ReadKeys(filepath.Join(dir, eval.RerunFile))
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		fmt.Println("\nsame labels, judge prompt after the fix:")
		score(labels, rerun)
		for _, m := range eval.Moves(keys, rerun) {
			fmt.Printf("moved         %d items from %s to %s\n", m.N, m.From, m.To)
		}
		return nil
	case "rejudge":
		return rejudge(s, labelPath, filepath.Join(dir, eval.RerunFile))
	}
	return fmt.Errorf("usage: luatdo eval judge sample|score|rejudge")
}

// score prints one agreement and the reasons the annotator left on the
// disagreements.
//
// The rejection check is not in here on purpose. It is a statement about the
// gate that drew the sample, not about whichever key it is being scored
// against, and printing it under a rerun would read as a second measurement
// when it is the same one twice.
func score(labels []eval.Item, keys []eval.Key) {
	agreement, unsure, disagreed := eval.Judged(labels, keys)
	fmt.Print(agreement)
	fmt.Printf("unsure        %d items the annotator could not decide, left out of the agreement\n", unsure)
	for _, d := range disagreed {
		if d.Why != "" {
			fmt.Printf("  %s: %s\n", d.ProvisionID, d.Why)
		}
	}
}

// rejudge runs the entailment judge again over the sampled statements with
// whatever the prompt says today, and writes its verdicts beside the original
// key instead of over it.
//
// The labels are not touched and are not read here. That is the only thing
// keeping this honest: the sample was worked blind against the old judge, so
// the new judge is being scored against a fixed answer sheet it had no hand in.
func rejudge(s *store.Store, labelPath, outPath string) error {
	labels, err := eval.ReadItems(labelPath)
	if err != nil {
		return fmt.Errorf("no sample to rejudge, run luatdo eval judge sample first: %w", err)
	}
	want := map[string]bool{}
	for _, it := range labels {
		want[it.ID] = true
	}
	jobs, err := loadNormJobs(s)
	if err != nil {
		return err
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	byID := map[string]*law.Document{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	judge := &extract.Judge{Completer: eng.completer, Model: eng.model, Registry: reg, MaxCorrections: 2}

	var keys []eval.Key
	calls := 0
	for _, job := range jobs {
		for i := range job.Records {
			r := &job.Records[i]
			if !want[r.ID] {
				continue
			}
			doc := byID[r.DocID]
			if doc == nil {
				return fmt.Errorf("no parsed document %s for record %s", r.DocID, r.ID)
			}
			w, err := extract.BuildWindow(doc, r.ProvisionID)
			if err != nil {
				return err
			}
			verdict, _, err := judge.Entail(context.Background(), w, &r.Statement)
			if err != nil {
				return err
			}
			calls++
			machine := eval.LabelNotEntailed
			if verdict.Verdict == norm.VerdictEntailed {
				machine = eval.LabelEntailed
			}
			stratum := "accepted"
			if r.Status == norm.StatusRejected {
				stratum = "rejected"
			}
			keys = append(keys, eval.Key{ID: r.ID, Machine: machine, Stratum: stratum})
			fmt.Printf("%-12s %s\n", verdict.Verdict, r.ProvisionID)
		}
	}
	if err := eval.WriteKeys(outPath, keys); err != nil {
		return err
	}
	fmt.Printf("rejudged %d of %d sampled statements in %d model calls, wrote %s\n", len(keys), len(labels), calls, outPath)
	fmt.Println("the stratum is still the first judge's, because that is the pass whose rejections were sampled")
	return nil
}

// evalGates runs the release gates over a campaign and reports the verdict.
func evalGates(s *store.Store, scopeName string) error {
	v, err := gateVerdict(s, scopeName)
	if err != nil {
		return err
	}
	fmt.Print(v)
	if ok, _ := v.Ship(); !ok {
		return fmt.Errorf("release gates failed")
	}
	return nil
}

// gateVerdict is shared with the export path, which is the point of the
// milestone item: a failing campaign cannot ship by accident because the gate
// is on the road out rather than in a document beside it.
func gateVerdict(s *store.Store, scopeName string) (eval.Verdict, error) {
	var v eval.Verdict
	records, err := loadTrusted(s)
	if err != nil {
		return v, err
	}
	if scopeName != "" {
		sc, err := campaign.LookupScope(scopeName)
		if err != nil {
			return v, err
		}
		_, docs, err := campaignDocs(s, sc)
		if err != nil {
			return v, err
		}
		var kept []norm.Record
		for _, r := range records {
			if docs[r.DocID] {
				kept = append(kept, r)
			}
		}
		records = kept
	}

	var evidence, bearers eval.Count
	for i := range records {
		st := &records[i].Statement
		evidence.Observe(true, st.Evidence.Quote != "" && st.Evidence.End > st.Evidence.Start)
		if norm.NeedsBearer(st.Type) {
			bearers.Observe(true, st.Bearer != nil && st.Bearer.ClassID != "")
		}
	}
	gate := func(name string) eval.Gate {
		g, _ := eval.Named(name)
		return g
	}
	v.Results = append(v.Results,
		gate("evidence").Check(evidence.TP, evidence.Decided()),
		gate("bearer-placement").Check(bearers.TP, bearers.Decided()),
	)

	// The concept layer's invariants are already a build failure, so the gate
	// here reports the same check rather than a second opinion on it.
	if layer, err := concept.ReadLayer(s.Concepts()); err == nil && layer != nil {
		problems := layer.Check()
		v.Results = append(v.Results, gate("invariants").Check(len(layer.Concepts)-len(problems), len(layer.Concepts)))
	}

	agreement, err := judgeAgreement(s)
	if err != nil {
		return v, err
	}
	pairs := 0
	kappa := 0.0
	if agreement != nil {
		pairs, kappa = agreement.Pairs, agreement.Kappa()
	}
	v.Results = append(v.Results, gate("judge-agreement").CheckValue(kappa, pairs))
	return v, nil
}

// judgeAgreement reads the labelled sample if a person has worked one, and
// returns nil when nobody has. Nil is not zero: a store with no labels has not
// failed the check, it has not run it, and the two print differently.
func judgeAgreement(s *store.Store) (*eval.Agreement, error) {
	labels, err := eval.ReadItems(filepath.Join(s.Eval(), eval.LabelFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	keys, err := eval.ReadKeys(filepath.Join(s.Eval(), eval.KeyFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	a, _, _ := eval.Judged(labels, keys)
	if a.Pairs == 0 {
		return nil, nil
	}
	return a, nil
}

// judgeItems turns every judged statement in the store into a blind item.
func judgeItems(s *store.Store) ([]eval.Item, []eval.Key, error) {
	jobs, err := loadNormJobs(s)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	docs, err := loadDocs(s)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]*law.Document{}
	for _, d := range docs {
		byID[d.ID] = d
	}

	var items []eval.Item
	var keys []eval.Key
	for _, job := range jobs {
		for i := range job.Records {
			r := &job.Records[i]
			verdict := ""
			switch r.Status {
			case norm.StatusVerified:
				verdict = eval.LabelEntailed
			case norm.StatusRejected:
				verdict = eval.LabelNotEntailed
			default:
				continue // invalid never reached the judge
			}
			items = append(items, eval.Item{
				ID:          r.ID,
				ProvisionID: r.ProvisionID,
				Text:        window(byID[r.DocID], r.ProvisionID),
				Statement:   describe(&r.Statement),
				Quote:       r.Statement.Evidence.Quote,
			})
			keys = append(keys, eval.Key{ID: r.ID, Machine: verdict})
		}
	}
	return items, keys, nil
}

// window rebuilds the text the judge was given, which is the provision plus the
// context BuildWindow adds. Handing a person the provision alone would measure
// the window rather than the judge: half these clauses are list items whose
// bearer is in the article stem above them, and a reader without the stem marks
// a correct statement wrong for naming a party the fragment does not.
func window(doc *law.Document, provisionID string) string {
	if doc == nil {
		return ""
	}
	w, err := extract.BuildWindow(doc, provisionID)
	if err != nil {
		return ""
	}
	return w.Prompt()
}

// describe renders a statement as one line a person can read against the
// provision without decoding JSON.
func describe(s *norm.Statement) string {
	var b strings.Builder
	b.WriteString(s.Type + ": ")
	if s.Bearer != nil {
		b.WriteString(s.Bearer.Text + " ")
	}
	b.WriteString(s.Action.Text)
	if s.Object != nil && s.Object.Text != "" {
		b.WriteString(" [" + s.Object.Text + "]")
	}
	if s.Counterparty != nil && s.Counterparty.Text != "" {
		b.WriteString(" -> " + s.Counterparty.Text)
	}
	for _, c := range s.Conditions {
		b.WriteString(" | if " + c.Text)
	}
	for _, e := range s.Exceptions {
		b.WriteString(" | unless " + e.Text)
	}
	if s.Deadline != nil {
		b.WriteString(" | by " + s.Deadline.Text)
	}
	if s.Sanction != nil {
		b.WriteString(" | else " + s.Sanction.Text)
	}
	return b.String()
}
