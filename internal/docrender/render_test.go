package docrender

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func coverPNG(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cover.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 200, 100))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}
func TestCoverDOCXPreservesBodyAndPackage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.docx")
	section := `<w:sectPr><w:headerReference w:type="default" r:id="rId1"/><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:left="1440" w:right="1440" w:bottom="1440"/></w:sectPr>`
	body := `<w:p><w:r><w:t>Original body ÄÖÜ</w:t></w:r></w:p>` + section
	parts := map[string]string{
		"word/document.xml":            `<w:document xmlns:w="` + wordNS + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` + body + `</w:body></w:document>`,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="header" Target="header1.xml"/></Relationships>`,
		"[Content_Types].xml":          `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"word/header1.xml":             "original header", "word/styles.xml": "original styles",
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, s := range parts {
		w, _ := zw.Create(name)
		io.WriteString(w, s)
	}
	zw.Close()
	f.Close()
	if err = CoverDOCX(path, coverPNG(t)); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	got := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		got[f.Name] = string(b)
	}
	for _, name := range []string{"word/header1.xml", "word/styles.xml"} {
		if got[name] != parts[name] {
			t.Errorf("changed %s", name)
		}
	}
	doc := got["word/document.xml"]
	if !strings.Contains(doc, body) {
		t.Fatal("original body/section changed")
	}
	// 200x100 cover on A4 (7560310x10692130 EMU): fit to width, no crop,
	// anchored bottom-right, so the free space is above the image.
	for _, want := range []string{`relativeFrom="page"`, `<wp:posOffset>0</wp:posOffset>`, `<wp:posOffset>6911975</wp:posOffset>`, `cx="7560310" cy="3780155"`, `<w:pgMar w:top="0"`, `w:val="nextPage"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(doc, "srcRect") {
		t.Error("cover must not be cropped")
	}
	for _, name := range []string{"word/document.xml", "word/_rels/document.xml.rels", "[Content_Types].xml"} {
		d := xml.NewDecoder(strings.NewReader(got[name]))
		for {
			_, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("invalid %s: %v", name, err)
			}
		}
	}
	if got["word/media/qm-cover.png"] == "" {
		t.Fatal("cover not embedded")
	}
}

func TestPDFPaginationUnicodeCoverAndLinks(t *testing.T) {
	header := func(s string) Node {
		b, _ := json.Marshal([]any{1, []any{s, []string{}, [][]string{}}, []Node{{T: "Str", C: json.RawMessage(strconvQuote(s))}}})
		return Node{T: "Header", C: b}
	}
	para := func(s string) Node {
		b, _ := json.Marshal([]Node{{T: "Str", C: json.RawMessage(strconvQuote(s))}})
		return Node{T: "Para", C: b}
	}
	d := &Document{Blocks: []Node{header("Überblick"), para(strings.Repeat("Änderung über Größe und Straße. ", 400)), header("Ende"), para("Final paragraph")}}
	path := filepath.Join(t.TempDir(), "book.pdf")
	if err := PDF(path, d, Options{Cover: coverPNG(t), TOC: true, Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"%PDF-", "/Subtype /Image", "/Type /Pages", "/ToUnicode", "/Outlines"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("PDF missing %s", want)
		}
	}
	if bytes.Count(b, []byte("/Type /Page\n")) < 4 {
		t.Fatal("expected cover, contents and multiple body pages")
	}
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }
func TestUnsupportedContentIsNotDiscarded(t *testing.T) {
	_, err := blocks([]Node{{T: "Mystery"}}, 0)
	if err == nil {
		t.Fatal("unknown block accepted")
	}
	if !layoutOnlyXML(`<w:tbl><w:tc><w:tcPr><w:shd w:fill="000000"/></w:tcPr>`) {
		t.Fatal("Quarto callout wrapper rejected")
	}
	if layoutOnlyXML(`<w:p><w:r><w:t>Important content</w:t></w:r></w:p>`) {
		t.Fatal("text-containing XML would be lost")
	}
}

// Optional local regression check against a captured real project document.
func TestCapturedDocument(t *testing.T) {
	path := os.Getenv("QM_TEST_AST")
	if path == "" {
		t.Skip("QM_TEST_AST not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}
	out := os.Getenv("QM_TEST_PDF")
	if out == "" {
		out = filepath.Join(t.TempDir(), "captured.pdf")
	}
	if err = PDF(out, d, Options{Root: os.Getenv("QM_TEST_ROOT"), TOC: true}); err != nil {
		t.Fatal(err)
	}
}
