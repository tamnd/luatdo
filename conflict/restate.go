package conflict

import (
	"strings"
	"unicode"
)

// Restate writes a comparable form back out as a sentence.
//
// It exists for the baseline. A generated case is a real statement beside a
// mutated copy of it, and the copy has no sentence anywhere in the corpus
// because nobody ever wrote it. Several mutations also change the original
// side, by planting a condition or an interval on it, so its real sentence no
// longer says what the case says it says. Handing those two quotes to a model
// and asking whether they conflict measures nothing: the first run of the
// baseline scored zero on all sixty conflicting pairs because the second quote
// was a placeholder that stated no norm at all, which is a defect of the
// harness and not a finding about the model.
//
// So both sides are restated from the same fields, and the baseline reads two
// sentences that really do state the two norms being compared. That makes the
// baseline stronger than the quote version rather than weaker, because it also
// removes the extraction step: the model is given the norm the checker is
// working on, not the paragraph it was pulled out of. If the checker still
// wins, it wins against a baseline that was handed clean input.
//
// What it does not get is any of the checker's work. There is no operator name,
// no rule, no scope arithmetic and no slot label here, only Vietnamese.
func Restate(f *Form) string {
	if f == nil {
		return ""
	}
	party, act, object := f.Words()

	var b strings.Builder
	b.WriteString(upperFirst(unslug(party)))
	if w := operatorWord(f.Operator); w != "" {
		b.WriteString(" " + w)
	}
	if act != "" {
		b.WriteString(" " + unslug(act))
	}
	if object != "" {
		b.WriteString(" " + unslug(object))
	}
	if toward := or(f.Canon.Toward, f.Toward); toward != "" {
		b.WriteString(" cho " + unslug(toward))
	}
	if f.Deadline != nil {
		// The phrases are kept as the provision wrote them, and a provision that
		// already said "trong thời hạn" must not have it said again.
		if text := lead(f.Deadline.Text, "trong thời hạn"); text != "" {
			b.WriteString(" " + text)
		}
		// Many provisions write the event into the phrase itself, and repeating
		// it as an anchor produces "chậm nhất là năm ngày trước khi có những
		// thay đổi trên kể từ có những thay đổi trên".
		if anchor := lead(f.Deadline.Anchor, "kể từ"); anchor != "" && !said(f.Deadline.Text, f.Deadline.Anchor) {
			b.WriteString(" " + anchor)
		}
	}
	if len(f.Scope.Conditions) > 0 {
		b.WriteString(", khi " + joined(f.Scope.Conditions, " và "))
	}
	if len(f.Scope.Exceptions) > 0 {
		b.WriteString(", trừ trường hợp " + joined(f.Scope.Exceptions, " hoặc "))
	}
	b.WriteString(".")

	if s := or(f.Canon.Sanction, f.Sanction); s != "" {
		b.WriteString(" Vi phạm thì bị " + unslug(s) + ".")
	}
	if len(f.Scope.Defers) > 0 {
		// Instrument identifiers are left alone. They are references rather than
		// folded atoms, and pulling their hyphens apart would name a document
		// that does not exist.
		b.WriteString(" Việc này thực hiện theo " + strings.Join(f.Scope.Defers, ", ") + ".")
	}
	if v := validity(f.Scope); v != "" {
		b.WriteString(" " + v)
	}
	return b.String()
}

// operatorWord is how a provision says the modality out loud.
func operatorWord(op string) string {
	switch op {
	case Obligation:
		return "phải"
	case Prohibition:
		return "không được"
	case Permission:
		return "được"
	case Right:
		return "có quyền"
	}
	return ""
}

// validity says when the norm was in force, and says nothing when it was always
// in force, because a sentence about that would be noise on most of the corpus.
func validity(s Scope) string {
	switch {
	case s.From != "" && s.To != "":
		return "Quy định này có hiệu lực từ ngày " + s.From + " đến trước ngày " + s.To + "."
	case s.From != "":
		return "Quy định này có hiệu lực từ ngày " + s.From + "."
	case s.To != "":
		return "Quy định này hết hiệu lực từ ngày " + s.To + "."
	}
	return ""
}

// said reports whether the second fragment is already inside the first.
func said(text, part string) bool {
	part = strings.TrimSpace(part)
	return part != "" && strings.Contains(strings.ToLower(text), strings.ToLower(part))
}

// lead puts a phrase in front of a fragment that does not already open with it.
func lead(s, with string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(strings.ToLower(s), with) {
		return s
	}
	return with + " " + s
}

func joined(in []string, sep string) string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, unslug(s))
	}
	return strings.Join(out, sep)
}

// unslug turns a stored key back into words. The atoms are folded to hyphens
// without tones on the way in, and this only undoes the hyphens, so the reader
// gets "trong truong hop khan cap" rather than the tones it was written with.
// A model reads that correctly and a person can see what it came from.
func unslug(s string) string {
	return strings.ReplaceAll(s, "-", " ")
}

func upperFirst(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}
