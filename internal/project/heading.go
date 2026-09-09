package project

import (
	"bytes"
	"regexp"
)

var atxHeadingText = regexp.MustCompile(`^#{1,6}[ \t]+(.*)$`)

// headingAttrs matches a trailing Pandoc attribute block on a heading,
// e.g. `# Title {.unnumbered .unlisted}`.
//
// The discriminator against a bracketed span at the end of the heading
// text (`# [Spickzettel]{.pol}`) is not whitespace: Pandoc always reads a
// `{...}` immediately preceded by `]` as that span's attributes, and a
// `{...}` not preceded by `]` as the heading's own attribute block, with
// or without a space before it (`# Title{#id}` is a valid attribute block).
// So the block is matched only when it is not preceded by `]`.
var headingAttrs = regexp.MustCompile(`(^|[^\]])\{[^{}]*\}[ \t]*$`)

// FirstHeading returns the text of the first ATX Markdown heading in src's
// body, ignoring the frontmatter and any fenced code blocks. It returns ""
// when the body has no heading. The optional trailing "#" closing sequence
// of an ATX heading is stripped, as is the heading's own Pandoc attribute
// block: the title of `# Schulungen {.unnumbered .unlisted}` is
// "Schulungen", because the attributes tell Quarto how to render the
// heading and are not part of what it says.
func FirstHeading(src []byte) string {
	_, body := splitFrontmatter(src)
	inFence := false
	for _, ln := range bytes.Split(body, []byte("\n")) {
		trimmed := bytes.TrimLeft(ln, " ")
		if bytes.HasPrefix(trimmed, []byte("```")) || bytes.HasPrefix(trimmed, []byte("~~~")) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		m := atxHeadingText.FindSubmatch(trimmed)
		if m == nil {
			continue
		}
		if text := trimHeadingAttrs(trimClosingHashes(bytes.TrimSpace(m[1]))); len(text) > 0 {
			return string(text)
		}
	}
	return ""
}

// trimHeadingAttrs removes the heading's own trailing attribute block, if
// it has one.
func trimHeadingAttrs(text []byte) []byte {
	return bytes.TrimSpace(headingAttrs.ReplaceAll(text, []byte("$1")))
}

// trimClosingHashes removes an optional ATX closing sequence: a run of '#'
// at the end of the heading text that is either the whole text or preceded
// by a space.
func trimClosingHashes(text []byte) []byte {
	end := len(text)
	i := end
	for i > 0 && text[i-1] == '#' {
		i--
	}
	if i < end && (i == 0 || text[i-1] == ' ') {
		text = bytes.TrimRight(text[:i], " ")
	}
	return text
}
