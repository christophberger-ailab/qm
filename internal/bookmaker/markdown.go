package bookmaker

import (
	"regexp"
	"strings"
)

// This file implements the Markdown-level surgery the bookmaker performs on
// each page:
//
//   - locate fenced code blocks so that nothing inside them is ever touched,
//   - track Pandoc fenced divs (`:::`) and their nesting,
//   - detach an opening div fence from the paragraph above it, which Pandoc
//     would otherwise swallow as text,
//   - shift ATX headings so that a page's top heading lands on the level
//     implied by its position in the folder hierarchy,
//   - attach explicit identifiers and classes to those top headings so that
//     in-book cross-links can be rewritten into anchors and the book title
//     can be kept out of the chapter numbering.

var (
	// atxHeading matches a CommonMark ATX heading: up to three leading
	// spaces, one to six hashes, then either end-of-line or whitespace.
	atxHeading = regexp.MustCompile(`^( {0,3})(#{1,6})([ \t]+.*)?$`)

	// divFence matches a Pandoc fenced-div line: up to three leading spaces
	// followed by at least three colons, then an optional attribute string.
	divFence = regexp.MustCompile(`^ {0,3}(:{3,})[ \t]*(.*)$`)

	// codeFence matches a fenced code block delimiter (backticks or tildes).
	codeFence = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})(.*)$")

	// headingAttrs matches a trailing Pandoc attribute block on a heading,
	// e.g. `## Title {#sec-foo .unnumbered}`.
	//
	// The real discriminator against a bracketed span at the end of the
	// heading text (`## [Wartenwand]{.fw}[Unit-Matrix]{.pol}`) is not
	// whitespace: Pandoc always resolves a `{...}` immediately preceded by
	// `]` as that span's attributes, whitespace or not, and a `{...}` not
	// preceded by `]` as the heading's own attribute block, whitespace or
	// not (`## Title{#id}` is a valid attribute block with no space at
	// all). So the block is matched only when it is not preceded by `]`.
	headingAttrs = regexp.MustCompile(`(^|[^\]])(\{[^{}]*\})[ \t]*$`)

	// attrHasID reports whether an attribute block already declares an
	// identifier.
	attrHasID = regexp.MustCompile(`(?:^|[ \t])#[^ \t{}]+`)
)

// opensDiv reports whether the text following the colons of a div fence
// opens a div. Pandoc reads any attribute text -- the shorthand `::: slide`
// as well as the explicit `::: {.slide #id}` -- as an opening fence, and a
// bare fence as the close of the innermost open div.
func opensDiv(attrs string) bool {
	return strings.TrimSpace(attrs) != ""
}

// splitAttrTokens splits an attribute string on whitespace while keeping
// quoted values (e.g. `when-profile="dispatcher-fw"`) intact.
func splitAttrTokens(s string) []string {
	var (
		tokens []string
		cur    strings.Builder
		quote  rune
	)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote = r
			cur.WriteRune(r)
		case r == ' ' || r == '\t':
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// codeTracker follows fenced-code-block state across a sequence of lines so
// that callers can ignore Markdown constructs that appear inside code.
type codeTracker struct {
	open   bool
	marker byte
	length int
}

// step advances the tracker by one line and reports whether that line is
// part of a fenced code block (including its delimiters).
func (t *codeTracker) step(line string) bool {
	m := codeFence.FindStringSubmatch(line)
	if m == nil {
		return t.open
	}
	fence := m[2]
	marker, length := fence[0], len(fence)

	if !t.open {
		// An opening fence may carry an info string; a tilde fence may
		// contain backticks in it, a backtick fence may not.
		if marker == '`' && strings.Contains(m[3], "`") {
			return false
		}
		t.open, t.marker, t.length = true, marker, length
		return true
	}
	// A closing fence must use the same marker, be at least as long, and
	// carry no info string.
	if marker == t.marker && length >= t.length && strings.TrimSpace(m[3]) == "" {
		t.open = false
	}
	return true
}

// fenceRepair records the corrections balanceFences had to make.
type fenceRepair struct {
	// Code is set when the page ended inside a fenced code block.
	Code bool
	// Divs is the number of fenced divs left open by the page.
	Divs int
	// Stray is the number of closing div fences that had nothing to close.
	Stray int
	// Glued is the number of opening div fences that had to be separated
	// from the paragraph or list item directly above them.
	Glued int
}

func (r fenceRepair) clean() bool {
	return !r.Code && r.Divs == 0 && r.Stray == 0 && r.Glued == 0
}

// balanceFences makes a page's fenced constructs self-contained.
//
// Source pages in the wild are not always balanced, and Pandoc tolerates it
// because it closes anything still open at the end of a file. Concatenating
// such a page would instead swallow everything that follows it: an unclosed
// code fence turns the rest of the book into a code listing, and an unclosed
// div wraps it in that div. Both are therefore closed explicitly here, and a
// stray closing div fence that would escape the page is dropped.
//
// It also gives an opening div fence the blank line Pandoc needs to see it
// as a fence at all. Written straight underneath a paragraph or a list item
// the line is lazy continuation and stays text -- but the fence meant to
// close it is still read as a fence, so it closes an enclosing div instead
// and every division after it is off by one. That is invisible while a page
// is rendered on its own and catastrophic once pages are concatenated.
func balanceFences(lines []string) ([]string, fenceRepair) {
	out := make([]string, 0, len(lines))
	var (
		code   codeTracker
		repair fenceRepair
		depth  int
		width  = 3
	)

	for _, line := range lines {
		if code.step(line) {
			out = append(out, line)
			continue
		}

		m := divFence.FindStringSubmatch(line)
		if m == nil {
			out = append(out, line)
			continue
		}

		if opensDiv(m[2]) {
			if len(out) > 0 && continuesParagraph(out[len(out)-1]) {
				repair.Glued++
				out = append(out, "")
			}
			depth++
			if len(m[1]) > width {
				width = len(m[1])
			}
			out = append(out, line)
			continue
		}

		if depth == 0 {
			repair.Stray++
			continue
		}
		depth--
		out = append(out, line)
	}

	if code.open {
		repair.Code = true
		out = append(out, strings.Repeat(string(code.marker), code.length))
	}
	for ; depth > 0; depth-- {
		repair.Divs++
		out = append(out, strings.Repeat(":", width))
	}
	return out, repair
}

// continuesParagraph reports whether a following line would be read as more
// of the same block rather than as the start of a new one. Pandoc ends a
// paragraph at a blank line, a heading or a div fence; everything else it
// keeps reading, which is what makes an opening div fence glued to the line
// above disappear into it.
func continuesParagraph(line string) bool {
	return strings.TrimSpace(line) != "" &&
		!divFence.MatchString(line) &&
		!atxHeading.MatchString(line)
}

// heading is a located ATX heading within a slice of lines.
type heading struct {
	Index  int // position in the line slice
	Level  int
	Indent string
	Rest   string // text after the hashes, including its leading whitespace
	// Block identifies the top-level fenced div the heading sits in.
	// Content outside any div is block 0. Sibling divs such as
	// `::: explanation` and `::: tutorial` normally repeat the same page
	// heading, and at most one of them survives the project's content
	// filter, so anchors are assigned per block rather than per page.
	Block int
}

// Text returns the heading's title with its trailing attribute block and
// surrounding whitespace removed.
func (h heading) Text() string {
	rest := headingAttrs.ReplaceAllString(h.Rest, "$1")
	return strings.TrimSpace(strings.TrimRight(rest, "# \t"))
}

// findHeadings returns every ATX heading outside fenced code blocks.
func findHeadings(lines []string) []heading {
	var (
		out   []heading
		code  codeTracker
		depth int
		block int
	)
	for i, line := range lines {
		if code.step(line) {
			continue
		}

		if m := divFence.FindStringSubmatch(line); m != nil {
			if opensDiv(m[2]) {
				if depth == 0 {
					block++
				}
				depth++
			} else if depth > 0 {
				depth--
			}
			continue
		}

		m := atxHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		blockOf := 0
		if depth > 0 {
			blockOf = block
		}
		out = append(out, heading{
			Index:  i,
			Level:  len(m[2]),
			Indent: m[1],
			Rest:   m[3],
			Block:  blockOf,
		})
	}
	return out
}

// minHeadingLevel returns the shallowest heading level in the slice, or 0
// when there are no headings.
func minHeadingLevel(hs []heading) int {
	min := 0
	for _, h := range hs {
		if min == 0 || h.Level < min {
			min = h.Level
		}
	}
	return min
}

// maxFenceWidth returns the longest run of colons used by a div fence in the
// given lines, ignoring fenced code blocks. It is used to pick a wrapper
// fence that reads unambiguously wider than anything it contains.
func maxFenceWidth(lines []string) int {
	var (
		code codeTracker
		max  int
	)
	for _, line := range lines {
		if code.step(line) {
			continue
		}
		if m := divFence.FindStringSubmatch(line); m != nil && len(m[1]) > max {
			max = len(m[1])
		}
	}
	return max
}

// titleEdit describes the changes made to a page's own title heading: the
// identifier in-book links point at, a replacement text for `--title`, and
// the classes the heading takes on. The zero value changes nothing.
type titleEdit struct {
	ID      string
	Text    string
	Classes []string
}

// empty reports whether the edit would leave a heading as it is.
func (e titleEdit) empty() bool {
	return e.ID == "" && e.Text == "" && len(e.Classes) == 0
}

// renderHeading rebuilds a heading line at the given level and applies edit
// to it. A heading that already declares an identifier keeps it; other
// attributes are preserved and the new ones are merged in.
func renderHeading(h heading, level int, edit titleEdit) string {
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}

	text, attrs, spaced := splitHeadingRest(h.Rest)
	if edit.Text != "" {
		// The replacement brings its own spacing.
		text, spaced = " "+edit.Text, false
	}
	attrs = mergeAttrs(attrs, edit.ID, edit.Classes)

	line := h.Indent + strings.Repeat("#", level) + text
	switch {
	case attrs == "":
		return line
	case spaced:
		// The heading already had an attribute block; keep the spacing
		// it chose, which `## Title{#id}` relies on.
		return line + attrs
	default:
		return strings.TrimRight(line, " \t") + " " + attrs
	}
}

// splitHeadingRest separates the part of a heading line following the hashes
// into its text and its trailing Pandoc attribute block. attrs is empty when
// the heading carries none; spaced reports whether text already ends where
// the original attribute block began, so that its spacing can be restored.
func splitHeadingRest(rest string) (text, attrs string, spaced bool) {
	m := headingAttrs.FindStringSubmatchIndex(rest)
	if m == nil {
		return rest, "", false
	}
	// m[2]:m[3] is the character preceding the brace (group 1, part of the
	// text); m[4]:m[5] is the attribute block itself (group 2).
	return rest[:m[3]], rest[m[4]:m[5]], true
}

// mergeAttrs folds an identifier and a set of classes into a heading's
// attribute block, leaving the attributes that are already there alone. A
// heading that declares its own identifier keeps it, because the source
// page may be linking to it.
func mergeAttrs(block, id string, classes []string) string {
	inner := ""
	if block != "" {
		inner = strings.TrimSpace(block[1 : len(block)-1])
	}

	var parts []string
	if id != "" && !attrHasID.MatchString(inner) {
		parts = append(parts, "#"+id)
	}
	if inner != "" {
		parts = append(parts, inner)
	}
	for _, class := range classes {
		if !hasAttrClass(inner, class) {
			parts = append(parts, "."+class)
		}
	}

	if len(parts) == 0 {
		return block
	}
	return "{" + strings.Join(parts, " ") + "}"
}

// hasAttrClass reports whether an attribute block's contents already carry
// the given class.
func hasAttrClass(inner, class string) bool {
	for _, tok := range splitAttrTokens(inner) {
		if strings.EqualFold(tok, "."+class) {
			return true
		}
	}
	return false
}
