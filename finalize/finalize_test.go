package finalize

import (
	"archive/zip"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a project with the two format profiles that need a
// post-render fix-up, plus a book format that needs none.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"_quarto.yml":                 "project:\n  type: book\n",
		"_quarto-topic-calltaker.yml": "_quarto-vars:\n  topic: calltaker\n",
		// A book names itself: Quarto resolves book: output-file: and puts
		// the file where it belongs, so nothing is left to do here.
		"_quarto-format-handout.yml": "project:\n  type: book\n  output-dir: _output/handout\n" +
			"book:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n",
		// A deck is not a book: a type: default project mirrors the source
		// layout and has no interpolating key to name the output with.
		"_quarto-format-slides.yml": "project:\n  type: default\n  output-dir: _output/slides\n" +
			"qm:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n",
		// The website's directory depends on the audience, and
		// project: output-dir: does not interpolate.
		"_quarto-format-website.yml": "project:\n  type: website\n  output-dir: _output/site\n" +
			"qm:\n  output-dir-suffix: \"{{< var audience >}}\"\n",
		"_quarto-audience-pol.yml": "_quarto-vars:\n  audience: \"-pol\"\n",
		"_quarto-audience-std.yml": "_quarto-vars:\n  audience: \"\"\n",
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

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

// The deck lands under the name of the source file, in a mirror of the
// source directory. It is moved to the top of the output directory and
// named from the topic and the audience.
func TestRunRenamesTheDeck(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/slides/_build/slides.pptx", "deck")

	err := Run(root, []string{"topic-calltaker", "format-slides", "audience-pol"},
		filepath.Join(root, "_output/slides"),
		[]string{"_output/slides/_build/slides.pptx"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exists(root, "_output/slides/calltaker-pol.pptx") {
		t.Error("the deck was not renamed")
	}
	// The mirrored source directory is not part of the output.
	if exists(root, "_output/slides/_build") {
		t.Error("the mirrored _build directory was left behind")
	}
}

func TestRunRenamesTheOutputDirPerAudience(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/site/index.html", "site")

	err := Run(root, []string{"topic-calltaker", "format-website", "audience-pol"},
		filepath.Join(root, "_output/site"), []string{"_output/site/index.html"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exists(root, "_output/site-pol/index.html") {
		t.Error("the output directory was not renamed")
	}
	if exists(root, "_output/site") {
		t.Error("the original output directory is still there")
	}
}

// An audience whose suffix is empty leaves the directory where it is.
func TestRunLeavesTheDirAloneForAnEmptySuffix(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/site/index.html", "site")

	err := Run(root, []string{"topic-calltaker", "format-website", "audience-std"},
		filepath.Join(root, "_output/site"), []string{"_output/site/index.html"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exists(root, "_output/site/index.html") {
		t.Error("the output directory was moved despite an empty suffix")
	}
}

// A previous render's directory is replaced, not merged into.
func TestRunReplacesAnOlderOutputDir(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/site/index.html", "new")
	write(t, root, "_output/site-pol/stale.html", "old")

	err := Run(root, []string{"topic-calltaker", "format-website", "audience-pol"},
		filepath.Join(root, "_output/site"), []string{"_output/site/index.html"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exists(root, "_output/site-pol/stale.html") {
		t.Error("the previous render's files survived")
	}
}

// A book needs no fix-up: Quarto resolves book: output-file: itself.
func TestRunDoesNothingForABook(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/handout/calltaker-pol.docx", "book")

	err := Run(root, []string{"topic-calltaker", "format-handout", "audience-pol"},
		filepath.Join(root, "_output/handout"),
		[]string{"_output/handout/calltaker-pol.docx"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exists(root, "_output/handout/calltaker-pol.docx") {
		t.Error("the book output was touched")
	}
}

// The qm renderer: Quarto wrote the DOCX and the capture filter saved the
// document; finalize lays out the PDF beside the DOCX and adds the covers.
func TestRunCompletesTheQMRenderer(t *testing.T) {
	root := fixture(t)
	write(t, root, "_quarto-format-handbook.yml", "project:\n  type: book\n  output-dir: _output/handbook\n"+
		"book:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n"+
		"qm:\n  renderer: qm\n"+
		"filters:\n  - path: _build/qm-capture.lua\n    at: post-finalize\n"+
		"format:\n  docx: default\n")
	write(t, root, "_quarto-topic-calltaker.yml", "_quarto-vars:\n  topic: calltaker\nqm:\n  cover: cover.png\n")
	cover, err := os.Create(filepath.Join(root, "cover.png"))
	if err != nil {
		t.Fatal(err)
	}
	png.Encode(cover, image.NewRGBA(image.Rect(0, 0, 100, 140)))
	cover.Close()
	write(t, root, "_build/qm-document.json",
		`{"pandoc-api-version":[1,23,1],"meta":{},"blocks":[{"t":"Para","c":[{"t":"Str","c":"Hello"}]}]}`)
	docx := filepath.Join(root, "_output/handbook/calltaker-pol.docx")
	os.MkdirAll(filepath.Dir(docx), 0o755)
	f, err := os.Create(docx)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"word/document.xml":            `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p/><w:sectPr/></w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
		"[Content_Types].xml":          `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
	} {
		w, _ := zw.Create(name)
		io.WriteString(w, body)
	}
	zw.Close()
	f.Close()

	err = Run(root, []string{"topic-calltaker", "format-handbook", "audience-pol"},
		filepath.Join(root, "_output/handbook"),
		[]string{"_output/handbook/calltaker-pol.docx"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if info, err := os.Stat(filepath.Join(root, "_output/handbook/calltaker-pol.pdf")); err != nil || info.Size() < 1000 {
		t.Errorf("PDF missing or empty: %v", err)
	}
	z, err := zip.OpenReader(docx)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	covered := false
	for _, f := range z.File {
		covered = covered || f.Name == "word/media/qm-cover.png"
	}
	if !covered {
		t.Error("the DOCX has no cover")
	}
	if exists(root, "_build/qm-document.json") {
		t.Error("the captured document was left behind")
	}
}

// Without a captured document there is nothing to lay out the PDF from.
func TestRunFailsWithoutACapture(t *testing.T) {
	root := fixture(t)
	write(t, root, "_quarto-format-handbook.yml", "project:\n  type: book\n  output-dir: _output/handbook\n"+
		"qm:\n  renderer: qm\n"+
		"filters:\n  - path: _build/qm-capture.lua\n    at: post-finalize\n"+
		"format:\n  docx: default\n")
	write(t, root, "_output/handbook/x.docx", "")
	err := Run(root, []string{"topic-calltaker", "format-handbook", "audience-pol"},
		filepath.Join(root, "_output/handbook"), []string{"_output/handbook/x.docx"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not captured") {
		t.Errorf("error = %v, want a missing-capture error", err)
	}
}

func TestRunRejectsBadSelections(t *testing.T) {
	root := fixture(t)
	if err := Run(root, nil, "", nil, nil); err == nil {
		t.Error("expected an error without a profile")
	}
	err := Run(root, []string{"topic-calltaker", "format-slides", "audience-pol"},
		filepath.Join(root, "_output/slides"), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no output files") {
		t.Errorf("error = %v, want a missing-output-files error", err)
	}
}

func TestSplitFiles(t *testing.T) {
	got := splitFiles("_output/a.docx\n_output/b.epub\n")
	if len(got) != 2 || got[0] != "_output/a.docx" || got[1] != "_output/b.epub" {
		t.Errorf("splitFiles = %v", got)
	}
	if got := splitFiles(""); got != nil {
		t.Errorf("splitFiles = %v, want nil", got)
	}
}

// A preview server keeps reading the files it just rendered, under the path
// it reported, so finalize leaves everything where Quarto put it.
func TestRunDoesNothingUnderPreview(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/site/index.html", "site")
	underPreview = func() bool { return true }
	t.Cleanup(func() { underPreview = previewing })

	err := Run(root, []string{"topic-calltaker", "format-website", "audience-pol"},
		filepath.Join(root, "_output/site"), []string{"_output/site/index.html"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exists(root, "_output/site/index.html") {
		t.Error("the output directory was renamed during a preview")
	}
	if exists(root, "_output/site-pol") {
		t.Error("the suffixed directory was created during a preview")
	}
}

func TestIsQuartoPreview(t *testing.T) {
	cases := map[string]bool{
		"/opt/quarto/bin/tools/x86_64/deno run /opt/quarto/bin/quarto.js preview": true,
		"quarto preview":                     true,
		"/usr/local/bin/quarto serve _site":  true,
		"quarto render --profile a,b":        false,
		"bash -c 'cd doc && make'":           false,
		"vim quarto-preview-notes.md":        false,
		"/opt/quarto/bin/quarto.js render .": false,
	}
	for args, want := range cases {
		if got := isQuartoPreview(args); got != want {
			t.Errorf("isQuartoPreview(%q) = %v, want %v", args, got, want)
		}
	}
}

func TestPreviewingHonoursTheOverride(t *testing.T) {
	t.Setenv("QM_FINALIZE_PREVIEW", "1")
	if !previewing() {
		t.Error("QM_FINALIZE_PREVIEW=1 was not honoured")
	}
	t.Setenv("QM_FINALIZE_PREVIEW", "0")
	if previewing() {
		t.Error("QM_FINALIZE_PREVIEW=0 was not honoured")
	}
}
