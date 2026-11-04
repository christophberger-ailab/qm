package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func project(t *testing.T, yml string, folders ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range folders {
		os.MkdirAll(filepath.Join(root, f), 0o755)
		os.WriteFile(filepath.Join(root, f, "index.qmd"), []byte("# x\n"), 0o644)
	}
	if yml != "" {
		os.WriteFile(filepath.Join(root, FileName), []byte(yml), 0o644)
	}
	return root
}

func TestDefaults(t *testing.T) {
	root := project(t, "", "b", "a", "_hidden", "assets")
	os.Remove(filepath.Join(root, "assets", "index.qmd"))
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Topics.Names(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("topics %q", got)
	}
	if got := c.Formats.Names(); !slices.Equal(got, []string{"web"}) {
		t.Errorf("formats %q", got)
	}
	if c.Output != "_output" || len(c.Audiences) != 1 {
		t.Errorf("output %q audiences %v", c.Output, c.Audiences.Names())
	}
}

func TestLoadKeepsOrderAndRejectsTypos(t *testing.T) {
	root := project(t, `
formats:
  zeta: {type: docx}
  alpha: {type: [docx, pdf], toc: true}
audiences: [std, pol]
topics:
  t1: {folder: one}
`, "one")
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Formats.Names(); !slices.Equal(got, []string{"zeta", "alpha"}) {
		t.Errorf("order lost: %q", got)
	}
	if f, _ := c.Formats.Get("alpha"); !f.Has(TypePDF) || !f.TOC {
		t.Errorf("alpha %+v", f)
	}
	for name, yml := range map[string]string{
		"unknown key":      "formatz: {}\n",
		"unknown type":     "formats: {x: {type: pptx}}\n",
		"html mixed":       "formats: {x: {type: [html, docx]}}\n",
		"unknown audience": "topics: {one: {audiences: [fw]}}\n",
		"duplicate":        "topics:\n  one:\n  one:\n",
	} {
		os.WriteFile(filepath.Join(root, FileName), []byte(yml), 0o644)
		if _, err := Load(root); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestKeep(t *testing.T) {
	root := project(t, `
formats:
  handout: {type: docx, show: [explanation]}
  slides:  {type: docx, show: [slide-only]}
audiences:
  std:
  pol: {show: [behoerde]}
  fw:  {show: [behoerde]}
`, "a")
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	sels, err := c.Expand(Request{Formats: []string{"handout"}, Audiences: []string{"pol"}})
	if err != nil {
		t.Fatal(err)
	}
	s := sels[0]
	for classes, want := range map[string]bool{
		"":                     true,
		"unrelated":            true,
		"explanation":          true,
		"slide-only":           false,
		"pol":                  true,
		"fw":                   false,
		"behoerde":             true,
		"explanation pol":      true,
		"explanation fw":       false,
		"slide-only pol":       false,
		"fw pol":               true,
		"explanation tutorial": true,
	} {
		if got := s.Keep(strings.Fields(classes)); got != want {
			t.Errorf("Keep(%q) = %v, want %v", classes, got, want)
		}
	}
}

func TestPaths(t *testing.T) {
	root := project(t, `
vars: {org: LS}
formats:
  handout: {type: docx, file: "{org}_{topic}{suffix}", template: "tpl/{audience}.docx"}
  web: {type: html}
audiences:
  std: {vars: {suffix: ""}}
  pol: {vars: {suffix: _POL}}
topics:
  calltaker: {cover: "covers/{topic}{suffix}.png", folder: a}
`, "a")
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	sels, err := c.Expand(Request{Audiences: []string{"pol"}})
	if err != nil {
		t.Fatal(err)
	}
	var doc, web Selection
	for _, s := range sels {
		if s.Format.IsWeb() {
			web = s
		} else {
			doc = s
		}
	}
	stem, _ := doc.FileStem()
	cover, _ := doc.Cover()
	tpl, _ := doc.Template()
	dir, _ := doc.OutputDir()
	wdir, _ := web.OutputDir()
	if stem != "LS_calltaker_POL" ||
		cover != filepath.Join(root, "covers", "calltaker_POL.png") ||
		tpl != filepath.Join(root, "tpl", "pol.docx") ||
		dir != filepath.Join(root, "_output", "handout") ||
		wdir != filepath.Join(root, "_output", "web-pol") {
		t.Errorf("stem %q cover %q tpl %q dir %q web %q", stem, cover, tpl, dir, wdir)
	}
	if _, err := Interpolate("{nope}", nil); err == nil {
		t.Error("undefined placeholder accepted")
	}
}

// A project still configured for Quarto must not be rendered on the
// defaults: that would silently produce something else.
func TestOldQuartoConfigWithoutQmYmlIsAnError(t *testing.T) {
	root := project(t, "", "a")
	os.WriteFile(filepath.Join(root, "_quarto-topic-a.yml"), []byte("{}\n"), 0o644)
	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "qm migrate") || !strings.Contains(err.Error(), "_quarto-topic-a.yml") {
		t.Fatalf("err = %v", err)
	}
	// Once qm.yml exists, the old files no longer matter.
	os.WriteFile(filepath.Join(root, FileName), []byte("title: x\n"), 0o644)
	if _, err := Load(root); err != nil {
		t.Fatal(err)
	}
}
