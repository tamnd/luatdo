package relation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/luatdo/api"
)

// Thresholds are what a relation has to clear before it stops being a claim one
// provision made and starts being something the corpus shows.
//
// The numbers are small and the reason they can be is that they count the right
// things. Forty provisions of one decree are one drafter's habit, so documents
// are counted apart from provisions, and a relation supported inside a single
// document never promotes on volume alone.
type Thresholds struct {
	MinProvisions int
	MinDocs       int
}

// DefaultThresholds is two provisions in two documents. Two is not a large
// number and it is not meant to be: it is the smallest threshold that makes a
// single confident hallucination insufficient, and past that the defence stops
// being cheap and starts costing recall.
var DefaultThresholds = Thresholds{MinProvisions: 2, MinDocs: 2}

// Fold merges every sighting of one relation into one edge and decides, by
// counting, whether it has earned canonical status.
//
// This is source 4.3. A corpus level relation is never asserted from a single
// provision, it is proposed by deterministic aggregation over provision level
// ones, and the model that confirms it is shown the aggregate rather than any
// one sentence.
func Fold(edges []Edge, r *Registry, th Thresholds) []Edge {
	if th.MinProvisions < 1 {
		th.MinProvisions = 1
	}
	if th.MinDocs < 1 {
		th.MinDocs = 1
	}

	byKey := map[string]*Edge{}
	var order []string
	provisions := map[string]map[string]bool{}
	docs := map[string]map[string]bool{}
	confidence := map[string][]float64{}
	seenEvidence := map[string]map[string]bool{}

	for _, e := range edges {
		key := e.Key()
		agg := byKey[key]
		if agg == nil {
			clone := e
			clone.Evidence = nil
			clone.SupportCount, clone.SupportDocs = 0, 0
			byKey[key] = &clone
			agg = &clone
			order = append(order, key)
			provisions[key] = map[string]bool{}
			docs[key] = map[string]bool{}
			seenEvidence[key] = map[string]bool{}
		}
		// The strongest source wins. An edge the drafter wrote in a definition
		// and also used across ten provisions is still a definitional edge, and
		// downgrading it to corpus would lose the one fact that lets it be
		// canonical on its own.
		agg.Source = strongerSource(agg.Source, e.Source)
		if e.Definition != "" && agg.Definition == "" {
			agg.Definition = e.Definition
		}
		agg.Direction = mergeDirection(agg.Direction, e.Direction)
		if e.FirstSeen != "" && (agg.FirstSeen == "" || e.FirstSeen < agg.FirstSeen) {
			agg.FirstSeen = e.FirstSeen
		}
		if e.LastSeen > agg.LastSeen {
			agg.LastSeen = e.LastSeen
		}
		if e.OntologyVersion > agg.OntologyVersion {
			agg.OntologyVersion = e.OntologyVersion
		}
		confidence[key] = append(confidence[key], e.Confidence)

		for _, ev := range e.Evidence {
			fp := ev.ProvisionID + "\x00" + ev.Quote
			if seenEvidence[key][fp] {
				continue
			}
			seenEvidence[key][fp] = true
			agg.Evidence = append(agg.Evidence, ev)
			provisions[key][ev.ProvisionID] = true
			if ev.DocID != "" {
				docs[key][ev.DocID] = true
			}
		}
	}

	out := make([]Edge, 0, len(order))
	for _, key := range order {
		e := *byKey[key]
		e.SupportCount = len(provisions[key])
		e.SupportDocs = len(docs[key])
		// The mean rather than the maximum. Forty weak sightings are forty weak
		// sightings, and taking the best one would let volume launder
		// uncertainty into confidence.
		e.Confidence = mean(confidence[key])
		sort.SliceStable(e.Evidence, func(i, j int) bool {
			if e.Evidence[i].ProvisionID != e.Evidence[j].ProvisionID {
				return e.Evidence[i].ProvisionID < e.Evidence[j].ProvisionID
			}
			return e.Evidence[i].CharStart < e.Evidence[j].CharStart
		})
		if e.SupportCount > 1 && e.Source == SourceProvision {
			e.Source = SourceCorpus
		}
		e.Status, e.Why = promote(e, r, th)
		out = append(out, e)
	}
	Sort(out)
	return out
}

// promote applies the gates in the order a reader would want them reported: an
// unknown type first, because nothing else about the edge matters until the type
// is decided, then direction, then corroboration.
func promote(e Edge, r *Registry, th Thresholds) (string, string) {
	if r.Type(e.Type) == nil {
		return StatusProvisional, WhyUnknownType
	}
	if e.Direction == DirectionFlipped || e.Direction == DirectionDisputed {
		return StatusProvisional, WhyDirectionWrong
	}
	if e.Source == SourceDefinitional {
		// The drafter stated it in a definition clause. That is the one source
		// allowed to stand on a single provision, and it is allowed because a
		// person wrote it rather than because a model was confident about it.
		return StatusCanonical, ""
	}
	if e.SupportCount < th.MinProvisions || e.SupportDocs < th.MinDocs {
		return StatusProvisional, WhySingleSupport
	}
	return StatusCanonical, ""
}

// strongerSource ranks the three sources by how much they can be trusted to
// stand alone.
func strongerSource(a, b string) string {
	rank := map[string]int{SourceCorpus: 1, SourceProvision: 2, SourceDefinitional: 3}
	if rank[b] > rank[a] {
		return b
	}
	if a == "" {
		return b
	}
	return a
}

// mergeDirection combines verdicts across the sightings of one relation. Two
// sightings that read opposite ways are disputed rather than averaged, because
// the disagreement is the finding.
func mergeDirection(a, b string) string {
	if a == b {
		return a
	}
	if a == DirectionUnverified {
		return b
	}
	if b == DirectionUnverified {
		return a
	}
	if a == DirectionUnclear {
		return b
	}
	if b == DirectionUnclear {
		return a
	}
	return DirectionDisputed
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// Confirmer is the model step over an aggregate. It is shown the counts and the
// spread of quotes rather than one sentence, because the claim being checked is
// a claim about the corpus and checking it against one provision would be
// checking a different claim.
type Confirmer struct {
	Completer      api.Completer
	Model          string
	MaxCorrections int
}

type wireConfirmation struct {
	Holds      bool    `json:"holds"`
	Rationale  string  `json:"rationale"`
	Confidence float64 `json:"confidence"`
}

// Instructions is the corpus level confirmation prompt.
func (c *Confirmer) Instructions() string {
	var b strings.Builder
	b.WriteString("Bạn được cho một quan hệ giữa hai khái niệm pháp lý và các trích dẫn từ nhiều điều khoản khác nhau được cho là chứng minh quan hệ đó.\n")
	b.WriteString("Nhiệm vụ: xác định các trích dẫn này có thực sự cho thấy quan hệ đó hay không.\n\n")
	b.WriteString("Quy tắc bắt buộc:\n")
	b.WriteString("1. Chỉ căn cứ vào các trích dẫn được cung cấp.\n")
	b.WriteString("2. Nếu các trích dẫn chỉ cho thấy hai khái niệm cùng xuất hiện chứ không cho thấy quan hệ, trả về holds là false.\n")
	b.WriteString("3. Nếu quan hệ đúng nhưng ngược chiều, trả về holds là false và nói rõ trong rationale.\n")
	b.WriteString("4. rationale phải dẫn ra trích dẫn cụ thể làm căn cứ.\n\n")
	b.WriteString("Trả về đúng một đối tượng JSON, không giải thích, theo dạng:\n")
	b.WriteString(`{"holds":true,"rationale":"...","confidence":0.9}`)
	b.WriteString("\n")
	return b.String()
}

// ConfirmationPrompt renders one aggregated edge.
func ConfirmationPrompt(e Edge, r *Registry, fromLabel, toLabel string) string {
	var b strings.Builder
	definition := e.Definition
	if t := r.Type(e.Type); t != nil {
		definition = t.Definition
	}
	fmt.Fprintf(&b, "Quan hệ: %s\n", e.Type)
	fmt.Fprintf(&b, "Nghĩa: %s\n", definition)
	fmt.Fprintf(&b, "X: %s\n", fromLabel)
	fmt.Fprintf(&b, "Y: %s\n\n", toLabel)
	fmt.Fprintf(&b, "Số điều khoản chứng minh: %d, trong %d văn bản\n\n", e.SupportCount, e.SupportDocs)
	b.WriteString("Các trích dẫn:\n")
	for i, ev := range e.Evidence {
		if i >= MaxExamples {
			fmt.Fprintf(&b, "  và %d trích dẫn khác\n", len(e.Evidence)-MaxExamples)
			break
		}
		fmt.Fprintf(&b, "  [%s] %s\n", ev.ProvisionID, ev.Quote)
	}
	return b.String()
}

// Confirm asks whether the aggregate holds. A refusal demotes the edge to
// provisional rather than deleting it, so the evidence survives for whoever
// wants to argue.
func (c *Confirmer) Confirm(ctx context.Context, e Edge, r *Registry, fromLabel, toLabel string) (bool, string, api.Usage, error) {
	var usage api.Usage
	input := ConfirmationPrompt(e, r, fromLabel, toLabel)
	for attempt := 0; attempt <= maxCorrections(c.MaxCorrections); attempt++ {
		resp, err := c.Completer.Complete(ctx, api.Request{
			Model: c.Model, Instructions: c.Instructions(), Input: input,
		})
		if err != nil {
			return false, "", usage, err
		}
		usage = addUsage(usage, resp.Usage)

		var wire wireConfirmation
		if err := json.Unmarshal([]byte(stripFences(resp.Text)), &wire); err != nil {
			input = ConfirmationPrompt(e, r, fromLabel, toLabel) +
				"\nLần trả lời trước không phải một đối tượng JSON hợp lệ: " + err.Error() + "\nTrả lời lại.\n"
			continue
		}
		return wire.Holds, wire.Rationale, usage, nil
	}
	// An unreadable answer is not a confirmation.
	return false, "", usage, nil
}
