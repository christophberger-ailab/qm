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

const nativeFormat = "project:\n  type: book\n  output-dir: _output/handout\n" +
	"book:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n" +
	"qm:\n  renderer: qm\n  pdf:\n    toc: true\n" +
	"filters:\n  - path: _build/qm-capture.lua\n    at: post-finalize\n" +
	"format:\n  docx:\n    toc: true\n"

func writePNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 100, 140))); err != nil {
		t.Fatal(err)
	}
}

func loadNative(t *testing.T, root string) (*Native, error) {
	t.Helper()
	ps, err := qmcore.LoadSelection(root, sel("book", "handout", "std"))
	if err != nil {
		t.Fatal(err)
	}
	return LoadNative(root, ps)
}

func TestLoadNative(t *testing.T) {
	root := project(t)
	format := filepath.Join(root, "_quarto-format-handout.yml")
	topic := filepath.Join(root, "_quarto-topic-book.yml")
	writePNG(t, filepath.Join(root, "cover.png"))
	os.WriteFile(topic, []byte("book:\n  title: Book\n_quarto-vars:\n  topic: book\nqm:\n  cover: cover.png\n"), 0o644)

	if n, err := loadNative(t, root); err != nil || n != nil {
		t.Fatalf("a Quarto format: got %v, %v", n, err)
	}

	os.WriteFile(format, []byte(nativeFormat), 0o644)
	n, err := loadNative(t, root)
	if err != nil {
		t.Fatal(err)
	}
	if n.Cover != filepath.Join(root, "cover.png") || n.Title != "Book" || !n.TOC {
		t.Errorf("got %+v", n)
	}

	broken := map[string]string{
		"format: pdf": strings.Replace(nativeFormat, "format:\n", "format:\n  pdf: default\n", 1),
		"no docx":     strings.Replace(nativeFormat, "format:\n  docx:\n    toc: true\n", "format: html\n", 1),
		"no filter":   strings.Replace(nativeFormat, "    at: post-finalize\n", "", 1),
		"early":       strings.Replace(nativeFormat, "post-finalize", "pre-quarto", 1),
		"renderer":    strings.Replace(nativeFormat, "renderer: qm", "renderer: go", 1),
	}
	for name, body := range broken {
		os.WriteFile(format, []byte(body), 0o644)
		if _, err := loadNative(t, root); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	os.WriteFile(format, []byte(nativeFormat), 0o644)
	os.WriteFile(topic, []byte("_quarto-vars:\n  topic: book\nqm:\n  cover: missing.png\n"), 0o644)
	if _, err := loadNative(t, root); err == nil {
		t.Error("missing cover accepted")
	}
}

func TestNativeRenderIsAPlainQuartoRender(t *testing.T) {
	root := project(t)
	os.WriteFile(filepath.Join(root, "_quarto-format-handout.yml"), []byte(nativeFormat), 0o644)
	callLog := stubQuarto(t, "exit 0\n")
	if err := renderSelection(root, sel("book", "handout", "std"), true, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	if readLog(t, callLog) != "" {
		t.Fatal("dry run executed Quarto")
	}
	if _, err := os.Stat(BuildDirPath(root)); !os.IsNotExist(err) {
		t.Fatal("dry run created build directory")
	}
	if err := renderSelection(root, sel("book", "handout", "std"), false, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	if got, want := readLog(t, callLog), "args: render --profile topic-book,format-handout,audience-std --no-clean\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	os.WriteFile(filepath.Join(root, "_quarto-topic-book.yml"), []byte("qm:\n  cover: missing.png\n"), 0o644)
	os.Remove(callLog)
	if err := renderSelection(root, sel("book", "handout", "std"), false, func(string, ...any) {}); err == nil {
		t.Fatal("missing cover accepted")
	}
	if readLog(t, callLog) != "" {
		t.Fatal("invalid cover reached Quarto")
	}
}

func TestNativePrepareRemovesAStaleCapture(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(BuildDirPath(root), 0o755)
	os.WriteFile(CapturedDocFile(root), []byte("{}"), 0o644)
	if err := (&Native{}).Prepare(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(CapturedDocFile(root)); !os.IsNotExist(err) {
		t.Error("stale capture kept")
	}
	if b, err := os.ReadFile(CaptureFilterFile(root)); err != nil || !strings.Contains(string(b), "qm-document.json") {
		t.Errorf("capture filter not written: %v", err)
	}
}

// Opt-in because Quarto is an external integration dependency. The render is
// a plain `quarto render`, with qm only in the project's hooks: the qm
// renderer's output must not depend on being started by `qm render`.
func TestNativeQuartoIntegration(t *testing.T) {
	if os.Getenv("QM_TEST_QUARTO") == "" {
		t.Skip("set QM_TEST_QUARTO=1")
	}
	quarto, err := exec.LookPath("quarto")
	if err != nil {
		t.Skip(err)
	}
	qm := filepath.Join(t.TempDir(), "qm")
	build := exec.Command("go", "build", "-o", qm, ".")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	root := t.TempDir()
	files := map[string]string{
		"_quarto.yml": "project:\n  type: book\n  pre-render: " + qm + " prepare\n  post-render: " + qm + " finalize\n" +
			"book:\n  chapters: [index.qmd]\nfilters: [audience.lua]\n",
		"_quarto-format-handout.yml": nativeFormat,
		"_quarto-topic-book.yml":     "book:\n  title: Integration\n_quarto-vars:\n  topic: book\nqm:\n  cover: cover.png\n",
		"_quarto-audience-std.yml":   "_quarto-vars:\n  audience: \"\"\n",
		"audience.lua":               "function Div(el) if el.classes:includes('private') then return {} end end\n",
		"index.qmd":                  "{{< include _build/book.qmd >}}\n",
		"_build/book.qmd":            "",
		"book/index.qmd":             "---\norder: 1\n---\n# Überblick\n\nVisible ÄÖÜ **bold** and [link](https://example.org).\n\n::: private\nEXCLUDED-AUDIENCE-TEXT\n:::\n\n## Table\n\n| A | B |\n|---|---|\n| One | Two |\n",
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writePNG(t, filepath.Join(root, "cover.png"))
	cmd := exec.Command(quarto, "render", "--profile", "topic-book,format-handout,audience-std")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("quarto render: %v\n%s", err, out)
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
	pdf, err := os.ReadFile(filepath.Join(root, "_output/handout/book.pdf"))
	if err != nil || len(pdf) < 1000 {
		t.Fatal("PDF missing or empty")
	}
	if strings.Contains(string(pdf), "TeX") {
		t.Fatal("the PDF was typeset by LaTeX")
	}
	if _, err := os.Stat(CapturedDocFile(root)); !os.IsNotExist(err) {
		t.Error("captured document left behind")
	}
}
