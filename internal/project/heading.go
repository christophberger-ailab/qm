package project

import (
	"bytes"
	"regexp"
)

var atxHeadingText = regexp.MustCompile(`^#{1,6}[ \t]+(.*)$`)

// FirstHeading returns the text of the first ATX Markdown heading in src's
// body, ignoring the frontmatter and any fenced code blocks. It returns ""
// when the body has no heading. The optional trailing "#" closing sequence
// of an ATX heading is stripped.
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
		if text := trimClosingHashes(bytes.TrimSpace(m[1])); len(text) > 0 {
			return string(text)
		}
	}
	return ""
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
