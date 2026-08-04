package retrieve

import (
	"strings"
	"unicode"
)

// Tokens folds text into the units the index counts.
//
// Vietnamese writes a word as one or more syllables separated by spaces, so a
// syllable alone is a weak token: "lao" and "động" each match a great deal that
// "lao động" does not. Every adjacent pair is indexed alongside the syllables
// for that reason, which is a cheap stand in for word segmentation and gets the
// common two syllable compounds right without a dictionary.
//
// Diacritics are kept. Folding them away would merge "phải" with "phái" and
// "thuế" with "thuê", and the first of those pairs is the word that carries
// obligation in half the corpus. The rest of the repository folds diacritics
// when it needs a stable identifier, which is a different job.
func Tokens(text string) []string {
	syllables := Syllables(text)
	out := make([]string, 0, len(syllables)*2)
	out = append(out, syllables...)
	for i := 0; i+1 < len(syllables); i++ {
		out = append(out, syllables[i]+"_"+syllables[i+1])
	}
	return out
}

// Syllables splits text into lowercase syllables, dropping punctuation.
func Syllables(text string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			flush()
		}
	}
	flush()
	return out
}
