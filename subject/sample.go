package subject

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
)

// Stratum is one cell of the sampling grid: a subject crossed with a document
// type. Both matter. Subject alone would let a sample of the land domain be
// all provincial decisions, and type alone would let a sample of circulars be
// all tax. A document with no subject sits in a stratum whose subject is the
// empty string, which is a real cell and gets sampled like any other, because
// the documents the classifier could not read are exactly the ones a campaign
// most needs to see.
type Stratum struct {
	Subject string `json:"subject"`
	DocType string `json:"doc_type"`
}

// Selection is one sampled document and the cell it came from.
type Selection struct {
	DocID   string  `json:"doc_id"`
	Stratum Stratum `json:"stratum"`
}

// Sample draws n documents spread over the strata the records fall into.
//
// The draw is reproducible from the seed and the record set alone. It uses no
// random source: each document is ranked inside its stratum by a hash of the
// seed and its identifier, and the sample is the front of that order. Two runs
// on the same corpus agree, a run on a corpus that gained a document keeps
// almost all of its previous picks, and no machine in the fleet disagrees with
// another about what a seeded generator produces.
//
// Every non empty stratum contributes at least one document, and the rest of
// the budget is shared out in proportion to stratum size by largest remainder.
// The floor is what makes it stratified rather than proportional: a domain with
// forty documents in a corpus of a hundred and twenty thousand would round to
// zero, and a sample that never sees atomic energy law cannot report on it.
func Sample(records []Record, n int, seed string) []Selection {
	if n <= 0 || len(records) == 0 {
		return nil
	}

	members := stratify(records)
	strata := Strata(records)
	quota := allocate(strata, members, n)

	var out []Selection
	for _, s := range strata {
		k := quota[s]
		if k == 0 {
			continue
		}
		ids := append([]string(nil), members[s]...)
		sort.Slice(ids, func(i, j int) bool {
			ri, rj := rank(seed, ids[i]), rank(seed, ids[j])
			if ri != rj {
				return ri < rj
			}
			return ids[i] < ids[j]
		})
		if k > len(ids) {
			k = len(ids)
		}
		for _, id := range ids[:k] {
			out = append(out, Selection{DocID: id, Stratum: s})
		}
	}
	return out
}

// stratify buckets the records by the cell they belong to.
func stratify(records []Record) map[Stratum][]string {
	members := map[Stratum][]string{}
	for i := range records {
		s := Stratum{Subject: Primary(&records[i]), DocType: records[i].DocType}
		members[s] = append(members[s], records[i].DocID)
	}
	return members
}

// Strata returns the cells the records fall into, in a fixed order. A caller
// that is about to draw a sample wants this first: a sample smaller than the
// number of cells cannot reach all of them, and it is better to say so than to
// let the caller believe a five hundred document draw covered the grid.
func Strata(records []Record) []Stratum {
	members := stratify(records)
	strata := make([]Stratum, 0, len(members))
	for s := range members {
		strata = append(strata, s)
	}
	sort.Slice(strata, func(i, j int) bool {
		if strata[i].Subject != strata[j].Subject {
			return strata[i].Subject < strata[j].Subject
		}
		return strata[i].DocType < strata[j].DocType
	})
	return strata
}

// allocate shares n out over the strata: one each while the budget allows, then
// the remainder in proportion to size, then the last few by largest remainder so
// the totals add up exactly rather than nearly.
func allocate(strata []Stratum, members map[Stratum][]string, n int) map[Stratum]int {
	quota := make(map[Stratum]int, len(strata))
	if n <= len(strata) {
		// Not enough budget for one each. The largest strata get the seats,
		// which keeps a small sample representative of where the corpus
		// actually is rather than of how many corners it has.
		order := append([]Stratum(nil), strata...)
		sort.SliceStable(order, func(i, j int) bool {
			return len(members[order[i]]) > len(members[order[j]])
		})
		for _, s := range order[:n] {
			quota[s] = 1
		}
		return quota
	}

	total := 0
	for _, s := range strata {
		quota[s] = 1
		total += len(members[s])
	}
	left := n - len(strata)

	type share struct {
		stratum   Stratum
		remainder float64
	}
	shares := make([]share, 0, len(strata))
	given := 0
	for _, s := range strata {
		exact := float64(left) * float64(len(members[s])) / float64(total)
		whole := int(exact)
		if room := len(members[s]) - 1; whole > room {
			whole = room
		}
		quota[s] += whole
		given += whole
		shares = append(shares, share{stratum: s, remainder: exact - float64(int(exact))})
	}

	// The remainders hand out the seats that rounding lost, and the pass
	// repeats because a stratum that is already full cannot take its seat and
	// somebody else has to. It stops when a whole pass places nothing, which is
	// the case where the corpus is smaller than the sample asked for.
	sort.SliceStable(shares, func(i, j int) bool { return shares[i].remainder > shares[j].remainder })
	for given < left {
		placed := false
		for _, sh := range shares {
			if given >= left {
				break
			}
			if quota[sh.stratum] >= len(members[sh.stratum]) {
				continue
			}
			quota[sh.stratum]++
			given++
			placed = true
		}
		if !placed {
			break
		}
	}
	return quota
}

// rank hashes a seed and a document identifier into a number. It is a hash
// rather than a counter so a document's position in its stratum does not depend
// on how many documents were parsed before it.
func rank(seed, id string) uint64 {
	sum := sha256.Sum256([]byte(seed + "\x00" + id))
	return binary.BigEndian.Uint64(sum[:8])
}
