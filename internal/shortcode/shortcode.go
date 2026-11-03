// Package shortcode resolves the Quarto shortcodes qm's content uses, on
// the text, before the Markdown is parsed.
package shortcode

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/christophberger-ailab/qm/internal/qmcore"
)

// The shortcodes:
//
//	{{< include /path/file.md >}}   the file's text, in place (project-absolute)
//	{{< pagebreak >}}               a page break (see the page-break marker below)
//	{{< var name >}}                a profile variable or a `_variables.yml` value
//
// Includes and page breaks are whole-line shortcodes, which is how the
// content writes them; `var` may sit anywhere in a line. Text inside fenced
// code blocks is left alone. Any other shortcode is reported once and left
// in the text, where it shows up as literal braces.

// pagebreakDiv is the marker a page-break shortcode becomes: an empty div
// that every writer turns into its own page break.
const pagebreakDiv = "\n\n::: {.qm-pagebreak}\n:::\n\n"

// maxIncludeDepth stops an include that includes itself, however indirectly.
const maxIncludeDepth = 8

var (
	// shortcodeLine matches a line that holds one shortcode and nothing else.
	shortcodeLine = regexp.MustCompile(`^\s*\{\{<\s*([A-Za-z][\w-]*)\s*(.*?)\s*>\}\}\s*$`)
	// shortcodeAny finds shortcodes anywhere in a line, for the report.
	shortcodeAny = regexp.MustCompile(`\{\{<\s*([A-Za-z][\w-]*)`)
	// escapedShortcode is Quarto's way of writing a shortcode literally.
	escapedShortcode = regexp.MustCompile(`\{\{\{<(.*?)>\}\}\}`)
)

// Expander resolves shortcodes in text. It is configured once per render
// and collects what it found for the log.
type Expander struct {
	root string
	vars map[string]string
	warn func(format string, args ...any)

	warned   map[string]bool
	mermaid  int
	includes int
}

// New returns an Expander for the project at root. Includes are read
// relative to root; `var` resolves against vars; warn receives the
// problems that do not stop a render.
func New(root string, vars map[string]string, warn func(string, ...any)) *Expander {
	if warn == nil {
		warn = func(string, ...any) {}
	}
	return &Expander{root: root, vars: vars, warn: warn, warned: map[string]bool{}}
}

// Expand returns text with its shortcodes resolved.
func (s *Expander) Expand(text string) (string, error) {
	return s.expand(text, 0, nil)
}

func (s *Expander) expand(text string, depth int, stack []string) (string, error) {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	var fence codeFence
	for _, line := range lines {
		if fence.step(line) {
			if fence.opening && fence.mermaid {
				s.mermaid++
			}
			out = append(out, line)
			continue
		}
		if m := shortcodeLine.FindStringSubmatch(line); m != nil {
			replaced, handled, err := s.wholeLine(m[1], m[2], depth, stack)
			if err != nil {
				return "", err
			}
			if handled {
				out = append(out, replaced)
				continue
			}
		}
		out = append(out, s.inline(line))
	}
	return strings.Join(out, "\n"), nil
}

// wholeLine resolves a line that is one shortcode. handled is false for a
// shortcode it does not know; the caller then treats the line as text.
func (s *Expander) wholeLine(name, arg string, depth int, stack []string) (string, bool, error) {
	switch name {
	case "pagebreak":
		return pagebreakDiv, true, nil
	case "include":
		text, err := s.include(arg, depth, stack)
		return text, true, err
	}
	return "", false, nil
}

func (s *Expander) include(arg string, depth int, stack []string) (string, error) {
	path := strings.Trim(strings.TrimSpace(arg), `"'`)
	if path == "" {
		return "", fmt.Errorf("{{< include >}} names no file")
	}
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("{{< include %s >}}: the path must start with '/' (it is "+
			"relative to the project root; the flattened book has lost the page it was written in)", path)
	}
	abs, err := qmcore.ProjectPath(s.root, strings.TrimPrefix(path, "/"))
	if err != nil {
		return "", fmt.Errorf("{{< include %s >}}: %w", path, err)
	}
	for _, seen := range stack {
		if seen == abs {
			return "", fmt.Errorf("{{< include %s >}} includes itself", path)
		}
	}
	if depth >= maxIncludeDepth {
		return "", fmt.Errorf("{{< include %s >}}: includes are nested more than %d deep", path, maxIncludeDepth)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("{{< include %s >}}: %w", path, err)
	}
	s.includes++
	text, err := s.expand(strings.TrimRight(string(data), "\n"), depth+1, append(stack, abs))
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return text, nil
}

// inline resolves the shortcodes that may sit in the middle of a line.
func (s *Expander) inline(line string) string {
	if !strings.Contains(line, "{{") {
		return line
	}
	line = escapedShortcode.ReplaceAllString(line, "{{<$1>}}")
	line = varRef.ReplaceAllStringFunc(line, func(m string) string {
		name := varRef.FindStringSubmatch(m)[1]
		if v, ok := s.vars[name]; ok {
			return v
		}
		s.reportOnce("var "+name, "undefined variable %q in {{< var %s >}}; it is left as written", name, name)
		return m
	})
	for _, m := range shortcodeAny.FindAllStringSubmatch(line, -1) {
		if m[1] == "var" || m[1] == "include" || m[1] == "pagebreak" {
			continue
		}
		s.reportOnce(m[1], "the {{< %s >}} shortcode is not supported; it stays in the text as written", m[1])
	}
	return line
}

func (s *Expander) reportOnce(key, format string, args ...any) {
	if s.warned[key] {
		return
	}
	s.warned[key] = true
	s.warn(format, args...)
}

// varRef matches a `{{< var name >}}` shortcode.
var varRef = regexp.MustCompile(`\{\{<\s*var\s+([^\s>]+)\s*>\}\}`)

// codeFence tracks whether a line belongs to a fenced code block.
type codeFence struct {
	open    bool
	char    byte
	width   int
	mermaid bool
	// opening is true on the line that opens the current block.
	opening bool
}

// step feeds one line and reports whether it is part of a fenced block: its
// fences and everything between them.
func (f *codeFence) step(line string) bool {
	f.opening = false
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) >= 4 && !f.open {
		return false // an indented code block is not a fence
	}
	if !f.open {
		c := byte(0)
		if strings.HasPrefix(trimmed, "```") {
			c = '`'
		} else if strings.HasPrefix(trimmed, "~~~") {
			c = '~'
		}
		if c == 0 {
			return false
		}
		n := 0
		for n < len(trimmed) && trimmed[n] == c {
			n++
		}
		info := strings.TrimSpace(trimmed[n:])
		f.open, f.char, f.width, f.opening = true, c, n, true
		f.mermaid = strings.HasPrefix(info, "{mermaid}") || strings.HasPrefix(info, "{.mermaid}")
		return true
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == f.char {
		n++
	}
	if n >= f.width && strings.TrimSpace(trimmed[n:]) == "" {
		f.open = false
	}
	return true
}

// Mermaid is the number of Mermaid code blocks seen, which only Quarto
// could draw; they stay code.
func (s *Expander) Mermaid() int { return s.mermaid }

// Includes is the number of files included.
func (s *Expander) Includes() int { return s.includes }

// Variables is what `{{< var >}}` resolves against: the project's
// `_variables.yml`, whose nested keys are addressed with dots
// (`metadata.version`), overlaid with vars.
func Variables(root string, vars map[string]string) map[string]string {
	out := map[string]string{}
	for _, name := range []string{"_variables.yml", "_variables.yaml"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		var doc map[string]any
		if yaml.Unmarshal(data, &doc) == nil {
			flatten("", doc, out)
		}
		break
	}
	for k, v := range vars {
		out[k] = v
	}
	return out
}
func flatten(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			flatten(p, t[k], out)
		}
	case nil:
	default:
		if prefix != "" {
			out[prefix] = fmt.Sprint(t)
		}
	}
}
