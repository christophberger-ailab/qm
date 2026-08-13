package bookmaker

import (
	"fmt"
	"strings"
	"unicode"
)

// anchorPrefix namespaces every generated heading identifier. It deliberately
// avoids Quarto's reserved cross-reference prefixes (sec-, fig-, tbl-, ...)
// so that generated anchors are plain link targets and never turn a heading
// into a numbered cross-reference.
const anchorPrefix = "bm-"

// anchorID derives a heading identifier from a node's project-relative path.
// Using the path rather than the title keeps the mapping obvious when reading
// the generated file, but it is not injective -- `a/b-c` and `a-b/c` slugify
// alike -- so assignAnchors deduplicates the results.
func anchorID(rel string) string {
	var b strings.Builder
	b.WriteString(anchorPrefix)

	lastDash := true
	for _, r := range strings.ToLower(rel) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == 'ä', r == 'ö', r == 'ü', r == 'ß':
			b.WriteString(map[rune]string{'ä': "ae", 'ö': "oe", 'ü': "ue", 'ß': "ss"}[r])
			lastDash = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			// Any other non-ASCII letter or digit: keep it, Pandoc
			// identifiers are Unicode-aware.
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// assignAnchors gives every node in the tree a unique heading identifier.
// The tree is walked in render order, so the identifiers are stable for a
// given source tree.
func assignAnchors(root *Node) {
	seen := map[string]int{}
	root.Walk(func(n *Node) {
		id := anchorID(n.Rel)
		if count := seen[id]; count > 0 {
			seen[id] = count + 1
			id = fmt.Sprintf("%s-%d", id, count+1)
		}
		seen[id]++
		n.Anchor = id
	})
}
