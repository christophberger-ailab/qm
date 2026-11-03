package migrate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/christophberger-ailab/qm/internal/config"
)

func write(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func oldProject(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "_quarto.yml", `project:
  type: website
website:
  title: Handbuch
lang: de
filters:
  - filters/make.lua
`)
	write(t, root, "_quarto-vars.yml", "")
	write(t, root, "_quarto-topic-all.yml", "qm:\n  folder: ignored\n")
	write(t, root, "_quarto-topic-calltaker.yml", `book:
  title: Calltaker
qm:
  cover: assets/covers/calltaker{{< var audience >}}.png
  formats: [web, handout, slides]
  audiences: [std, pol]
_quarto-vars:
  topic: calltaker
`)
	write(t, root, "_quarto-topic-sysadmin.yml", `book:
  title: "{{< var topic >}} Admin"
qm:
  folder: systemadministration
  audiences: [std]
`)
	write(t, root, "_quarto-format-web.yml", `project:
  output-dir: _output/site
format:
  html: {}
qm:
  output-dir-suffix: "{{< var audience >}}"
  topics: [all]
`)
	write(t, root, "_quarto-format-handout.yml", `project:
  output-dir: _output/handout
book:
  output-file: "{{< var topic >}}{{< var audience >}}"
format:
  docx:
    toc: true
    reference-doc: _build/template.docx
qm:
  renderer: qm
  copy:
    _build/template.docx: "assets/templates/Template{{< var audience >}}.docx"
make:
  explanation: true
  tutorial: false
`)
	write(t, root, "_quarto-format-slides.yml", `project:
  output-dir: _output/slides
format:
  pptx: {}
`)
	write(t, root, "_quarto-audience-std.yml", "_quarto-vars:\n  audience: \"\"\n")
	write(t, root, "_quarto-audience-pol.yml", "make:\n  pol: true\n_quarto-vars:\n  audience: \"-pol\"\n")
	return root
}

func TestMigrate(t *testing.T) {
	root := oldProject(t)
	res, err := Run(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Written {
		t.Fatal("not written")
	}
	y := res.YAML
	t.Log("\n" + y)
	for _, want := range []string{
		"title: Handbuch", "lang: de", "audience -> audience_suffix", "filters/make.lua",
		"type: [docx, pdf]", "{topic}{audience_suffix}", "dir: site{audience_suffix}",
		"template: assets/templates/Template{audience_suffix}.docx",
		"cover: assets/covers/calltaker{audience_suffix}.png",
		"folder: systemadministration", "show: [explanation]",
	} {
		if !strings.Contains(y, want) {
			t.Errorf("YAML lacks %q", want)
		}
	}
	body := y[strings.Index(y, "\ntitle:"):]
	for _, bad := range []string{"\noutput:", "slides", "tutorial", "ignored", "all:"} {
		if strings.Contains(body, bad) {
			t.Errorf("YAML body should not contain %q", bad)
		}
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "slide decks are not supported yet") {
		t.Errorf("no pptx warning: %v", res.Warnings)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "sysadmin: title") {
		t.Errorf("no title warning: %v", res.Warnings)
	}

	c, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if c.Output != "_output" || c.Title != "Handbuch" || c.Lang != "de" {
		t.Errorf("top level: %+v", c)
	}
	if got := c.Formats.Names(); !slices.Equal(got, []string{"handout", "web"}) {
		t.Fatalf("formats %v", got)
	}
	h, _ := c.Formats.Get("handout")
	if !slices.Equal(h.Type, config.StringList{"docx", "pdf"}) || !h.TOC || !slices.Equal(h.Show, []string{"explanation"}) ||
		h.File != "{topic}{audience_suffix}" || h.Template != "assets/templates/Template{audience_suffix}.docx" {
		t.Errorf("handout: %+v", h)
	}
	w, _ := c.Formats.Get("web")
	if !w.IsWeb() || w.Dir != "site{audience_suffix}" {
		t.Errorf("web: %+v", w)
	}
	if got := c.Audiences.Names(); !slices.Equal(got, []string{"pol", "std"}) {
		t.Fatalf("audiences %v", got)
	}
	pol, _ := c.Audiences.Get("pol")
	if !slices.Equal(pol.Show, nil) || pol.Vars["audience_suffix"] != "-pol" {
		t.Errorf("pol: %+v", pol)
	}
	std, _ := c.Audiences.Get("std")
	if v, ok := std.Vars["audience_suffix"]; !ok || v != "" {
		t.Errorf("std: %+v", std)
	}
	if got := c.Topics.Names(); !slices.Equal(got, []string{"calltaker", "sysadmin"}) {
		t.Fatalf("topics %v", got)
	}
	ct, _ := c.Topics.Get("calltaker")
	if ct.Title != "Calltaker" || ct.Cover != "assets/covers/calltaker{audience_suffix}.png" ||
		!slices.Equal(ct.Formats, []string{"web", "handout"}) || !slices.Equal(ct.Audiences, []string{"std", "pol"}) {
		t.Errorf("calltaker: %+v", ct)
	}
	if len(ct.Vars) != 0 {
		t.Errorf("topic var kept: %v", ct.Vars)
	}
	sa, _ := c.Topics.Get("sysadmin")
	if sa.Folder != "systemadministration" || sa.Title != "" || !slices.Equal(sa.Audiences, []string{"std"}) {
		t.Errorf("sysadmin: %+v", sa)
	}
}

func TestMigrateExistingAndDryRun(t *testing.T) {
	root := oldProject(t)
	write(t, root, "qm.yml", "title: old\n")
	if _, err := Run(root, Options{}); err == nil {
		t.Fatal("overwrote qm.yml without --force")
	}
	res, err := Run(root, Options{DryRun: true})
	if err != nil || res.Written || res.YAML == "" {
		t.Fatalf("dry run: %v %+v", err, res)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "qm.yml")); string(b) != "title: old\n" {
		t.Error("dry run changed qm.yml")
	}
	if _, err := Run(root, Options{Force: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "qm.yml")); strings.Contains(string(b), "title: old") {
		t.Error("force did not overwrite")
	}
}
