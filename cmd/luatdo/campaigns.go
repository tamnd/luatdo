package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/luatdo/campaign"
	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/subject"
)

func init() {
	commands = append(commands,
		command{"campaign", "scope a named campaign and report what it has covered", cmdCampaign},
	)
}

func cmdCampaign(args []string) error {
	fs := flag.NewFlagSet("campaign", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	sub, rest, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	if sub == "list" || sub == "" {
		for _, name := range campaign.ScopeNames() {
			sc := campaign.Scopes[name]
			fmt.Printf("%-14s %s\n", name, sc.Note)
		}
		return nil
	}
	name := arg(rest, 0)
	if sub != "scope" && sub != "report" {
		return fmt.Errorf("usage: luatdo campaign list|scope <name>|report <name>")
	}
	if name == "" {
		return fmt.Errorf("usage: luatdo campaign %s <name>", sub)
	}
	sc, err := campaign.LookupScope(name)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	docs, inScope, err := campaignDocs(s, sc)
	if err != nil {
		return err
	}
	extractable, extracted, err := campaignCoverage(s, docs, inScope)
	if err != nil {
		return err
	}
	if sub == "scope" {
		total, done := 0, 0
		for id := range inScope {
			total += extractable[id]
			done += extracted[id]
		}
		fmt.Printf("campaign       %s, %s\n", sc.Name, sc.Note)
		fmt.Printf("documents      %d in scope of %d parsed\n", len(inScope), len(docs))
		fmt.Printf("provisions     %d extractable, %d already extracted, %d queued\n", total, done, total-done)
		return nil
	}

	records, err := loadTrusted(s)
	if err != nil {
		return err
	}
	var procedures []norm.Procedure
	path := filepath.Join(s.Trusted(), "procedures.json")
	if err := store.ReadJSON(path, &procedures); err != nil && !os.IsNotExist(err) {
		return err
	}
	report := campaign.Compile(sc, inScope, extractable, extracted, records, procedures, norm.Index(docs))
	summaries, err := loadSummaries(s)
	if err != nil {
		return err
	}
	report.Account(summaries)
	report.Calls, err = campaignCalls(s, inScope)
	if err != nil {
		return err
	}
	fmt.Print(report)
	out := filepath.Join(s.Campaign(), sc.Name+".json")
	if err := store.WriteJSON(out, report); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", out)
	return nil
}

// campaignDocs returns every parsed document and the identifiers of the ones in
// scope.
func campaignDocs(s *store.Store, sc campaign.Scope) ([]*law.Document, map[string]bool, error) {
	docs, err := loadDocs(s)
	if err != nil {
		return nil, nil, err
	}
	records, err := subject.ReadRecords(filepath.Join(s.Subject(), subject.AssignmentsFile))
	if err != nil {
		return nil, nil, fmt.Errorf("no subject assignments, run luatdo subjects first: %w", err)
	}
	return docs, sc.Documents(records, docs), nil
}

// campaignCoverage counts the extractable provisions of each document in scope
// and how many of them carry a job.
func campaignCoverage(s *store.Store, docs []*law.Document, inScope map[string]bool) (map[string]int, map[string]int, error) {
	done, err := jobFiles(s)
	if err != nil {
		return nil, nil, err
	}
	extractable, extracted := map[string]int{}, map[string]int{}
	for _, d := range docs {
		if !inScope[d.ID] {
			continue
		}
		for _, p := range coverage.Extractable(d) {
			extractable[d.ID]++
			if done[law.FileName(p.ID)] {
				extracted[d.ID]++
			}
		}
	}
	return extractable, extracted, nil
}

// jobFiles lists the norm job artifacts by file name. One directory listing
// answers the whole scope, the same way the coverage queue does it.
func jobFiles(s *store.Store) (map[string]bool, error) {
	entries, err := os.ReadDir(s.Norms())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	// The names go in whole. law.FileName appends the extension, so trimming
	// it here would build a set nothing ever matches and report a campaign
	// that has run as one that has not started.
	out := map[string]bool{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			out[e.Name()] = true
		}
	}
	return out, nil
}

// campaignCalls counts the model calls the jobs in scope made.
func campaignCalls(s *store.Store, inScope map[string]bool) (int, error) {
	jobs, err := loadNormJobs(s)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	calls := 0
	for _, job := range jobs {
		if inScope[job.DocID] {
			calls += campaign.ModelCalls(job)
		}
	}
	return calls, nil
}

func loadSummaries(s *store.Store) ([]campaign.Summary, error) {
	entries, err := os.ReadDir(s.Campaign())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []campaign.Summary
	for _, e := range entries {
		// The named report files live in the same directory and are not runs.
		if !strings.HasSuffix(e.Name(), ".json") || !isStamp(e.Name()) {
			continue
		}
		var sum campaign.Summary
		if err := store.ReadJSON(filepath.Join(s.Campaign(), e.Name()), &sum); err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, nil
}

// isStamp reports whether a file name is a run stamp, which is what the run
// command writes: eight digits, a dash, six digits.
func isStamp(name string) bool {
	base := strings.TrimSuffix(name, ".json")
	if len(base) != 15 || base[8] != '-' {
		return false
	}
	for i, r := range base {
		if i == 8 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
