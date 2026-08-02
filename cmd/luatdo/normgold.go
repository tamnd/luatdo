package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/coverage"
	"github.com/tamnd/luatdo/norm"
	"github.com/tamnd/luatdo/store"
)

// The norm gold set is drawn, checked and scored here. It is a separate subset
// from the concept one because it measures a different thing: whether a clause
// was read as the norm it states, and in particular whether the word được was
// read as the sense it carries.

// CandidateFile is where a draw lands, to be annotated by hand into the gold
// file beside it.
const candidateFile = "gold_norm_candidates.jsonl"

func normGold(args []string) error {
	fs := flag.NewFlagSet("norms gold", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	n := fs.Int("n", 120, "clauses to draw")
	duocShare := fs.Int("duoc", 60, "percent of the draw that must use the word được")
	seed := fs.String("seed", "m11", "sampling seed, so a draw is reproducible")
	sub, _, err := parseSub(fs, args)
	if err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	switch sub {
	case "sample":
		return normGoldSample(s, *n, *duocShare, *seed)
	case "check":
		return normGoldCheck(s)
	case "score":
		return normGoldScore(s)
	default:
		return fmt.Errorf("usage: luatdo norms gold sample|check|score")
	}
}

// normGoldSample draws the clauses to annotate, ranked by a hash of the seed so
// the draw is reproducible on every machine in the fleet without a random
// source, and deliberately oversampling the word being measured.
//
// A draw proportional to the corpus would carry được in a handful of the
// hundred and twenty clauses, and a metric computed over a handful moves by
// twenty points when one annotation is argued about. The oversampling is
// declared rather than hidden: the report says how many of the drawn clauses
// use the word, so nobody reads the accuracy as a corpus wide rate.
func normGoldSample(s *store.Store, n, duocShare int, seed string) error {
	type candidate struct {
		id, docID, docType, text, hash string
		duoc                           bool
		rank                           uint64
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	var withDuoc, without []candidate
	types := map[string]bool{}
	for _, d := range docs {
		for _, p := range coverage.Extractable(d) {
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			sum := sha256.Sum256([]byte(seed + "\x00" + p.ID))
			c := candidate{
				id: p.ID, docID: d.ID, docType: d.DocType, text: p.Text, hash: p.TextHash,
				duoc: strings.Contains(strings.ToLower(p.Text), "được"),
				rank: binary.BigEndian.Uint64(sum[:8]),
			}
			types[d.DocType] = true
			if c.duoc {
				withDuoc = append(withDuoc, c)
			} else {
				without = append(without, c)
			}
		}
	}
	if len(withDuoc)+len(without) == 0 {
		return fmt.Errorf("no extractable provisions, run luatdo parse first")
	}
	byRank := func(c []candidate) {
		sort.Slice(c, func(i, j int) bool {
			if c[i].rank != c[j].rank {
				return c[i].rank < c[j].rank
			}
			return c[i].id < c[j].id
		})
	}
	byRank(withDuoc)
	byRank(without)

	wantDuoc := min(n*duocShare/100, len(withDuoc))
	drawn := append([]candidate(nil), withDuoc[:wantDuoc]...)
	drawn = append(drawn, without[:min(n-wantDuoc, len(without))]...)
	sort.Slice(drawn, func(i, j int) bool { return drawn[i].id < drawn[j].id })

	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, c := range drawn {
		if err := enc.Encode(norm.Gold{
			UnitID: c.id, DocID: c.docID, TextHash: c.hash, Text: c.text,
		}); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(s.Norms(), 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.Norms(), candidateFile)
	if err := store.WriteFile(path, []byte(b.String())); err != nil {
		return err
	}
	fmt.Printf("drew %d clauses of %d extractable across %d document types, seed %q\n",
		len(drawn), len(withDuoc)+len(without), len(types), seed)
	fmt.Printf("%d of them use được, which is %d percent of the draw and not the share the corpus has\n",
		wantDuoc, 100*wantDuoc/max(len(drawn), 1))
	fmt.Printf("wrote %s, annotate it by hand into %s before reading the score\n",
		path, filepath.Join(s.Norms(), norm.GoldFile))
	return nil
}

func normGoldCheck(s *store.Store) error {
	gold, err := norm.ReadGold(s.Norms())
	if err != nil {
		return err
	}
	if len(gold) == 0 {
		return fmt.Errorf("no gold set at %s, draw one with luatdo norms gold sample and annotate it by hand",
			filepath.Join(s.Norms(), norm.GoldFile))
	}
	problems := norm.CheckGold(gold)
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "gold:", p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d problems in the gold set, fix the annotations before scoring against them", len(problems))
	}
	fmt.Printf("%d annotated clauses, no problems\n", len(gold))
	return nil
}

// normGoldScore reads the annotations, the extraction jobs and the text the
// jobs read, and reports what the gold set says about the pass.
//
// The hash of the text each job read comes from the parsed documents rather
// than from the job, because that is what says whether the clause has been
// amended under the annotation. A pass that covered a unit is one that produced
// a job for it, found or not.
func normGoldScore(s *store.Store) error {
	gold, err := norm.ReadGold(s.Norms())
	if err != nil {
		return err
	}
	if len(gold) == 0 {
		return fmt.Errorf("no gold set at %s, draw one with luatdo norms gold sample and annotate it by hand",
			filepath.Join(s.Norms(), norm.GoldFile))
	}
	if problems := norm.CheckGold(gold); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "gold:", p)
		}
		return fmt.Errorf("%d problems in the gold set, fix the annotations before scoring against them", len(problems))
	}
	jobs, err := loadNormJobs(s)
	if err != nil {
		return fmt.Errorf("no norm jobs, run luatdo norms first: %w", err)
	}
	docs, err := loadDocs(s)
	if err != nil {
		return err
	}
	hashOf := map[string]string{}
	for _, d := range docs {
		for i := range d.Provisions {
			hashOf[d.Provisions[i].ID] = d.Provisions[i].TextHash
		}
	}
	covered := map[string]string{}
	var records []norm.Record
	for _, job := range jobs {
		covered[job.ProvisionID] = hashOf[job.ProvisionID]
		records = append(records, job.Records...)
	}
	fmt.Print(norm.Score(gold, records, covered))
	return nil
}
