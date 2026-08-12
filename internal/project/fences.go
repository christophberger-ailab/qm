package project

import "bytes"

// BalancedFences reports whether all Quarto block fences in src are
// paired. An opening fence is three or more colons followed by text, a
// closing fence is three or more colons alone on a line. Frontmatter
// and fenced code blocks are left out of the check.
func BalancedFences(src []byte) bool {
	_, body := splitFrontmatter(src)
	depth, inCode := 0, false
	for _, ln := range bytes.Split(body, []byte("\n")) {
		ln = bytes.TrimSpace(ln)
		if bytes.HasPrefix(ln, []byte("```")) || bytes.HasPrefix(ln, []byte("~~~")) {
			inCode = !inCode
			continue
		}
		if inCode || !bytes.HasPrefix(ln, []byte(":::")) {
			continue
		}
		if len(bytes.TrimLeft(ln, ":")) == 0 {
			if depth--; depth < 0 {
				return false
			}
		} else {
			depth++
		}
	}
	return depth == 0
}
