package norm

import "testing"

func TestParseDeadlineTellsWorkingDaysFromCalendarDays(t *testing.T) {
	cases := []struct {
		text     string
		value    int
		unit     string
		calendar string
		anchorAt string
		anchor   string
	}{
		{"trong thời hạn 05 ngày làm việc kể từ ngày nhận được yêu cầu", 5, UnitDay, CalendarWorking, AnchorFrom, "nhận được yêu cầu"},
		{"trong thời hạn 30 ngày kể từ ngày ký", 30, UnitDay, CalendarNormal, AnchorFrom, "ký"},
		{"chậm nhất 15 ngày trước ngày hết hạn hợp đồng", 15, UnitDay, CalendarNormal, AnchorBefore, "hết hạn hợp đồng"},
		{"trong thời hạn 06 tháng", 6, UnitMonth, CalendarNormal, "", ""},
		{"48 giờ", 48, UnitHour, CalendarNormal, "", ""},
		{"02 tuần", 14, UnitDay, CalendarNormal, "", ""},
	}
	for _, c := range cases {
		got, ok := ParseDeadline(c.text)
		if !ok {
			t.Fatalf("%q did not parse", c.text)
		}
		if got.Value != c.value || got.Unit != c.unit || got.Calendar != c.calendar {
			t.Errorf("%q parsed as %d %s %s, want %d %s %s", c.text, got.Value, got.Unit, got.Calendar, c.value, c.unit, c.calendar)
		}
		if got.AnchorAt != c.anchorAt || got.Anchor != c.anchor {
			t.Errorf("%q anchored %q at %q, want %q at %q", c.text, got.Anchor, got.AnchorAt, c.anchor, c.anchorAt)
		}
	}
}

func TestParseDeadlineKeepsAPhraseItCannotTakeApart(t *testing.T) {
	got, ok := ParseDeadline("ngay sau khi phát hiện")
	if !ok || got.Text != "ngay sau khi phát hiện" {
		t.Fatalf("the drafter's words are kept even where nothing here parses them: %+v", got)
	}
	if _, hasDays := got.Days(); hasDays {
		t.Error("a phrase with no number in it must not acquire one")
	}
}

func TestParseDeadlineReadsAFixedDate(t *testing.T) {
	got, _ := ParseDeadline("chậm nhất là ngày 30 tháng 6 năm sau")
	if got.AnchorAt != AnchorBy || got.Anchor != "ngày 30 tháng 6" {
		t.Errorf("a deadline that is a date rather than a length: %+v", got)
	}
}

func TestShorterThanComparesBothCalendars(t *testing.T) {
	working, _ := ParseDeadline("03 ngày làm việc")
	if !working.ShorterThan(5) {
		t.Error("three working days is shorter than five working days")
	}
	calendar, _ := ParseDeadline("06 ngày")
	if !calendar.ShorterThan(5) {
		t.Error("six calendar days is inside the ordinary week that five working days spans")
	}
	long, _ := ParseDeadline("30 ngày")
	if long.ShorterThan(5) {
		t.Error("thirty calendar days is not shorter than five working days by any reading")
	}
}

func TestParseDeadlineReadsNumbersWrittenInWords(t *testing.T) {
	cases := map[string]struct {
		value    int
		unit     string
		calendar string
	}{
		"Trong thời hạn mười ngày":                        {10, UnitDay, CalendarNormal},
		"Trong thời hạn mười lăm ngày":                    {15, UnitDay, CalendarNormal},
		"Trong thời hạn hai mươi mốt ngày":                {21, UnitDay, CalendarNormal},
		"Trong thời hạn ba mươi ngày":                     {30, UnitDay, CalendarNormal},
		"Sau chín mươi ngày":                              {90, UnitDay, CalendarNormal},
		"trong thời gian một trăm tám mươi ngày":          {180, UnitDay, CalendarNormal},
		"Trong thời hạn năm ngày làm việc":                {5, UnitDay, CalendarWorking},
		"Chậm nhất là hai mươi ngày trước ngày khởi công": {20, UnitDay, CalendarNormal},
		"hợp đồng có thời hạn hai năm":                    {2, UnitYear, CalendarNormal},
	}
	for phrase, want := range cases {
		d, ok := ParseDeadline(phrase)
		if !ok {
			t.Fatalf("%q did not parse at all", phrase)
		}
		if d.Value != want.value || d.Unit != want.unit || d.Calendar != want.calendar {
			t.Errorf("%q = %d %s %s, want %d %s %s", phrase,
				d.Value, d.Unit, d.Calendar, want.value, want.unit, want.calendar)
		}
	}
}

func TestParseDeadlineLeavesWordsThatAreNotALengthAlone(t *testing.T) {
	for _, phrase := range []string{
		"trong ba số liên tiếp",
		"trong một thời hạn hợp lý do doanh nghiệp ấn định",
		"Định kỳ hằng năm, đột xuất",
	} {
		d, ok := ParseDeadline(phrase)
		if !ok {
			t.Fatalf("%q lost the drafter's words", phrase)
		}
		if _, has := d.Days(); has {
			t.Errorf("%q = %+v, a count of something that is not time is not a deadline", phrase, d)
		}
	}
}

func TestSpelledRefusesWhatIsNotANumber(t *testing.T) {
	for _, s := range []string{"hai ba", "mươi", "trăm", "ngày"} {
		if n, ok := spelled(s); ok {
			t.Errorf("spelled(%q) = %d, want a refusal", s, n)
		}
	}
}
