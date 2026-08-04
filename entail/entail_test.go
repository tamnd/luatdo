package entail

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/luatdo/norm"
)

const provision = "Người sử dụng lao động phải trả lương đúng hạn cho người lao động. Người lao động không được tiết lộ bí mật kinh doanh."

func duty() *norm.Statement {
	return &norm.Statement{
		Type:       "duty",
		Modality:   "obligation",
		Bearer:     &norm.Ref{Text: "người sử dụng lao động", ClassID: "vn-legal:Employer", IsActor: true},
		Action:     norm.Ref{Text: "trả lương đúng hạn"},
		Object:     &norm.Ref{Text: "lương"},
		Evidence:   norm.Evidence{Quote: "Người sử dụng lao động phải trả lương đúng hạn"},
		Confidence: 0.9,
	}
}

func TestFeaturesReadTheProvision(t *testing.T) {
	f := Features(provision, duty())
	for _, want := range []string{"type=duty", "bearer_cover=all", "action_cover=all", "quote_verbatim=yes", "text_has=phai"} {
		if !slices.Contains(f, want) {
			t.Errorf("missing feature %q in %v", want, f)
		}
	}
	if slices.Contains(f, "duty_without_obligation_marker") {
		t.Error("the provision says phải, so the duty is marked")
	}
}

func TestFeaturesNoticeUngroundedClaims(t *testing.T) {
	s := duty()
	s.Bearer = &norm.Ref{Text: "cơ quan bảo hiểm xã hội", IsActor: true}
	s.Action = norm.Ref{Text: "thu hồi giấy phép"}
	s.Evidence = norm.Evidence{Quote: "cơ quan bảo hiểm xã hội thu hồi giấy phép"}
	f := Features(provision, s)
	for _, want := range []string{"bearer_cover=none", "action_cover=none", "quote_verbatim=no"} {
		if !slices.Contains(f, want) {
			t.Errorf("missing feature %q in %v", want, f)
		}
	}
}

// Coverage matches whole folded words. Vietnamese words are short and folding
// makes them shorter, so a substring test would find cấm inside camera and
// report a claim as grounded because a longer unrelated word contains it.
func TestCoverMatchesWholeWords(t *testing.T) {
	if c := cover("-lap-dat-camera-giam-sat-", "cấm"); c != 0 {
		t.Errorf("cover = %v, cam inside camera is not the word cấm", c)
	}
	if c := cover("-nghiem-cam-hanh-vi-nay-", "cấm"); c != 1 {
		t.Errorf("cover = %v, the word is there", c)
	}
}

func TestFeaturesAreStable(t *testing.T) {
	a := Features(provision, duty())
	b := Features(provision, duty())
	if !slices.Equal(a, b) {
		t.Fatalf("features differ between calls:\n%v\n%v", a, b)
	}
}

// A gate has to be able to learn something a person can read back off it.
func TestTrainSeparatesGroundedFromUngrounded(t *testing.T) {
	var instances []Instance
	for i := range 20 {
		good := duty()
		bad := duty()
		bad.Bearer = &norm.Ref{Text: "cơ quan thuế", IsActor: true}
		bad.Action = norm.Ref{Text: "thu hồi giấy phép"}
		bad.Evidence = norm.Evidence{Quote: "cơ quan thuế thu hồi giấy phép"}
		p := fmt.Sprintf("vn:law:p%02d", i)
		instances = append(instances,
			Make(p, p+":a", provision, good, true, SourceJudge),
			Make(p, p+":b", provision, bad, false, SourceJudge))
	}
	g := Train(instances, 10)
	if g.TrainedOn != 40 || g.Positives != 20 || g.Source != SourceJudge {
		t.Fatalf("gate = %+v", g)
	}
	if got := g.Score(provision, duty()); got <= 0 {
		t.Errorf("grounded score = %v, want positive", got)
	}
	bad := duty()
	bad.Action = norm.Ref{Text: "thu hồi giấy phép"}
	bad.Bearer = &norm.Ref{Text: "cơ quan thuế", IsActor: true}
	bad.Evidence = norm.Evidence{Quote: "cơ quan thuế thu hồi giấy phép"}
	if got := g.Score(provision, bad); got >= 0 {
		t.Errorf("ungrounded score = %v, want negative", got)
	}
	// Same instances in a different order, same model, because the training
	// order is fixed inside Train rather than left to the caller.
	shuffled := append([]Instance(nil), instances...)
	slices.Reverse(shuffled)
	other := Train(shuffled, 10)
	if fmt.Sprint(other.Weights) != fmt.Sprint(g.Weights) {
		t.Error("training is order dependent, so two machines can disagree about the model")
	}
	if other.TeacherHash != g.TeacherHash {
		t.Errorf("fingerprint %s != %s", other.TeacherHash, g.TeacherHash)
	}
}

func TestVerdictBands(t *testing.T) {
	g := &Gate{Weights: map[string]float64{"bias": 1}, Accept: 2, Reject: -2, Accepts: true, Rejects: true}
	g.Weights = map[string]float64{}
	cases := []struct {
		score float64
		want  string
	}{{3, norm.GateAccept}, {2, norm.GateAccept}, {0, norm.GateJudge}, {-2, norm.GateReject}, {-9, norm.GateReject}}
	for _, c := range cases {
		g.Weights = map[string]float64{"bias": c.score}
		v := g.Verdict("id", provision, duty())
		if v.Decision != c.want {
			t.Errorf("score %v decided %q, want %q", c.score, v.Decision, c.want)
		}
	}
	// With the bands off the gate decides nothing, which is the milestone's
	// stated acceptable outcome and has to be representable.
	g.Accepts, g.Rejects = false, false
	g.Weights = map[string]float64{"bias": 99}
	if v := g.Verdict("id", provision, duty()); v.Decision != norm.GateJudge {
		t.Errorf("decision = %q with both bands off", v.Decision)
	}
}

func TestAuditIsFixedAndSampled(t *testing.T) {
	g := &Gate{Weights: map[string]float64{"bias": 9}, Accept: 1, Accepts: true, Audit: 50}
	audited := 0
	for i := range 200 {
		v := g.Verdict(fmt.Sprintf("vn:norm:%03d", i), provision, duty())
		if v.Audited {
			audited++
		}
		if again := g.Verdict(fmt.Sprintf("vn:norm:%03d", i), provision, duty()); again.Audited != v.Audited {
			t.Fatalf("record %d audited differently on a second run", i)
		}
	}
	if audited < 70 || audited > 130 {
		t.Errorf("audited %d of 200 at a 50 percent share", audited)
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	g := &Gate{
		Weights: map[string]float64{"bias": 1.5, "type=duty": -0.25},
		Accept:  1, Reject: -1, Accepts: true, Rejects: true,
		Audit: 10, Budget: 0.05, Epochs: 10, TrainedOn: 3, Positives: 2,
		TeacherHash: "abc", Source: SourceJudge,
	}
	var buf bytes.Buffer
	if err := g.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"f": "bias"`) {
		t.Fatalf("weights are not written as readable pairs:\n%s", buf.String())
	}
	back, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(back) != fmt.Sprint(g) {
		t.Errorf("round trip changed the gate:\n%+v\n%+v", back, g)
	}
}

func TestCalibrateRespectsTheBudget(t *testing.T) {
	// Ten entailed above zero, ten not entailed below, and one of each on the
	// wrong side. At a budget of zero the bands have to clear both intruders.
	var scored []Scored
	for i := range 10 {
		scored = append(scored, Scored{Instance: Instance{Entailed: true}, Score: float64(i + 1)})
		scored = append(scored, Scored{Instance: Instance{Entailed: false}, Score: -float64(i + 1)})
	}
	scored = append(scored,
		Scored{Instance: Instance{Entailed: false}, Score: 6},
		Scored{Instance: Instance{Entailed: true}, Score: -6})

	b := Calibrate(scored, 0)
	if !b.Accepts || b.Accept <= 6 {
		t.Errorf("accept band = %+v, it has to sit above the false positive at 6", b)
	}
	if !b.Rejects || b.Reject >= -6 {
		t.Errorf("reject band = %+v, it has to sit below the false negative at -6", b)
	}
	loose := Calibrate(scored, 0.2)
	if loose.Accept > b.Accept || loose.Reject < b.Reject {
		t.Errorf("a looser budget produced narrower bands: %+v against %+v", loose, b)
	}
}

func TestCalibrateGivesUpWhenNothingSeparates(t *testing.T) {
	var scored []Scored
	for i := range 20 {
		scored = append(scored, Scored{Instance: Instance{Entailed: i%2 == 0}, Score: float64(i % 3)})
	}
	b := Calibrate(scored, 0)
	if b.Accepts || b.Rejects {
		t.Errorf("bands = %+v over data where the classes are interleaved", b)
	}
}

func TestEvaluateGroupsFoldsByProvision(t *testing.T) {
	var instances []Instance
	for i := range 60 {
		p := fmt.Sprintf("vn:law:p%02d", i)
		good, bad := duty(), duty()
		bad.Action = norm.Ref{Text: "thu hồi giấy phép"}
		bad.Bearer = &norm.Ref{Text: "cơ quan thuế", IsActor: true}
		bad.Evidence = norm.Evidence{Quote: "cơ quan thuế thu hồi giấy phép"}
		instances = append(instances,
			Make(p, p+":a", provision, good, true, SourceJudge),
			Make(p, p+":b", provision, bad, false, SourceJudge))
	}
	r := Evaluate(instances, 5, 10, 0.05)
	if r.Instances != 120 || r.Provision != 60 || r.Outcome.Instances != 120 {
		t.Fatalf("report = %+v", r)
	}
	if r.Outcome.Accuracy() < 0.9 {
		t.Errorf("accuracy %.3f on separable data", r.Outcome.Accuracy())
	}
	if len(r.Bands) != 5 {
		t.Errorf("%d folds calibrated, want 5", len(r.Bands))
	}
	out := r.String()
	for _, want := range []string{"false rejects", "false accepts", "bands", "vocabulary"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report never mentions %q:\n%s", want, out)
		}
	}
}

func TestMeanKeepsABandOnlyIfEveryFoldFoundOne(t *testing.T) {
	accept, reject, accepts, rejects := Mean([]Bands{
		{Accept: 2, Reject: -2, Accepts: true, Rejects: true},
		{Accept: 4, Reject: -4, Accepts: true, Rejects: false},
	})
	if accept != 3 || reject != -3 {
		t.Errorf("edges = %v and %v, want the mean", accept, reject)
	}
	if !accepts || rejects {
		t.Errorf("accepts=%v rejects=%v, one fold found no reject band", accepts, rejects)
	}
	if _, _, a, r := Mean(nil); a || r {
		t.Error("no folds is no bands")
	}
}

func TestMeasureCountsBothMistakesSeparately(t *testing.T) {
	g := &Gate{Weights: map[string]float64{"bias": 0}, Accept: 1, Reject: -1, Accepts: true, Rejects: true}
	in := func(entailed bool, score float64) Instance {
		return Instance{Features: []string{fmt.Sprint(score)}, Entailed: entailed}
	}
	g.Weights = map[string]float64{"2": 2, "-2": -2, "0": 0}
	o := Measure(g, []Instance{
		in(true, 2), in(false, 2), in(true, -2), in(false, -2), in(true, 0),
	})
	if o.Accepted != 2 || o.Rejected != 2 || o.Escalated != 1 {
		t.Fatalf("triage = %+v", o)
	}
	if o.FalseAccepts != 1 || o.FalseRejects != 1 {
		t.Fatalf("mistakes = %+v", o)
	}
	if o.FalseAcceptRate() != 0.5 || o.FalseRejectRate() != 1.0/3 {
		t.Errorf("rates = %v and %v", o.FalseAcceptRate(), o.FalseRejectRate())
	}
}

func TestHeaviestIsSortedByWeight(t *testing.T) {
	got := Heaviest(map[string]float64{"a": 0.5, "b": -2, "c": 1}, 2)
	if len(got) != 2 || !strings.HasSuffix(got[0], "b") || !strings.HasSuffix(got[1], "c") {
		t.Errorf("heaviest = %v", got)
	}
}
