package render

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/christophberger-ailab/qm/internal/build"
	"github.com/christophberger-ailab/qm/internal/config"
)

// fixture builds a small project with two topics. `chapter2` takes part in
// every format and audience; `extra` declares a narrower matrix.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.qmd":           "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"chapter2/index.qmd":  "---\ntitle: Chapter 2\norder: 2\n---\n# Two\n\n::: pol\nnur pol\n:::\n",
		"chapter2/second.qmd": "---\ntitle: Second\norder: 1\n---\n# Second\n",
		"extra/index.qmd":     "---\ntitle: Extra\norder: 3\n---\n# Extra\n",
		"qm.yml": `title: Test
formats:
  web: {type: html}
  handout: {type: [docx, pdf]}
  handbook: {type: docx}
audiences:
  std:
  pol:
topics:
  chapter2:
  extra: {formats: [handbook], audiences: [std]}
`,
	}
	for name, content := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func selections(opts build.Options) []string {
	out := make([]string, 0, len(opts.Selections))
	for _, s := range opts.Selections {
		out = append(out, s.String())
	}
	return out
}

func TestBuildOptionsExpandsTheDeclaredMatrix(t *testing.T) {
	root := fixture(t)
	opts, err := BuildOptions(root, config.Request{}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	got := selections(opts)
	want := []string{
		"web · std (site)", "web · pol (site)",
		"chapter2 · handout · std", "chapter2 · handout · pol",
		"chapter2 · handbook · std", "chapter2 · handbook · pol",
		"extra · handbook · std",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
	// The website covers both topics; extra takes no part in it.
	if n := len(opts.Selections[0].Topics); n != 1 {
		t.Errorf("site topics = %d, want 1 (extra declares handbook only)", n)
	}
}

func TestBuildOptionsNarrowsByFlag(t *testing.T) {
	root := fixture(t)
	opts, err := BuildOptions(root, config.Request{Formats: []string{"handbook"}, Audiences: []string{"std"}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"chapter2 · handbook · std", "extra · handbook · std"}
	if got := selections(opts); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildOptionsRejectsUnknownNames(t *testing.T) {
	root := fixture(t)
	for _, req := range []config.Request{
		{Topics: []string{"calltakr"}},
		{Formats: []string{"pptx"}},
		{Audiences: []string{"fw"}},
	} {
		if _, err := BuildOptions(root, req, false, false); err == nil {
			t.Errorf("%+v: accepted", req)
		}
	}
}

func TestBuildOptionsWithoutConfigUsesDefaults(t *testing.T) {
	root := fixture(t)
	os.Remove(filepath.Join(root, "qm.yml"))
	opts, err := BuildOptions(root, config.Request{}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"web · all (site)", "chapter2 · docx · all", "extra · docx · all"}
	if got := selections(opts); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunWritesDocuments(t *testing.T) {
	root := fixture(t)
	if err := Run(root, config.Request{Topics: []string{"chapter2"}, Formats: []string{"handout"}}, false, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"chapter2-std.docx", "chapter2-std.pdf", "chapter2-pol.docx", "chapter2-pol.pdf"} {
		if fi, err := os.Stat(filepath.Join(root, "_output", "handout", name)); err != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRunWritesTheSite(t *testing.T) {
	root := fixture(t)
	if err := Run(root, config.Request{Formats: []string{"web"}, Audiences: []string{"std"}}, true, false); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(root, "_output", "web-std", "chapter2", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), "nur pol") {
		t.Error("the std site shows pol content")
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	root := fixture(t)
	if err := Run(root, config.Request{}, false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "_output")); !os.IsNotExist(err) {
		t.Errorf("dry run wrote output: %v", err)
	}
}

func TestSplitList(t *testing.T) {
	for in, want := range map[string][]string{
		"":        nil,
		"a":       {"a"},
		"a, b,,c": {"a", "b", "c"},
		" , ":     nil,
	} {
		if got := splitList(in); !slices.Equal(got, want) {
			t.Errorf("splitList(%q) = %q, want %q", in, got, want)
		}
	}
}
