package bookmaker

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// writeFile creates a file and any missing parent directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// containsHeading reports whether the document has a heading of the given
// level whose text starts with want.
func containsHeading(doc, prefix, want string) bool {
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, prefix) && strings.HasPrefix(strings.TrimPrefix(line, prefix), want) {
			return true
		}
	}
	return false
}

// hasAnchor reports whether the document attaches the given identifier to a
// heading, whether or not the heading carries further attributes.
func hasAnchor(doc, id string) bool {
	return strings.Contains(doc, "{#"+id+"}") || strings.Contains(doc, "{#"+id+" ")
}

// opensDivOfClass reports whether the document has a fenced div opening the
// given class, whatever width of fence it took to nest it.
func opensDivOfClass(doc, class string) bool {
	for _, line := range strings.Split(doc, "\n") {
		m := testDivFence.FindStringSubmatch(line)
		if m != nil && strings.TrimSpace(m[2]) == class {
			return true
		}
	}
	return false
}

// firstLines returns the first n lines of s.
func firstLines(s string, n int) string {
	parts := strings.SplitN(s, "\n", n+1)
	if len(parts) > n {
		parts = parts[:n]
	}
	return strings.Join(parts, "\n")
}

var (
	testCodeFence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	testDivFence  = regexp.MustCompile(`^ {0,3}(:{3,})[ \t]*(.*)$`)
)

// balanced reports whether a document's code fences and fenced divs all
// close. It re-implements the check independently of the production scanner
// so that a bug in one is not hidden by the same bug in the other.
func balanced(doc string) bool {
	var inCode bool
	depth := 0
	for _, line := range strings.Split(doc, "\n") {
		if testCodeFence.MatchString(line) {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		m := testDivFence.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if strings.TrimSpace(m[2]) == "" {
			depth--
			if depth < 0 {
				return false
			}
		} else {
			depth++
		}
	}
	return depth == 0 && !inCode
}
