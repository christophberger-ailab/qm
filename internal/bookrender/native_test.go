package bookrender

import (
	"archive/zip"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christophberger-ailab/qm/internal/qmcore"
)

func TestNativeDryRunAndCoverValidation(t *testing.T) {
	root := project(t)
	format := filepath.Join(root, "_quarto-format-handout.yml")
	os.WriteFile(format, []byte("project:\n  output-dir: _output/handout\nbook:\n  output-file: book\nqm:\n  renderer: qm\nformat:\n  pdf: default\n  docx: default\n"), 0644)
	callLog := stubQuarto(t, "exit 99\n")
	if err := renderSelection(root, sel("book", "handout", "std"), true, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	if readLog(t, callLog) != "" {
		t.Fatal("dry run executed Quarto")
	}
	if _, err := os.Stat(BuildDirPath(root)); !os.IsNotExist(err) {
		t.Fatal("dry run created build directory")
	}
	os.WriteFile(filepath.Join(root, "_quarto-topic-book.yml"), []byte("qm:\n  cover: missing.png\n"), 0644)
	if err := renderSelection(root, sel("book", "handout", "std"), false, func(string, ...any) {}); err == nil {
		t.Fatal("missing cover accepted")
	}
	if readLog(t, callLog) != "" {
		t.Fatal("invalid cover reached Quarto")
	}
}

// Opt-in because Quarto is an external integration dependency.
func TestNativeQuartoIntegration(t *testing.T) {
	if os.Getenv("QM_TEST_QUARTO") == "" {
		t.Skip("set QM_TEST_QUARTO=1")
	}
	if _, err := exec.LookPath("quarto"); err != nil {
		t.Skip(err)
	}
	root := project(t)
	files := map[string]string{
		"_quarto-format-handout.yml": "project:\n  type: book\n  output-dir: _output/handout\nbook:\n  output-file: book\nqm:\n  renderer: qm\nformat:\n  pdf:\n    toc: true\n  docx:\n    toc: true\n",
		"_quarto-topic-book.yml":     "book:\n  title: Integration\nqm:\n  cover: cover.png\n",
		"_quarto.yml":                "project:\n  type: book\nbook:\n  chapters: [index.qmd]\nfilters: [audience.lua]\n",
		"audience.lua":               "function Div(el) if el.classes:includes('private') then return {} end end\n",
		"index.qmd":                  "# Überblick\n\nVisible ÄÖÜ **bold** and [link](https://example.org).\n\n::: private\nEXCLUDED-AUDIENCE-TEXT\n:::\n\n## Table\n\n| A | B |\n|---|---|\n| One | Two |\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	f, _ := os.Create(filepath.Join(root, "cover.png"))
	png.Encode(f, image.NewRGBA(image.Rect(0, 0, 100, 140)))
	f.Close()
	if err := Run(Options{Root: root, Selections: []qmcore.Selection{sel("book", "handout", "std")}}, t.Logf); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(filepath.Join(root, "_output/handout/book.docx"))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var body string
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			body = string(b)
		}
	}
	if strings.Contains(body, "EXCLUDED-AUDIENCE-TEXT") || !strings.Contains(body, "Visible") {
		t.Fatal("audience filter not preserved")
	}
	if !strings.Contains(body, `name="qm cover"`) {
		t.Fatal("DOCX cover missing")
	}
	if info, err := os.Stat(filepath.Join(root, "_output/handout/book.pdf")); err != nil || info.Size() < 1000 {
		t.Fatal("PDF missing or empty")
	}
}
