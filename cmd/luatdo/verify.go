package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/entail"
	"github.com/tamnd/luatdo/eval"
	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/store"
)

func init() {
	commands = append(commands,
		command{"verify", "re-run a verification stage over what is already extracted", cmdVerify},
	)
}

// verify re-runs one stage of the verification pipeline over stored jobs.
//
// The stages are the ones in spec file 07 section 8 and they are ordered by
// what they cost. Schema and evidence are free and re-check stored records
// against today's rules, which matters because the rules have changed twice
// since the earliest jobs were written and nothing was re-checked. Entail is
// stage 5, the cheap gate, and it is the one this command was built for.
//
// Stage 6, the judge, is not here. It runs inside the norms pass, where the
// window and the correction loop live, and a second entry point to it would be
// a second implementation of the most consequential call in the project.
func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	campaignName := fs.String("campaign", "", "campaign scope, or empty for everything extracted")
	stage := fs.String("stage", "entail", "which stage to run: schema, evidence or entail")
	// Two percent is the default because the sweep put the held out cost of
	// every wider setting above what a silent deletion is worth: 0.02 saves
	// 7.9 percent of the judge calls and loses 2.2 percent of the true
	// statements, 0.05 saves 23.0 percent and loses 5.9. The number is a
	// default and not a law, which is why it is a flag.
	budget := fs.Float64("budget", 0.02, "the share of each class the gate's bands are allowed to get wrong")
	folds := fs.Int("folds", 5, "cross validation folds, grouped by provision")
	epochs := fs.Int("epochs", 10, "training epochs")
	audit := fs.Int("audit", 10, "percent of the gate's own decisions to send to the judge anyway")
	sub, _, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	switch *stage {
	case "schema", "evidence":
		return verifyCode(s, *campaignName, *stage)
	case "entail":
		switch sub {
		case "train":
			return entailTrain(s, *campaignName, *folds, *epochs, *budget, *audit)
		case "show":
			return entailShow(s)
		case "report", "":
			return entailReport(s, *campaignName)
		default:
			return fmt.Errorf("usage: luatdo verify --stage entail [train|report|show]")
		}
	}
	return fmt.Errorf("unknown stage %q, one of schema, evidence or entail", *stage)
}

// pair is one stored record with the window text its judge saw.
type pair struct {
	rec  *norm.Record
	text string
}

// storedPairs is every record of every job in scope, put back beside the
// provision window it was judged against.
//
// The window is rebuilt from the parsed document rather than stored in the job,
// so a document that has been reparsed since gives a different window and the
// record no longer matches. That is a real risk and the alternative is worse:
// caching the window in every record would make the job files several times
// larger and would hide reparsing rather than exposing it.
func storedPairs(s *store.Store, campaignName string) ([]pair, error) {
	jobs, err := loadNormJobs(s)
	if err != nil {
		return nil, err
	}
	docs, err := loadDocs(s)
	if err != nil {
		return nil, err
	}
	byID := map[string]*law.Document{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	inScope := map[string]bool{}
	if campaignName != "" {
		sc, err := campaign.LookupScope(campaignName)
		if err != nil {
			return nil, err
		}
		_, inScope, err = campaignDocs(s, sc)
		if err != nil {
			return nil, err
		}
	}
	windows := map[string]string{}
	var out []pair
	for _, job := range jobs {
		if campaignName != "" && !inScope[job.DocID] {
			continue
		}
		text, seen := windows[job.ProvisionID]
		if !seen {
			doc := byID[job.DocID]
			if doc == nil {
				return nil, fmt.Errorf("no parsed document %s, run luatdo parse", job.DocID)
			}
			w, err := extract.BuildWindow(doc, job.ProvisionID)
			if err != nil {
				return nil, err
			}
			text = w.Text
			windows[job.ProvisionID] = text
		}
		for i := range job.Records {
			out = append(out, pair{rec: &job.Records[i], text: text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rec.ProvisionID != out[j].rec.ProvisionID {
			return out[i].rec.ProvisionID < out[j].rec.ProvisionID
		}
		return out[i].rec.ID < out[j].rec.ID
	})
	return out, nil
}

// verifyCode runs stage 1 or stage 2 again over stored records.
//
// Both are the same walk with a different check, and both report failures as a
// list of provisions rather than as a rate, because a stored record failing a
// check it once passed is a defect somebody has to open rather than a
// percentage somebody can watch drift.
func verifyCode(s *store.Store, campaignName, stage string) error {
	pairs, err := storedPairs(s, campaignName)
	if err != nil {
		return err
	}
	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	checked, failed := 0, 0
	for _, p := range pairs {
		if p.rec.Status == norm.StatusInvalid {
			continue // it failed these checks when it was written and still says so
		}
		checked++
		var verr error
		if stage == "schema" {
			verr = norm.Validate(&p.rec.Statement, reg, p.text)
		} else {
			verr = evidenceOffsets(&p.rec.Statement, p.text)
		}
		if verr != nil {
			failed++
			fmt.Printf("%s %s: %v\n", p.rec.ProvisionID, p.rec.ID, verr)
		}
	}
	fmt.Printf("stage %s: %d records checked, %d failed\n", stage, checked, failed)
	if failed > 0 {
		return fmt.Errorf("%d stored records no longer pass stage %s", failed, stage)
	}
	return nil
}

// evidenceOffsets is stage 2 on its own: the quote sliced back out of the
// provision at the offsets the record carries, compared byte for byte.
func evidenceOffsets(s *norm.Statement, text string) error {
	e := s.Evidence
	if e.Quote == "" {
		return fmt.Errorf("no evidence quote")
	}
	if e.Start < 0 || e.End > len(text) || e.Start >= e.End {
		return fmt.Errorf("evidence offsets %d:%d are outside the provision, which is %d bytes", e.Start, e.End, len(text))
	}
	if got := text[e.Start:e.End]; got != e.Quote {
		return fmt.Errorf("the text at %d:%d is %q and the quote is %q", e.Start, e.End, got, e.Quote)
	}
	return nil
}

// entailInstances turns every judged record in scope into a labelled instance.
//
// Only records the judge actually ruled on are used. A record settled by a
// gate has no judge verdict, and training a gate on its own past decisions is
// how a model learns to agree with itself.
func entailInstances(s *store.Store, campaignName string) ([]entail.Instance, error) {
	pairs, err := storedPairs(s, campaignName)
	if err != nil {
		return nil, err
	}
	var out []entail.Instance
	for _, p := range pairs {
		if p.rec.Entailment == nil {
			continue
		}
		entailed := p.rec.Entailment.Verdict == norm.VerdictEntailed
		out = append(out, entail.Make(p.rec.ProvisionID, p.rec.ID, p.text, &p.rec.Statement, entailed, entail.SourceJudge))
	}
	return out, nil
}

// humanInstances turns the labelled judge sample into instances.
//
// The human labels are the only opinion in this project that did not come out
// of a model. They are never trained on, here or anywhere: a gate fitted to
// them has nothing left to be measured against, and fifty items is a
// measurement rather than a training set either way.
func humanInstances(s *store.Store, campaignName string) ([]entail.Instance, error) {
	labels, err := eval.ReadItems(filepath.Join(s.Eval(), eval.LabelFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	human := map[string]string{}
	for _, it := range labels {
		human[it.ID] = it.Human
	}
	pairs, err := storedPairs(s, campaignName)
	if err != nil {
		return nil, err
	}
	var out []entail.Instance
	for _, p := range pairs {
		switch human[p.rec.ID] {
		case eval.LabelEntailed:
			out = append(out, entail.Make(p.rec.ProvisionID, p.rec.ID, p.text, &p.rec.Statement, true, entail.SourceHuman))
		case eval.LabelNotEntailed:
			out = append(out, entail.Make(p.rec.ProvisionID, p.rec.ID, p.text, &p.rec.Statement, false, entail.SourceHuman))
		}
	}
	return out, nil
}

// entailTrain cross validates the design, then fits the shipped gate on
// everything and calibrates its bands on held out folds.
//
// The bands the shipped gate carries are the mean of the per fold bands rather
// than bands drawn on the whole set. Calibrating on the training data is how a
// gate ends up with a false rejection rate that is zero in the report and
// something else in production.
func entailTrain(s *store.Store, campaignName string, folds, epochs int, budget float64, audit int) error {
	instances, err := entailInstances(s, campaignName)
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		return fmt.Errorf("no judged records to learn from, run luatdo norms first")
	}
	report := entail.Evaluate(instances, folds, epochs, budget)
	fmt.Print(report)

	g := entail.Train(instances, epochs)
	g.Accept, g.Reject, g.Accepts, g.Rejects = entail.Mean(report.Bands)
	g.Budget, g.Audit, g.CalibratedOn = budget, audit, len(instances)
	path := filepath.Join(s.Eval(), entail.ModelFile)
	if err := os.MkdirAll(s.Eval(), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := g.Write(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println()
	if !g.Accepts && !g.Rejects {
		fmt.Println("both bands are off, so this gate decides nothing and every statement still costs a judge call")
		fmt.Println("that is the honest outcome when the folds could not agree on an edge inside the budget")
	}
	fmt.Printf("wrote %s\n", path)

	humans, err := humanInstances(s, campaignName)
	if err != nil {
		return err
	}
	fmt.Println()
	if len(humans) == 0 {
		fmt.Println("no human labels in this store, so the gate has been measured against the judge and against nothing else")
		fmt.Println("run luatdo eval judge sample and label it, or every number above is agreement with an unchecked instrument")
		return nil
	}
	// The gate saw these records in training, because they are a sample of the
	// same judged corpus. That makes this an upper bound on how well it reads
	// the law rather than a held out measurement, and saying so is the whole
	// point of reporting it separately.
	fmt.Printf("against %d human labels, which the gate trained on and which are therefore an upper bound:\n", len(humans))
	printOutcome(entail.Measure(g, humans))
	return nil
}

// entailReport says what the gate on disk would do to the records in scope
// without calling anything.
func entailReport(s *store.Store, campaignName string) error {
	g, err := readGate(s)
	if err != nil {
		return err
	}
	instances, err := entailInstances(s, campaignName)
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		return fmt.Errorf("no judged records in scope to compare against")
	}
	fmt.Printf("the gate on disk against %d judge verdicts in scope, which it was fitted on:\n", len(instances))
	o := entail.Measure(g, instances)
	printOutcome(o)
	saved := o.Accepted + o.Rejected
	fmt.Printf("on a rerun of this scope the gate would remove %d of %d judge calls, and the audit share would put %d of them back\n",
		saved, o.Instances, saved*g.Audit/100)
	humans, err := humanInstances(s, campaignName)
	if err != nil {
		return err
	}
	if len(humans) > 0 {
		fmt.Printf("\nagainst %d human labels:\n", len(humans))
		printOutcome(entail.Measure(g, humans))
	}
	return nil
}

// entailShow prints what the gate learned, which is the part of a distilled
// model that can be argued with.
func entailShow(s *store.Store) error {
	g, err := readGate(s)
	if err != nil {
		return err
	}
	fmt.Printf("trained on %d instances from the %s, %d of them entailed, %d epochs, %d features\n",
		g.TrainedOn, g.Source, g.Positives, g.Epochs, len(g.Weights))
	fmt.Printf("teacher fingerprint %s\n", g.TeacherHash)
	if g.Accepts {
		fmt.Printf("accept at %+.3f and above\n", g.Accept)
	} else {
		fmt.Println("no accept band: nothing is taken as entailed without a judge")
	}
	if g.Rejects {
		fmt.Printf("reject at %+.3f and below\n", g.Reject)
	} else {
		fmt.Println("no reject band: nothing is thrown away without a judge")
	}
	fmt.Printf("calibrated to a %.0f percent budget for each error, %d percent of decisions audited\n", g.Budget*100, g.Audit)
	fmt.Println("\nheaviest features:")
	for _, line := range entail.Heaviest(g.Weights, 40) {
		fmt.Println("  " + line)
	}
	return nil
}

// gateIfAsked loads the gate for an extraction run, and loads nothing unless
// the run asked for it.
//
// The gate is off by default and stays off until its measured error rates are
// worth its saving, which as of the milestone that built it they are not: the
// held out numbers say it removes a quarter of the judge calls and deletes
// about six percent of the true statements along the way. A caller that turns
// it on is told what it costs, on the run itself, because a saving printed
// without its price is the kind of number this project keeps trying not to
// print.
func gateIfAsked(s *store.Store, on bool) (*entail.Gate, error) {
	if !on {
		return nil, nil
	}
	g, err := readGate(s)
	if err != nil {
		return nil, err
	}
	fmt.Printf("gate: on, calibrated to a %.0f percent budget for each error, %d percent of its decisions audited\n",
		g.Budget*100, g.Audit)
	fmt.Println("gate: a rejected statement is deleted without a judge ever seeing it, and the measured false rejection rate is not zero")
	return g, nil
}

func readGate(s *store.Store) (*entail.Gate, error) {
	f, err := os.Open(filepath.Join(s.Eval(), entail.ModelFile))
	if err != nil {
		return nil, fmt.Errorf("no gate in this store, run luatdo verify --stage entail train: %w", err)
	}
	defer func() { _ = f.Close() }()
	return entail.Read(f)
}

func printOutcome(o entail.Outcome) {
	var b strings.Builder
	fmt.Fprintf(&b, "  %d labelled, %d entailed, %d not entailed\n", o.Instances, o.Entailed, o.NotEntailed)
	fmt.Fprintf(&b, "  agreement %.1f%%, precision %.1f%%, recall %.1f%%\n",
		o.Accuracy()*100, o.Precision()*100, o.Recall()*100)
	fmt.Fprintf(&b, "  triage %d accepted, %d rejected, %d left to the judge\n", o.Accepted, o.Rejected, o.Escalated)
	fmt.Fprintf(&b, "  false accepts %d of %d (%.1f%%), false rejects %d of %d (%.1f%%)\n",
		o.FalseAccepts, o.NotEntailed, o.FalseAcceptRate()*100,
		o.FalseRejects, o.Entailed, o.FalseRejectRate()*100)
	fmt.Print(b.String())
}
