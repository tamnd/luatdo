package parse

import (
	"strings"
	"testing"
)

func TestHTMLText(t *testing.T) {
	source := `<!DOCTYPE html><html><head><title>bỏ qua</title>` +
		`<style>p{color:red}</style></head><body>` +
		`<!-- ghi chú --><script>var a = 1 > 0;</script>` +
		`<p class="pNoiDung">Điều 1. Phạm&nbsp;vi điều chỉnh</p>` +
		`<p title="a > b">Luật này quy định về &quot;quyền&quot; &amp; nghĩa vụ.</p>` +
		`<p></p><p></p>` +
		`<table><tr><td>1.</td><td>Khoản một</td></tr></table>` +
		`</body></html>`
	// Opening and closing a block tag each end a line, so blocks come out
	// separated by one blank line. The document parser is anchored to line
	// starts, so the separation costs nothing and keeps paragraphs apart.
	got := HTMLText(source)
	want := strings.Join([]string{
		"Điều 1. Phạm vi điều chỉnh",
		"",
		`Luật này quy định về "quyền" & nghĩa vụ.`,
		"",
		"1.",
		"",
		"Khoản một",
	}, "\n")
	if got != want {
		t.Errorf("HTMLText =\n%q\nwant\n%q", got, want)
	}
}

func TestHTMLTextDropsMarkupOnly(t *testing.T) {
	for _, source := range []string{
		"<script>alert('Điều 1')</script>",
		"<style>.a{content:'Điều 1'}</style>",
		"<head><title>Điều 1</title></head>",
	} {
		if got := HTMLText(source); got != "" {
			t.Errorf("HTMLText(%q) = %q, markup is not document text", source, got)
		}
	}
}

func TestHTMLTextSurvivesMalformedMarkup(t *testing.T) {
	// A scanner cannot fail on broken markup, it can only produce slightly
	// worse text, which is the whole reason this is not a parser.
	cases := map[string]string{
		"<p>Điều 1<p>Điều 2":          "Điều 1\nĐiều 2",
		"Điều 1 <p title='chưa đóng":  "Điều 1",
		"<p>Điều 1</p><!-- chưa đóng": "Điều 1",
		"":                            "",
		"<>":                          "",
	}
	for source, want := range cases {
		if got := HTMLText(source); got != want {
			t.Errorf("HTMLText(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestHTMLTextCollapsesBlankRuns(t *testing.T) {
	got := HTMLText("<div>Điều 1</div><div></div><div></div><div></div><div>Điều 2</div>")
	if got != "Điều 1\n\nĐiều 2" {
		t.Errorf("HTMLText = %q, want one blank line between the two", got)
	}
	if strings.HasPrefix(got, "\n") || strings.HasSuffix(got, "\n") {
		t.Error("the rendered text must not open or close with a blank line")
	}
}
