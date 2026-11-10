// Package markdown parses qm's Markdown — CommonMark with the Pandoc
// extensions the content uses — into a goldmark syntax tree that every
// writer (website, DOCX, PDF) walks.
//
// On top of CommonMark and GFM tables, strikethrough, footnotes and
// definition lists, it reads:
//
//	::: {.class #id key=value}   fenced divs (Div), nested by fence order
//	[text]{.class}               bracketed spans (Span)
//	# Heading {#id .class}       heading attributes; {-} is .unnumbered
//	![alt](src){width=50%}       link and image attributes
//
// Every heading gets an identifier, Pandoc-style, unless it declares one.
// Filter removes the conditional content a selection does not show.
package markdown

import (
	"strings"
	"unicode"
)

// Attrs are Pandoc attributes: an identifier, classes, and key/value pairs
// in source order.
type Attrs struct {
	ID      string
	Classes []string
	KV      [][2]string
}

// Has reports whether the attributes carry class c.
func (a Attrs) Has(c string) bool {
	for _, x := range a.Classes {
		if x == c {
			return true
		}
	}
	return false
}

// Get returns the value of key k.
func (a Attrs) Get(k string) (string, bool) {
	for _, kv := range a.KV {
		if kv[0] == k {
			return kv[1], true
		}
	}
	return "", false
}

// Empty reports whether there are no attributes at all.
func (a Attrs) Empty() bool { return a.ID == "" && len(a.Classes) == 0 && len(a.KV) == 0 }

// ParseAttrs reads an attribute block, braces included: `{#id .a .b k=v
// k2="quoted value"}`. It reports false for anything that is not one, so
// text like `{not attributes}` is left alone. `{-}` is `.unnumbered`, and
// `{=format}` is a raw attribute, returned as the class "=format".
func ParseAttrs(s string) (Attrs, bool) {
	var a Attrs
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return a, false
	}
	body := strings.TrimSpace(s[1 : len(s)-1])
	if body == "" {
		return a, false
	}
	if strings.HasPrefix(body, "=") && !strings.ContainsAny(body, " \t") {
		a.Classes = []string{body}
		return a, true
	}
	i := 0
	for i < len(body) {
		for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n') {
			i++
		}
		if i >= len(body) {
			break
		}
		switch c := body[i]; {
		case c == '#':
			j := i + 1
			for j < len(body) && isAttrNameByte(body[j]) {
				j++
			}
			if j == i+1 {
				return Attrs{}, false
			}
			a.ID = body[i+1 : j]
			i = j
		case c == '.':
			j := i + 1
			for j < len(body) && isAttrNameByte(body[j]) {
				j++
			}
			if j == i+1 {
				return Attrs{}, false
			}
			a.Classes = append(a.Classes, body[i+1:j])
			i = j
		case c == '-' && (i+1 == len(body) || body[i+1] == ' '):
			a.Classes = append(a.Classes, "unnumbered")
			i++
		default:
			j := i
			for j < len(body) && isAttrNameByte(body[j]) {
				j++
			}
			if j == i || j >= len(body) || body[j] != '=' {
				return Attrs{}, false
			}
			key := body[i:j]
			j++
			var val string
			if j < len(body) && (body[j] == '"' || body[j] == '\'') {
				q := body[j]
				k := j + 1
				var b strings.Builder
				for k < len(body) && body[k] != q {
					if body[k] == '\\' && k+1 < len(body) {
						k++
					}
					b.WriteByte(body[k])
					k++
				}
				if k >= len(body) {
					return Attrs{}, false
				}
				val = b.String()
				j = k + 1
			} else {
				k := j
				for k < len(body) && body[k] != ' ' && body[k] != '\t' {
					k++
				}
				val = body[j:k]
				j = k
			}
			a.KV = append(a.KV, [2]string{key, val})
			i = j
		}
	}
	return a, !a.Empty()
}

func isAttrNameByte(b byte) bool {
	return b == '-' || b == '_' || b == ':' ||
		b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

// trailingAttrs splits `text {attrs}` into the text and its attributes.
func trailingAttrs(s string) (string, Attrs, bool) {
	t := strings.TrimRight(s, " \t")
	if !strings.HasSuffix(t, "}") {
		return s, Attrs{}, false
	}
	open := strings.LastIndexByte(t, '{')
	if open < 0 {
		return s, Attrs{}, false
	}
	a, ok := ParseAttrs(t[open:])
	if !ok {
		return s, Attrs{}, false
	}
	return strings.TrimRight(t[:open], " \t"), a, true
}

// leadingAttrs splits `{attrs}rest` into the attributes and the rest.
func leadingAttrs(s string) (Attrs, string, bool) {
	if !strings.HasPrefix(s, "{") {
		return Attrs{}, s, false
	}
	depth, quote := 0, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				a, ok := ParseAttrs(s[:i+1])
				if !ok {
					return Attrs{}, s, false
				}
				return a, s[i+1:], true
			}
		}
	}
	return Attrs{}, s, false
}

// Slug makes a Pandoc-style identifier from heading text: lower case,
// everything but letters, digits, `_`, `-` and `.` removed, spaces turned
// into hyphens, and everything before the first letter dropped.
func Slug(text string) string {
	var b strings.Builder
	started := false
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r):
			started = true
			b.WriteRune(r)
		case !started:
			continue
		case unicode.IsDigit(r) || r == '_' || r == '-' || r == '.':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('-')
		}
	}
	s := b.String()
	if s == "" {
		return "section"
	}
	return s
}
