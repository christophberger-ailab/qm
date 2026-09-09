package web

import (
	"regexp"
	"strings"

	"github.com/christophberger-ailab/qm/internal/project"
)

// The page tree shows a page's title as text, so the Quarto spans a title
// may be built from -- `# [Polizei-]{.pol}[Feuerwehr-]{.fw}Spickzettel` --
// have to be flattened into it. The preview marks those spans with the
// symbol its stylesheet gives their class (`.quarto.pol:before { content:
// "🚔" }`); the tree reads the very same rules out of the active
// stylesheet, so both panes speak the same alphabet and a user who edits
// custom.css changes the tree along with the preview.

var (
	// cssComment matches a /* ... */ comment, removed before the rules are
	// read so that a commented-out rule contributes no symbol.
	cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

	// cssSpanSymbol matches a `.quarto.<class>:before { ... content: "<symbol>" ... }`
	// rule. Both the CSS2 `:before` and the CSS3 `::before` spelling are
	// accepted, as is a single-quoted content string.
	cssSpanSymbol = regexp.MustCompile(
		`\.quarto\.([A-Za-z_-][A-Za-z0-9_-]*)\s*::?before\s*\{[^{}]*?content\s*:\s*` +
			`(?:"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)')`)

	// titleSpan matches a bracketed Quarto span with its attribute block,
	// e.g. `[Polizei-]{.pol}`. Nested brackets are not matched: a title is
	// a line of prose, not a place for a link inside a span.
	titleSpan = regexp.MustCompile(`\[([^\[\]]*)\]\{([^{}]*)\}`)
)

// spanSymbols reads the class-to-symbol map out of a preview stylesheet.
func spanSymbols(css string) map[string]string {
	out := map[string]string{}
	for _, m := range cssSpanSymbol.FindAllStringSubmatch(cssComment.ReplaceAllString(css, " "), -1) {
		content := m[2] + m[3] // exactly one of the two alternatives matched
		if sym := unescapeCSSString(content); sym != "" {
			out[m[1]] = sym
		}
	}
	return out
}

// unescapeCSSString resolves the backslash escapes a CSS string may carry.
// Only the literal `\<char>` form is handled -- the numeric `\1F693` form
// is left as written, because a stylesheet that spells its symbols out is
// the case worth serving here.
func unescapeCSSString(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// flattenSpans rewrites the Quarto spans in a title into plain text, each
// one replaced by its symbol followed by its text: `[Polizei-]{.pol}`
// becomes "🚔Polizei-". A span whose classes the stylesheet gives no
// symbol is left as it stands, so nothing the user wrote goes missing
// without a sign of it.
func flattenSpans(title string, symbols map[string]string) string {
	if len(symbols) == 0 {
		return title
	}
	return titleSpan.ReplaceAllStringFunc(title, func(span string) string {
		m := titleSpan.FindStringSubmatch(span)
		var prefix strings.Builder
		for _, class := range spanClasses(m[2]) {
			if sym, ok := symbols[class]; ok {
				prefix.WriteString(sym)
			}
		}
		if prefix.Len() == 0 {
			return span
		}
		return prefix.String() + m[1]
	})
}

// spanClasses lists the classes of a span's attribute block, in both
// spellings Pandoc accepts: the shorthand `{.pol}` and a bare word. Key
// and id attributes carry no symbol and are skipped.
func spanClasses(attrs string) []string {
	var out []string
	for _, token := range strings.Fields(attrs) {
		switch {
		case token == "" || strings.Contains(token, "=") || strings.HasPrefix(token, "#"):
		case strings.HasPrefix(token, "."):
			out = append(out, token[1:])
		default:
			out = append(out, token)
		}
	}
	return out
}

// flattenTreeSpans applies flattenSpans to every title in the page tree.
// It works on the tree the template is about to render, which is loaded
// afresh for each response and never written back to disk.
func flattenTreeSpans(pages []*project.Page, symbols map[string]string) {
	for _, p := range pages {
		p.Title = flattenSpans(p.Title, symbols)
		flattenTreeSpans(p.Children, symbols)
	}
}
