package shortcode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortcodes(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("assets/colophon.md", "Colophon with {{< var who >}}.\n\n{{< include /assets/nested.md >}}\n")
	write("assets/nested.md", "Nested text.\n")
	write("assets/loop.md", "{{< include /assets/loop.md >}}\n")

	var warnings []string
	sc := New(root, map[string]string{"who": "Octave", "metadata.version": "1.2"}, func(f string, a ...any) {
		warnings = append(warnings, strings.TrimSpace(f))
	})
	in := strings.Join([]string{
		"Intro {{< var who >}} v{{< var metadata.version >}}.",
		"{{< include /assets/colophon.md >}}",
		"{{< pagebreak >}}",
		"```",
		"{{< include /assets/colophon.md >}}",
		"```",
		"```{mermaid}",
		"graph TD",
		"```",
		"Unknown {{< kbd Ctrl >}} twice {{< kbd Alt >}}, and {{< var missing >}}.",
		"Literal {{{< include x >}}}.",
	}, "\n")
	out, err := sc.Expand(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Intro Octave v1.2.",
		"Colophon with Octave.",
		"Nested text.",
		"::: {.qm-pagebreak}",
		"Literal {{< include x >}}.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// Inside a fence, text stays as written.
	if !strings.Contains(out, "```\n{{< include /assets/colophon.md >}}\n```") {
		t.Errorf("an include inside a code fence was expanded:\n%s", out)
	}
	if sc.Mermaid() != 1 {
		t.Errorf("mermaid blocks: got %d, want 1", sc.Mermaid())
	}
	// Each unsupported shortcode is reported once; so is an undefined variable.
	if len(warnings) != 2 {
		t.Errorf("warnings = %q; want one for the kbd shortcode and one for the variable", warnings)
	}

	for name, in := range map[string]string{
		"outside":     "{{< include /../secret.md >}}",
		"outside rel": "{{< include ../secret.md >}}",
		"missing":     "{{< include /assets/none.md >}}",
		"recursive":   "{{< include /assets/loop.md >}}",
		"no file":     "{{< include >}}",
	} {
		if _, err := New(root, nil, nil).Expand(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRelativeIncludes(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("assets/outer.md", "Outer.\n\n{{< include parts/inner.md >}}\n")
	write("assets/parts/inner.md", "Inner.\n\n{{< include '../../top.md' >}}\n")
	write("top.md", "Top.\n")

	for _, in := range []string{"{{< include assets/outer.md >}}", `{{< include "/assets/outer.md" >}}`} {
		out, err := New(root, nil, nil).Expand(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if want := "Outer.\n\nInner.\n\nTop."; out != want {
			t.Errorf("%s: got %q, want %q", in, out, want)
		}
	}
	for _, tc := range [][3]string{
		{".", "a.md", "a.md"},
		{"book/ch", "../x.md", "book/x.md"},
		{"book", "/assets/x.md", "assets/x.md"},
		{".", "../x.md", "../x.md"},
	} {
		if got := ResolveInclude(tc[0], tc[1]); got != tc[2] {
			t.Errorf("ResolveInclude(%q, %q) = %q, want %q", tc[0], tc[1], got, tc[2])
		}
	}
}
