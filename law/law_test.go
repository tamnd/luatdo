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

func TestProvisionID(t *testing.T) {
	got := ProvisionID("vn:law:2019:45-2019-qh14", "article", "94")
	want := "vn:law:2019:45-2019-qh14:article-94"
	if got != want {
		t.Errorf("ProvisionID = %q, want %q", got, want)
	}
	if got := ProvisionID(want, "clause", "1"); got != want+":clause-1" {
		t.Errorf("nested ProvisionID = %q", got)
	}
	if got := ProvisionID(want, "point", "đ"); got != want+":point-d" {
		t.Errorf("folded point ProvisionID = %q", got)
	}
}

func TestFileName(t *testing.T) {
	got := FileName("vn:law:2019:45-2019-qh14")
	want := "vn_law_2019_45-2019-qh14.json"
	if got != want {
		t.Errorf("FileName = %q, want %q", got, want)
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
