package norm

import (
	"regexp"
	"strconv"
	"strings"
)

// The shapes a Vietnamese deadline is written in. There are not many of them
// and they are stable, which is why this is a grammar rather than a model call:
// a model asked for a number will give a number every time, including for the
// phrases that do not have one.
var (
	amount = regexp.MustCompile(`(?i)(\d+)\s*(giờ|ngày làm việc|ngày|tháng|năm|tuần)`)
	// A deadline counted forward. "trong thời hạn 05 ngày làm việc kể từ ngày
	// nhận được yêu cầu" and "sau 30 ngày kể từ ngày ký" are the same shape.
	fromAnchor = regexp.MustCompile(`(?i)(?:kể từ|tính từ|từ)\s+(ngày|thời điểm|khi)\s+([^;.]*)`)
	// A deadline counted back. "chậm nhất 15 ngày trước ngày hết hạn".
	beforeAnchor = regexp.MustCompile(`(?i)trước\s+(ngày|thời điểm|khi)\s+([^;.]*)`)
	// A deadline that is a date rather than a length. "trước ngày 30 tháng 6".
	byDate = regexp.MustCompile(`(?i)(?:trước|chậm nhất là|chậm nhất vào)\s+ngày\s+(\d{1,2})\s+tháng\s+(\d{1,2})`)
	// The same length written in words. The older instruments spell every
	// number out, so a parser that only reads digits reads the 2019 labour code
	// and goes blind on the 2006 law next to it.
	numeral    = `(?:không|một|mốt|hai|ba|bốn|tư|năm|lăm|nhăm|sáu|bảy|bẩy|tám|chín|mười|mươi|trăm|nghìn|ngàn|linh|lẻ)`
	wordAmount = regexp.MustCompile(`(?i)\b((?:` + numeral + `\s+)*` + numeral + `)\s+(giờ|ngày làm việc|ngày|tháng|năm|tuần)\b`)
)

// digits are the Vietnamese ones. Several have two spellings and the second is
// not a variant a writer chooses freely: lăm is the five that follows a ten and
// mốt is the one that does, so mười lăm is written that way and mười năm is not.
var digits = map[string]int{
	"không": 0, "một": 1, "mốt": 1, "hai": 2, "ba": 3, "bốn": 4, "tư": 4,
	"năm": 5, "lăm": 5, "nhăm": 5, "sáu": 6, "bảy": 7, "bẩy": 7, "tám": 8, "chín": 9,
}

// spelled reads a Vietnamese number written in words.
//
// The ambiguity worth naming is năm, which is both the digit five and the word
// for year. It is resolved by position rather than by meaning: the pattern
// requires a unit word after the number, so in "năm ngày làm việc" the năm is a
// five and in "hai năm" it is the unit, and neither reading needs to know what
// the sentence is about.
func spelled(text string) (int, bool) {
	total, group, last := 0, 0, -1
	for _, w := range strings.Fields(strings.ToLower(text)) {
		if d, ok := digits[w]; ok {
			if last >= 0 {
				return 0, false // two bare digits in a row is not a number
			}
			last = d
			continue
		}
		switch w {
		case "mười":
			if last >= 0 {
				return 0, false
			}
			group += 10
		case "mươi", "trăm":
			if last < 0 {
				return 0, false
			}
			scale := 10
			if w == "trăm" {
				scale = 100
			}
			group, last = group+last*scale, -1
		case "nghìn", "ngàn":
			if last >= 0 {
				group += last
			}
			total, group, last = total+group*1000, 0, -1
		case "linh", "lẻ": // the zero tens of "một trăm linh năm"
		default:
			return 0, false
		}
	}
	if last >= 0 {
		group += last
	}
	return total + group, true
}

// units maps the Vietnamese word to the unit and the calendar it implies. A
// week is seven calendar days and is stored that way, because nothing in the
// corpus reasons in weeks and a separate unit for it would only mean every
// caller has one more case to forget.
var units = map[string]struct {
	unit     string
	calendar string
	scale    int
}{
	"giờ":           {UnitHour, CalendarNormal, 1},
	"ngày làm việc": {UnitDay, CalendarWorking, 1},
	"ngày":          {UnitDay, CalendarNormal, 1},
	"tuần":          {UnitDay, CalendarNormal, 7},
	"tháng":         {UnitMonth, CalendarNormal, 1},
	"năm":           {UnitYear, CalendarNormal, 1},
}

// ParseDeadline reads a deadline phrase into its parts, and reports whether it
// found anything worth storing.
//
// The order of the alternatives in the unit pattern matters and is the whole
// trick: ngày làm việc has to be tried before ngày, or every working day
// deadline in the corpus is recorded as a calendar day deadline and question 12
// answers with a set that is both too large and too small.
//
// A phrase with no number in it still parses when it names a date, because
// "chậm nhất là ngày 30 tháng 6" is a deadline that people miss. A phrase with
// neither is returned as text alone rather than discarded, so the graph keeps
// the drafter's words even where nothing here could take them apart.
func ParseDeadline(text string) (*Deadline, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, false
	}
	d := &Deadline{Text: trimmed}

	// The fixed date is read first and then cut out of the phrase. "chậm nhất là
	// ngày 30 tháng 6" contains a number followed by a unit word, so anything
	// that looks for a length before it looks for a date reads that deadline as
	// thirty months and files it under a threshold it never belonged to.
	rest := trimmed
	if m := byDate.FindStringSubmatchIndex(trimmed); m != nil {
		d.AnchorAt = AnchorBy
		d.Anchor = "ngày " + trimmed[m[2]:m[3]] + " tháng " + trimmed[m[4]:m[5]]
		rest = trimmed[:m[0]] + " " + trimmed[m[1]:]
	}

	// Digits first. An instrument that writes one deadline in digits and the
	// next in words is ordinary, so both are tried, and the digits win where a
	// phrase somehow carries the two.
	if m := amount.FindStringSubmatch(rest); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			d.set(n, m[2])
		}
	}
	if d.Unit == "" {
		if m := wordAmount.FindStringSubmatch(rest); m != nil {
			if n, ok := spelled(m[1]); ok && n > 0 {
				d.set(n, m[2])
			}
		}
	}

	if d.AnchorAt != "" {
		return d, true
	}
	switch {
	case beforeAnchor.MatchString(rest):
		m := beforeAnchor.FindStringSubmatch(rest)
		d.AnchorAt, d.Anchor = AnchorBefore, strings.TrimSpace(m[2])
	case fromAnchor.MatchString(rest):
		m := fromAnchor.FindStringSubmatch(rest)
		d.AnchorAt, d.Anchor = AnchorFrom, strings.TrimSpace(m[2])
	}
	return d, true
}

// set records a length against the unit word it was written with, and does
// nothing at all for a word this package has no unit for.
func (d *Deadline) set(n int, unit string) {
	u, known := units[strings.ToLower(unit)]
	if !known {
		return
	}
	d.Value, d.Unit, d.Calendar = n*u.scale, u.unit, u.calendar
}

// ShorterThan reports whether a deadline is shorter than a number of working
// days, which is the comparison question 12 asks.
//
// A calendar day deadline is compared against the working day threshold by
// treating five working days as seven calendar days, the ordinary week. That
// is an approximation and it is stated here rather than buried: the alternative
// is refusing to compare the two at all, which leaves the calendar day
// deadlines out of an answer they belong in.
func (d Deadline) ShorterThan(workingDays int) bool {
	days, ok := d.Days()
	if !ok {
		return false
	}
	if d.Calendar == CalendarWorking {
		return days < workingDays
	}
	return days < workingDays*7/5
}
