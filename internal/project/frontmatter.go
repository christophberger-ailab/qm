// Package project loads a Quarto project, builds its page tree from the
// order frontmatter, and applies structural edits (reorder, reparent,
// create, delete) back to the files.
package project

import (
	"bytes"
	"fmt"
	"regexp"

	"go.yaml.in/yaml/v4"
)

// FrontMatter holds the frontmatter fields the sorter cares about.
// Order is nil when the file has no order field.
type FrontMatter struct {
	Title string `yaml:"title"`
	Order *int   `yaml:"order"`
}

var fmDelim = []byte("---\n")

// splitFrontmatter returns the frontmatter block (delimiters included)
// and the body. The block is empty if src has no frontmatter.
func splitFrontmatter(src []byte) (block, body []byte) {
	if !bytes.HasPrefix(src, fmDelim) {
		return nil, src
	}
	end := bytes.Index(src[len(fmDelim):], fmDelim)
	if end < 0 {
		return nil, src
	}
	n := len(fmDelim) + end + len(fmDelim)
	return src[:n], src[n:]
}

// ParseFrontmatter extracts title and order from a page's frontmatter.
func ParseFrontmatter(src []byte) FrontMatter {
	var fm FrontMatter
	block, _ := splitFrontmatter(src)
	if block == nil {
		return fm
	}
	yaml.Unmarshal(block[len(fmDelim):len(block)-len(fmDelim)], &fm)
	return fm
}

var orderLine = regexp.MustCompile(`(?m)^order\s*:.*$`)

// SetOrder returns src with the frontmatter order field set to order,
// leaving everything else byte-for-byte intact. A missing order field or
// frontmatter block is created. src is not modified.
func SetOrder(src []byte, order int) []byte {
	line := fmt.Sprintf("order: %d", order)
	block, body := splitFrontmatter(src)
	if block == nil {
		return append([]byte("---\n"+line+"\n---\n\n"), body...)
	}
	from, to := len(block)-len(fmDelim), len(block)-len(fmDelim) // insert before closing ---
	if loc := orderLine.FindIndex(block); loc != nil {
		from, to = loc[0], loc[1]+1 // replace the existing line
	}
	out := make([]byte, 0, len(src)+len(line)+1)
	out = append(out, src[:from]...)
	out = append(out, line...)
	out = append(out, '\n')
	return append(out, src[to:]...)
}

var atxHeading = regexp.MustCompile(`^(#{1,6}) `)

// ShiftHeadings shifts all ATX headings in the Markdown body by delta
// levels, clamped to the range 1..6. Frontmatter and fenced code blocks
// are left untouched.
func ShiftHeadings(src []byte, delta int) []byte {
	if delta == 0 {
		return src
	}
	block, body := splitFrontmatter(src)
	lines := bytes.Split(body, []byte("\n"))
	inFence := false
	for i, ln := range lines {
		trimmed := bytes.TrimLeft(ln, " ")
		if bytes.HasPrefix(trimmed, []byte("```")) || bytes.HasPrefix(trimmed, []byte("~~~")) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := atxHeading.FindSubmatch(ln); m != nil {
			level := min(6, max(1, len(m[1])+delta))
			lines[i] = append(bytes.Repeat([]byte("#"), level), ln[len(m[1]):]...)
		}
	}
	out := make([]byte, len(block), len(src))
	copy(out, block)
	return append(out, bytes.Join(lines, []byte("\n"))...)
}
