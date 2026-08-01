package parse

import (
	"html"
	"strings"
)

// blockTags are the tags that end a line of text. vbpl.vn renders a document
// as a run of paragraphs and tables, so paragraph and row boundaries are the
// only structure the HTML carries and the article and clause structure has to
// be recovered from the text itself, exactly as it is for the Markdown corpus.
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "tr": true, "td": true, "th": true,
	"li": true, "ul": true, "ol": true, "table": true, "section": true,
	"article": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "blockquote": true, "hr": true,
}

// dropTags are the tags whose content is markup, not document text.
var dropTags = map[string]bool{"script": true, "style": true, "head": true}

// HTMLText renders vbpl.vn HTML as plain text: tags become line breaks or
// nothing, entities are decoded, and runs of blank lines collapse. It is
// deliberately a scanner rather than a parser, because the input is generated
// HTML of one known shape and a scanner cannot fail on malformed markup, it
// can only produce slightly worse text.
func HTMLText(source string) string {
	var b strings.Builder
	b.Grow(len(source))
	skip := ""
	for i := 0; i < len(source); {
		c := source[i]
		if c != '<' {
			j := strings.IndexByte(source[i:], '<')
			if j < 0 {
				j = len(source) - i
			}
			if skip == "" {
				b.WriteString(source[i : i+j])
			}
			i += j
			continue
		}
		name, closing, end := scanTag(source, i)
		i = end
		if name == "" {
			continue
		}
		switch {
		case skip != "":
			if closing && name == skip {
				skip = ""
			}
		case dropTags[name] && !closing:
			skip = name
		case blockTags[name]:
			b.WriteByte('\n')
		}
	}
	return normalizeLines(html.UnescapeString(b.String()))
}

// scanTag reads the tag that starts at i and returns its lowercased name,
// whether it is a closing tag, and the index just past the tag. Quoted
// attribute values may contain the closing angle bracket, so they are skipped
// rather than searched through.
func scanTag(source string, i int) (name string, closing bool, end int) {
	j := i + 1
	if j < len(source) && source[j] == '/' {
		closing = true
		j++
	}
	if j < len(source) && source[j] == '!' {
		// A comment or a doctype: neither carries text.
		if k := strings.Index(source[j:], ">"); k >= 0 {
			return "", false, j + k + 1
		}
		return "", false, len(source)
	}
	start := j
	for j < len(source) && isNameByte(source[j]) {
		j++
	}
	name = strings.ToLower(source[start:j])
	quote := byte(0)
	for ; j < len(source); j++ {
		switch {
		case quote != 0:
			if source[j] == quote {
				quote = 0
			}
		case source[j] == '"' || source[j] == '\'':
			quote = source[j]
		case source[j] == '>':
			return name, closing, j + 1
		}
	}
	return name, closing, len(source)
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// normalizeLines collapses the whitespace runs inside each line, which is what
// clears the non breaking spaces vbpl.vn is full of, and collapses runs of
// blank lines to a single blank line.
func normalizeLines(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(strings.Join(strings.Fields(line), " "))
		if line == "" {
			if blank || len(out) == 0 {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, line)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}
