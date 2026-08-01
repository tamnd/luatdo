package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/tamnd/luatdo/law"
	"github.com/tamnd/luatdo/store"
	"github.com/tamnd/luatdo/subject"
)

func init() {
	commands = append(commands,
		command{"subjects", "file every document under the subject vocabulary", cmdSubjects},
		command{"sample", "draw a reproducible sample stratified over subject and instrument type", cmdSample},
	)
}

func cmdSubjects(args []string) error {
	fs := flag.NewFlagSet("subjects", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}
	v, err := subject.Load()
	if err != nil {
		return err
	}

	sum := subject.NewSummary()
	var records []subject.Record
	if err := eachDoc(s, func(doc *law.Document) error {
		if doc.Status == "quarantined" {
			// A quarantined document was never read, so filing it under a
			// subject would put a guess in the navigation tree and a document
			// nobody can open in the sampling frame.
			return nil
		}
		r := subject.Record{DocID: doc.ID, DocType: doc.DocType, Subjects: v.Classify(doc)}
		sum.Add(&r)
		records = append(records, r)
		return nil
	}); err != nil {
		return err
	}

	data, err := subject.Encode(records)
	if err != nil {
		return err
	}
	path := filepath.Join(s.Subject(), subject.AssignmentsFile)
	if err := store.WriteFile(path, data); err != nil {
		return err
	}
	if err := store.WriteJSON(filepath.Join(s.Subject(), subject.SummaryFile), sum); err != nil {
		return err
	}
	fmt.Println(sum)
	fmt.Printf("wrote %s\n", path)
	return nil
}

func cmdSample(args []string) error {
	fs := flag.NewFlagSet("sample", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory")
	// The default is a thousand because the corpus falls into a bit under a
	// thousand cells, and a draw smaller than the grid reaches only the largest
	// cells, which is the thing stratifying exists to avoid.
	n := fs.Int("n", 1000, "how many documents to draw")
	seed := fs.String("seed", "luatdo", "seed, so a named sample can be redrawn exactly")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := openStore(*dataDir)
	if err != nil {
		return err
	}

	path := filepath.Join(s.Subject(), subject.AssignmentsFile)
	records, err := subject.ReadRecords(path)
	if err != nil {
		return fmt.Errorf("no subject assignments, run luatdo subjects first: %w", err)
	}

	selection := subject.Sample(records, *n, *seed)
	data, err := subject.EncodeSelection(selection)
	if err != nil {
		return err
	}
	out := filepath.Join(s.Subject(), subject.SelectionFile)
	if err := store.WriteFile(out, data); err != nil {
		return err
	}

	drawn := map[subject.Stratum]bool{}
	for _, sel := range selection {
		drawn[sel.Stratum] = true
	}
	strata := subject.Strata(records)
	fmt.Printf("drew %d of %d documents, from %d of %d strata, seed %q\n",
		len(selection), len(records), len(drawn), len(strata), *seed)
	if len(strata) > len(selection) {
		fmt.Printf("%d strata got no place, ask for at least %d to reach every one\n",
			len(strata)-len(drawn), len(strata))
	}
	fmt.Printf("wrote %s\n", out)
	return nil
}
