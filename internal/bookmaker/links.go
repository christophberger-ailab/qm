package bookmaker

import (
	"path"
	"regexp"
	"strings"
)

// refDefinition matches a link reference definition: `[label]: /target`.
var refDefinition = regexp.MustCompile(`^ {0,3}\[([^\]]+)\]:[ \t]+(\S+)(.*)$`)

// linkTargets maps the website URL forms of every page in the book to the
// anchor that page's heading carries in the flattened document.
type linkTargets map[string]string

// buildLinkTargets registers every URL spelling under which a node of the
// tree can be referenced from another page.
//
// Quarto websites link to pages by their output URL, so the same page is
// reachable as `/a/b/`, `/a/b`, `/a/b/index.qmd` and `/a/b/index.html`.
func buildLinkTargets(root *Node) linkTargets {
	targets := linkTargets{}
	root.Walk(func(n *Node) {
		id := n.Anchor
		base := "/" + n.Rel
		if n.IsDir() {
			for _, form := range []string{base, base + "/", base + "/index.qmd", base + "/index.html", base + "/index.md"} {
				targets[form] = id
			}
			return
		}
		for _, form := range []string{base, base + ".qmd", base + ".html", base + ".md"} {
			targets[form] = id
		}
	})
	return targets
}

// lookup resolves a link destination to an anchor. baseDir is the
// project-relative directory of the page containing the link, which relative
// destinations are resolved against.
//
// It returns the replacement destination and whether the link points at a
// page inside the book.
func (t linkTargets) lookup(dest, baseDir string) (string, bool) {
	if dest == "" || strings.HasPrefix(dest, "#") {
		return "", false
	}
	// Absolute URLs, protocol-relative URLs and non-http schemes leave the
	// project entirely.
	if strings.Contains(dest, "://") || strings.HasPrefix(dest, "//") ||
		strings.HasPrefix(dest, "mailto:") || strings.HasPrefix(dest, "tel:") {
		return "", false
	}

	target, fragment, hasFragment := strings.Cut(dest, "#")
	if q := strings.IndexByte(target, '?'); q >= 0 {
		target = target[:q]
	}
	if target == "" {
		return "", false
	}

	if !strings.HasPrefix(target, "/") {
		trailingSlash := strings.HasSuffix(target, "/")
		target = "/" + path.Join(baseDir, target)
		if trailingSlash && !strings.HasSuffix(target, "/") {
			target += "/"
		}
	}

	id, ok := t[target]
	if !ok {
		id, ok = t[strings.TrimSuffix(target, "/")]
	}
	if !ok {
		return "", false
	}

	// A link that carries its own fragment addresses a heading inside the
	// target page. That heading keeps its own identifier, so once both
	// pages live in the same document the fragment alone is the answer.
	if hasFragment && fragment != "" {
		return "#" + fragment, true
	}
	return "#" + id, true
}

// rewriteLinks converts every in-book link in the given lines into an anchor
// reference. Image destinations and fenced code blocks are left untouched,
// and links leaving the book keep their original destination.
//
// It returns the rewritten lines and the number of destinations changed.
func rewriteLinks(lines []string, targets linkTargets, baseDir string) ([]string, int) {
	var (
		code    codeTracker
		changed int
	)
	out := make([]string, len(lines))

	for i, line := range lines {
		if code.step(line) {
			out[i] = line
			continue
		}

		if m := refDefinition.FindStringSubmatch(line); m != nil {
			if anchor, ok := targets.lookup(m[2], baseDir); ok {
				changed++
				out[i] = "[" + m[1] + "]: " + anchor + m[3]
				continue
			}
		}

		rewritten, n := rewriteInlineLinks(line, targets, baseDir)
		out[i], changed = rewritten, changed+n
	}
	return out, changed
}

// rewriteInlineLinks rewrites the destinations of inline links on a single
// line.
//
// Markdown inline links are not a regular language once nested brackets are
// involved -- `[![alt](/img.png)](/page/)` is common in this kind of
// project -- so the line is scanned rather than pattern-matched. For every
// `](` the matching opening bracket is located by walking backwards with a
// depth counter, which is what distinguishes an image from a link and an
// inner image from its enclosing link.
func rewriteInlineLinks(line string, targets linkTargets, baseDir string) (string, int) {
	var (
		b       strings.Builder
		changed int
		i       int
	)

	for i < len(line) {
		p := strings.Index(line[i:], "](")
		if p < 0 {
			break
		}
		p += i

		dest, rest, end, ok := parseLinkTail(line, p+1)
		if !ok {
			b.WriteString(line[i : p+2])
			i = p + 2
			continue
		}

		if isImageLink(line, p) {
			b.WriteString(line[i:end])
			i = end
			continue
		}

		anchor, resolved := targets.lookup(dest, baseDir)
		if !resolved {
			b.WriteString(line[i:end])
			i = end
			continue
		}

		b.WriteString(line[i : p+2])
		b.WriteString(anchor)
		b.WriteString(rest)
		b.WriteByte(')')
		changed++
		i = end
	}

	if changed == 0 {
		return line, 0
	}
	b.WriteString(line[i:])
	return b.String(), changed
}

// isImageLink reports whether the `](` at position p belongs to an image
// rather than a link, by locating the matching `[` and checking for a
// preceding `!`.
func isImageLink(line string, p int) bool {
	depth := 0
	for j := p - 1; j >= 0; j-- {
		if isEscaped(line, j) {
			continue
		}
		switch line[j] {
		case ']':
			depth++
		case '[':
			if depth == 0 {
				return j > 0 && line[j-1] == '!' && !isEscaped(line, j-1)
			}
			depth--
		}
	}
	return false
}

// isEscaped reports whether the byte at index i is preceded by an odd number
// of backslashes and therefore escaped.
func isEscaped(s string, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

// parseLinkTail reads the destination and optional title of an inline link
// whose `(` sits at index open. It returns the destination, the raw text
// between destination and closing paren, and the index just past the closing
// paren.
func parseLinkTail(line string, open int) (dest, rest string, end int, ok bool) {
	i := open + 1
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) {
		return "", "", 0, false
	}

	if line[i] == '<' {
		close := strings.IndexByte(line[i:], '>')
		if close < 0 {
			return "", "", 0, false
		}
		dest = line[i+1 : i+close]
		i += close + 1
	} else {
		start := i
		depth := 0
		for i < len(line) {
			c := line[i]
			if c == ' ' || c == '\t' {
				break
			}
			if c == '(' && !isEscaped(line, i) {
				depth++
			}
			if c == ')' && !isEscaped(line, i) {
				if depth == 0 {
					break
				}
				depth--
			}
			i++
		}
		dest = line[start:i]
	}

	start := i
	for i < len(line) && line[i] != ')' {
		i++
	}
	if i >= len(line) {
		return "", "", 0, false
	}
	return dest, line[start:i], i + 1, true
}
