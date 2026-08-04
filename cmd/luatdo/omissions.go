package main

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/extract"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/omission"
	"github.com/tamnd/luatdo/ontology"
	"github.com/tamnd/luatdo/store"
)

func init() {
	commands = append(commands,
		command{"omission", "audit what the pass never said anything about", cmdOmission},
	)
}

// cmdOmission runs the two omission measurements.
//
// markers is free and runs over everything extracted. recovery costs model
// calls and runs over a handful of provisions, because it re-extracts them
// several times over to find out what one pass misses.
func cmdOmission(args []string) error {
	fs := flag.NewFlagSet("omission", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	campaignName := fs.String("campaign", "", "campaign scope, or empty for everything extracted")
	gold := fs.Bool("gold", false, "run recovery over the annotated gold clauses of the campaign")
	limit := fs.Int("limit", 10, "how many provisions recovery re-extracts")
	population := fs.Int("population", 3, "independent candidates per provision in slow mode")
	corrections := fs.Int("max-corrections", 2, "bounded retries on invalid model output")
	dryRun := fs.Bool("dry-run", false, "print what recovery would re-extract and exit")
	sub, _, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	switch sub {
	case "markers", "":
		r, err := auditOmission(s, *campaignName)
		if err != nil {
			return err
		}
		fmt.Print(r)
		return nil
	case "recovery":
		return recovery(s, *campaignName, *gold, *limit, *population, *corrections, *dryRun)
	}
	return fmt.Errorf("usage: luatdo omission [markers|recovery] [--campaign name]")
}

// auditOmission runs the modality surface form check over stored jobs.
//
// The status a record carries here is the one the build settled on, not the one
// the job wrote, so a statement a person approved counts as covering its
// sentence. Without that the audit would report every human review decision as
// an omission.
func auditOmission(s *store.Store, campaignName string) (omission.Report, error) {
	var r omission.Report
	jobs, err := loadNormJobs(s)
	if err != nil {
		return r, err
	}
	docs, err := loadDocs(s)
	if err != nil {
		return r, err
	}
	byID := map[string]*law.Document{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	inScope := map[string]bool{}
	if campaignName != "" {
		sc, err := campaign.LookupScope(campaignName)
		if err != nil {
			return r, err
		}
		_, inScope, err = campaignDocs(s, sc)
		if err != nil {
			return r, err
		}
	}
	status := map[string]string{}
	if trusted, err := loadTrusted(s); err == nil {
		for _, rec := range trusted {
			status[rec.ID] = rec.Status
		}
	}
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].ProvisionID < jobs[j].ProvisionID })
	for _, job := range jobs {
		if campaignName != "" && !inScope[job.DocID] {
			continue
		}
		doc := byID[job.DocID]
		if doc == nil {
			return r, fmt.Errorf("no parsed document %s, run luatdo parse", job.DocID)
		}
		w, err := extract.BuildWindow(doc, job.ProvisionID)
		if err != nil {
			return r, err
		}
		records := append([]norm.Record(nil), job.Records...)
		for i := range records {
			if st, ok := status[records[i].ID]; ok {
				records[i].Status = st
			}
		}
		r.Provision(job.DocID, job.ProvisionID, w.Text, records)
	}
	return r, nil
}

// Recovery is what several independent extractions of one provision found that
// one extraction did not.
//
// It is written to its own file rather than over the job artifacts. The stored
// jobs are what the graph was built from, and a measurement that overwrites the
// thing it measures leaves the project unable to say what the graph contained
// when the number was taken.
type Recovery struct {
	Campaign   string               `json:"campaign"`
	Population int                  `json:"population"`
	Provisions []RecoveredUnit      `json:"provisions"`
	Records    []norm.Record        `json:"records"`
	Usage      extract.Verification `json:"verification"`
}

// RecoveredUnit is one provision run both ways.
type RecoveredUnit struct {
	ProvisionID string   `json:"provision_id"`
	DocID       string   `json:"doc_id"`
	Fast        int      `json:"fast"`      // claims fast mode had trusted
	Slow        int      `json:"slow"`      // claims slow mode verified
	Shared      int      `json:"shared"`    // claims both found
	Recovered   []string `json:"recovered"` // claims only slow mode found
	Lost        []string `json:"lost"`      // claims only fast mode found
	Calls       int      `json:"calls"`
}

// recovery re-extracts provisions in slow mode and reports what the extra
// populations found.
//
// The fast side is read off disk rather than re-run. Re-running it would
// measure two draws of the same distribution against each other, which is a
// different and much less interesting number than what the pass that built the
// graph actually has.
func recovery(s *store.Store, campaignName string, gold bool, limit, population, corrections int, dryRun bool) error {
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
	byProvision := map[string]*extract.NormJob{}
	for _, job := range jobs {
		byProvision[job.ProvisionID] = job
	}
	targets, err := recoveryTargets(s, campaignName, gold, byProvision)
	if err != nil {
		return err
	}
	if len(targets) > limit {
		fmt.Printf("%d provisions qualify and the limit is %d, so %d are left out and this is a sample of them\n",
			len(targets), limit, len(targets)-limit)
		targets = targets[:limit]
	}
	if len(targets) == 0 {
		return fmt.Errorf("no provisions with a stored job to compare against, run luatdo norms over them first")
	}
	if dryRun {
		for _, id := range targets {
			fmt.Println(id)
		}
		fmt.Printf("%d provisions, %d independent extractions each, plus a judge call per statement and a second on each survivor\n",
			len(targets), population)
		return nil
	}

	reg, err := ontology.Load(s.Ontology())
	if err != nil {
		return err
	}
	eng, err := openEngine()
	if err != nil {
		return err
	}
	runner := &extract.NormRunner{
		Completer: eng.completer, Model: eng.model, Registry: reg,
		MaxCorrections: corrections, Mode: "slow", Population: population,
	}
	out := Recovery{Campaign: campaignName, Population: population}
	for _, id := range targets {
		job := byProvision[id]
		doc := byID[job.DocID]
		slow, err := runner.Run(context.Background(), doc, id)
		if slow != nil {
			out.Records = append(out.Records, slow.Records...)
			out.Usage.Usage = addTokens(out.Usage.Usage, slow.Verification.Usage)
			out.Usage.Calls += slow.Verification.Calls
		}
		if err != nil {
			return err
		}
		unit := compare(job, slow)
		out.Provisions = append(out.Provisions, unit)
		fmt.Printf("%-52s fast %d, slow %d, recovered %d, lost %d\n", id, unit.Fast, unit.Slow, len(unit.Recovered), len(unit.Lost))
		for _, k := range unit.Recovered {
			fmt.Printf("  only slow mode found: %s\n", k)
		}
	}
	path := filepath.Join(s.Eval(), "recovery_"+or(campaignName, "all")+".json")
	if err := store.WriteJSON(path, out); err != nil {
		return err
	}
	fmt.Print(recoverySummary(out))
	fmt.Printf("wrote %s\n", path)
	return nil
}

// recoveryTargets is the provisions to re-extract, in a fixed order.
func recoveryTargets(s *store.Store, campaignName string, gold bool, byProvision map[string]*extract.NormJob) ([]string, error) {
	var ids []string
	if gold {
		annotated, err := norm.ReadGold(s.Norms(), campaignName)
		if err != nil {
			return nil, err
		}
		for _, g := range annotated {
			if byProvision[g.UnitID] != nil {
				ids = append(ids, g.UnitID)
			}
		}
		sort.Strings(ids)
		return ids, nil
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
	for id, job := range byProvision {
		if campaignName != "" && !inScope[job.DocID] {
			continue
		}
		if job.Mode == "slow" {
			continue // already several populations, nothing to recover against
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// compare puts the two runs of one provision side by side by claim key.
//
// The key is norm.Key, the same one the slow mode selector unions on, so two
// wordings of one claim count once. Only trusted statements count on either
// side: a claim slow mode found and the judge rejected is not a recovery, it is
// the pass working.
func compare(fast *extract.NormJob, slow *extract.NormJob) RecoveredUnit {
	u := RecoveredUnit{ProvisionID: fast.ProvisionID, DocID: fast.DocID}
	fastKeys := map[string]bool{}
	for i := range fast.Records {
		if fast.Records[i].Trusted() {
			fastKeys[norm.Key(&fast.Records[i].Statement)] = true
		}
	}
	slowKeys := map[string]bool{}
	if slow != nil {
		for i := range slow.Records {
			if slow.Records[i].Trusted() {
				slowKeys[norm.Key(&slow.Records[i].Statement)] = true
			}
		}
		u.Calls = campaign.ModelCalls(slow)
	}
	u.Fast, u.Slow = len(fastKeys), len(slowKeys)
	for k := range slowKeys {
		if fastKeys[k] {
			u.Shared++
			continue
		}
		u.Recovered = append(u.Recovered, k)
	}
	for k := range fastKeys {
		if !slowKeys[k] {
			u.Lost = append(u.Lost, k)
		}
	}
	sort.Strings(u.Recovered)
	sort.Strings(u.Lost)
	return u
}

// recoverySummary is the number the milestone asked for, with the two things
// that qualify it printed beside it.
//
// The lost column is not a rounding error and is why this is a comparison
// rather than a recall estimate. Slow mode unions its populations and then
// judges them, so a claim fast mode had can fail the second judge in slow mode
// and disappear, and a recovery rate quoted without that column would read as
// a free improvement.
func recoverySummary(r Recovery) string {
	var b strings.Builder
	var fast, slow, shared, recovered, lost, calls int
	for _, u := range r.Provisions {
		fast += u.Fast
		slow += u.Slow
		shared += u.Shared
		recovered += len(u.Recovered)
		lost += len(u.Lost)
		calls += u.Calls
	}
	fmt.Fprintf(&b, "recovery       %d provisions, %d populations each, %d model calls\n", len(r.Provisions), r.Population, calls)
	fmt.Fprintf(&b, "claims         %d trusted by the fast pass, %d by the slow one, %d in both\n", fast, slow, shared)
	fmt.Fprintf(&b, "recovered      %d claims only the extra populations found, %.1f%% of what slow mode ended with\n",
		recovered, percent(recovered, slow))
	fmt.Fprintf(&b, "lost           %d claims the fast pass had and slow mode did not keep\n", lost)
	fmt.Fprintf(&b, "reading        the fast pass over these provisions was missing about %.1f%% of what it could have had, at %.1f times the calls\n",
		percent(recovered, fast+recovered), ratioOf(calls, len(r.Provisions)))
	fmt.Fprintf(&b, "               this is a comparison of two extraction settings, not a recall figure, because both were judged by the same instrument\n")
	return b.String()
}

func percent(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func ratioOf(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func addTokens(a, b api.Usage) api.Usage {
	a.InputTokens += b.InputTokens
	a.CachedInputTokens += b.CachedInputTokens
	a.CacheWriteTokens += b.CacheWriteTokens
	a.OutputTokens += b.OutputTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.TotalTokens += b.TotalTokens
	return a
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
