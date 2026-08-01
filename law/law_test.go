package law

import "testing"

func TestDocID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"45/2019/QH14", "vn:law:2019:45-2019-qh14"},
		{"15/2020/NĐ-CP", "vn:law:2020:15-2020-nd-cp"},
		{" 32/2013/QH13 ", "vn:law:2013:32-2013-qh13"},
		{"Hiến pháp 2013", "vn:constitution:2013:hien-phap-2013"},
	}
	for _, c := range cases {
		got, err := DocID(c.in)
		if err != nil {
			t.Fatalf("DocID(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("DocID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDocIDRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "45/2019", "not a number", "45-2019-QH14"} {
		if got, err := DocID(in); err == nil {
			t.Errorf("DocID(%q) = %q, want error", in, got)
		}
	}
}

func TestBodyRelative(t *testing.T) {
	local := []string{
		"01/2024/QĐ-UBND",
		"12/2014/NQ-HĐND",
		"72/2004/QĐ-UB",
		"02/2026/QĐ-CTUBND",
		"05/2016/NQ-HĐND8", // the council term is part of the abbreviation
		"07/2011/QĐ-UBND.", // trailing punctuation is in the corpus
	}
	for _, number := range local {
		if !BodyRelative(number) {
			t.Errorf("BodyRelative(%q) = false, every province issues this number", number)
		}
	}
	// These name one body each, so the number is already an identity. UBTVQH
	// and HĐBT are the traps: they start like a local abbreviation and are not.
	central := []string{
		"45/2019/QH14",
		"15/2020/NĐ-CP",
		"08/2021/TT-BTC",
		"1234/2011/NQ-UBTVQH12",
		"49/1991/QĐ-HĐBT",
		"Hiến pháp 2013",
	}
	for _, number := range central {
		if BodyRelative(number) {
			t.Errorf("BodyRelative(%q) = true, this number names one body already", number)
		}
	}
}

func TestDocIDIn(t *testing.T) {
	// A central number ignores the body, so a document keeps one identifier
	// however its issuer is spelled in a given dataset.
	central, err := DocIDIn("45/2019/QH14", "Quốc hội")
	if err != nil || central != "vn:law:2019:45-2019-qh14" {
		t.Fatalf("DocIDIn central = %q, %v", central, err)
	}

	longAn, err := DocIDIn("01/2024/QĐ-UBND", "UBND tỉnh Long An")
	if err != nil {
		t.Fatalf("DocIDIn: %v", err)
	}
	if longAn != "vn:law:2024:01-2024-qd-ubnd:ubnd-tinh-long-an" {
		t.Errorf("DocIDIn = %q, a local number carries the body that issued it", longAn)
	}
	// Case and spacing vary row by row in the corpus and must not fork one
	// province into two documents.
	if same, _ := DocIDIn("01/2024/QĐ-UBND", " UBND Tỉnh Long An "); same != longAn {
		t.Errorf("DocIDIn = %q, want %q from the same body spelled differently", same, longAn)
	}
	other, _ := DocIDIn("01/2024/QĐ-UBND", "UBND tỉnh Lạng Sơn")
	if other == longAn {
		t.Errorf("two provinces share the identifier %q", other)
	}
	if got, err := DocIDIn("01/2024/QĐ-UBND", ""); err == nil {
		t.Errorf("DocIDIn = %q, a local number with no body is not an identity", got)
	}
	if _, err := DocIDIn("khong-so", "UBND tỉnh Long An"); err == nil {
		t.Error("a number with no year has no identifier whoever issued it")
	}
}

func TestProvisionID(t *testing.T) {
	got := ProvisionID("vn:law:2019:45-2019-qh14", "article", "94")
	want := "vn:law:2019:45-2019-qh14:article-94"
	if got != want {
		t.Errorf("ProvisionID = %q, want %q", got, want)
	}
	if got := ProvisionID(want, "clause", "1"); got != want+":clause-1" {
		t.Errorf("nested ProvisionID = %q", got)
	}
	// Point d and point đ are two different points of the same clause, and one
	// identifier for both means whichever is parsed last answers for both.
	if got := ProvisionID(want, "point", "đ"); got != want+":point-dd" {
		t.Errorf("point đ ProvisionID = %q", got)
	}
	if ProvisionID(want, "point", "đ") == ProvisionID(want, "point", "d") {
		t.Error("point d and point đ share one identifier")
	}
}

func TestNumberSegment(t *testing.T) {
	cases := map[string]string{
		"94": "94", "15a": "15a", "IV": "iv", " 1 ": "1",
		"d": "d", "đ": "dd", "Đ": "dd",
		// The rest of the modified letters are spelled the same way, so a number
		// that reaches one of them is not a new decision later.
		"ă": "aw", "â": "aa", "ê": "ee", "ô": "oo", "ơ": "ow", "ư": "uw",
	}
	for in, want := range cases {
		if got := NumberSegment(in); got != want {
			t.Errorf("NumberSegment(%q) = %q, want %q", in, got, want)
		}
	}
	seen := map[string]string{}
	for _, letter := range []string{"a", "ă", "â", "d", "đ", "e", "ê", "o", "ô", "ơ", "u", "ư"} {
		seg := NumberSegment(letter)
		if other, ok := seen[seg]; ok {
			t.Errorf("%q and %q both spell %q", other, letter, seg)
		}
		seen[seg] = letter
	}
}

func TestFileName(t *testing.T) {
	got := FileName("vn:law:2019:45-2019-qh14")
	want := "vn_law_2019_45-2019-qh14.json"
	if got != want {
		t.Errorf("FileName = %q, want %q", got, want)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Người sử dụng lao động": "nguoi-su-dung-lao-dong",
		"Hoạt động ngân hàng":    "hoat-dong-ngan-hang",
		"Tổ chức tín dụng":       "to-chuc-tin-dung",
		"  Quỹ  đầu tư  ":        "quy-dau-tu",
		"ĐIỀU ƯỚC QUỐC TẾ":       "dieu-uoc-quoc-te",
		"":                       "",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRomanToArabic(t *testing.T) {
	cases := map[string]string{
		"I": "1", "IV": "4", "IX": "9", "XIV": "14", "XL": "40",
		"7": "7", "": "", "Chương": "Chương",
	}
	for in, want := range cases {
		if got := RomanToArabic(in); got != want {
			t.Errorf("RomanToArabic(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestISODate(t *testing.T) {
	cases := map[string]string{
		"17/08/2007":   "2007-08-17",
		" 01/06/2016 ": "2016-06-01",
		"2007-08-17":   "2007-08-17",
		// Anything else is a date nobody has, and a guessed one cannot be told
		// apart later from a date somebody read off the instrument.
		"1/6/2016":            "",
		"ngày 17 tháng 8":     "",
		"":                    "",
		"2007-08-17T00:00:00": "",
	}
	for in, want := range cases {
		if got := ISODate(in); got != want {
			t.Errorf("ISODate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestISODateOrdersTheWayTheCorpusDoesNot(t *testing.T) {
	// This is the whole reason the function exists. As written, the later date
	// sorts first, and a version graph built on that ordering applies
	// amendments backwards while looking finished.
	early, late := "17/08/2007", "01/06/2016"
	if early <= late {
		t.Fatal("the corpus form no longer misorders, so this test is testing nothing")
	}
	if ISODate(early) >= ISODate(late) {
		t.Errorf("ISODate(%q) = %q does not sort before ISODate(%q) = %q",
			early, ISODate(early), late, ISODate(late))
	}
}
