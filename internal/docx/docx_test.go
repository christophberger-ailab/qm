package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	dx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"

	"github.com/christophberger-ailab/qm/internal/markdown"
)

// build writes Markdown to a DOCX and returns its parts by name. Every XML
// part must be well-formed.
func build(t *testing.T, md string, o Options) map[string]string {
	t.Helper()
	d := markdown.Parse([]byte(md))
	d.Prepare(markdown.Options{})
	path := filepath.Join(t.TempDir(), "out.docx")
	if err := Write(path, d, o); err != nil {
		t.Fatal(err)
	}
	return readParts(t, path)
}

func readParts(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	parts := map[string]string{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		parts[f.Name] = string(b)
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
			dec := xml.NewDecoder(bytes.NewReader(b))
			for {
				if _, err := dec.Token(); err != nil {
					if err != io.EOF {
						t.Errorf("%s is not well-formed: %v", f.Name, err)
					}
					break
				}
			}
		}
	}
	return parts
}

func has(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("missing %q in\n%s", sub, abbreviate(s))
		}
	}
}

func lacks(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			t.Errorf("unexpected %q in\n%s", sub, abbreviate(s))
		}
	}
}

func abbreviate(s string) string {
	if len(s) > 6000 {
		return s[:6000] + "..."
	}
	return s
}

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPackage(t *testing.T) {
	p := build(t, "Hello\n", Options{Title: "My Title", Lang: "de-DE"})
	for _, name := range []string{"[Content_Types].xml", "word/document.xml", "word/styles.xml", "docProps/core.xml"} {
		if _, ok := p[name]; !ok {
			t.Errorf("part %s missing", name)
		}
	}
	has(t, p["docProps/core.xml"], "<dc:title>My Title</dc:title>")
	has(t, p["word/styles.xml"], `w:lang w:val="de-DE"`)
	has(t, p["word/document.xml"], `w:pgSz w:w="11906" w:h="16838"`, "<w:t xml:space=\"preserve\">Hello</w:t>")
}

func TestHeadingsAndLinks(t *testing.T) {
	p := build(t, `# Intro {#intro}

## Second Part

See [below](#second-part), [the site](https://example.com/x?a=1&b=2), <https://auto.example.org> and [again](https://example.com/x?a=1&b=2).
`, Options{})
	doc := p["word/document.xml"]
	has(t, doc, `<w:pStyle w:val="Heading1"/>`, `<w:pStyle w:val="Heading2"/>`,
		`w:name="intro"`, `w:name="second-part"`,
		`<w:hyperlink w:anchor="second-part" w:history="1">`)
	// bookmark starts and ends pair up
	if a, b := strings.Count(doc, "<w:bookmarkStart"), strings.Count(doc, "<w:bookmarkEnd"); a != 2 || a != b {
		t.Errorf("bookmarks: %d starts, %d ends", a, b)
	}
	rels := p["word/_rels/document.xml.rels"]
	has(t, rels, `Target="https://example.com/x?a=1&amp;b=2" TargetMode="External"`, `Target="https://auto.example.org"`)
	if n := strings.Count(rels, "example.com/x"); n != 1 {
		t.Errorf("the same URL got %d relationships", n)
	}
	if n := len(regexp.MustCompile(`<w:hyperlink r:id="rIdQm\d+"`).FindAllString(doc, -1)); n != 3 {
		t.Errorf("%d external hyperlinks, want 3", n)
	}
	has(t, doc, `<w:rStyle w:val="Hyperlink"/>`)
}

func TestInlineFormatting(t *testing.T) {
	p := build(t, "A *it* **bold** ~~gone~~ `mono` end\\\nnext line  \nsecond<br>third &amp; \\* ok\n", Options{})
	doc := p["word/document.xml"]
	has(t, doc, `<w:i/>`, `<w:b/>`, `<w:strike/>`, `<w:rStyle w:val="VerbatimChar"/>`,
		`>mono</w:t>`, `third &amp; * ok`)
	if n := strings.Count(doc, "<w:br/>"); n != 3 {
		t.Errorf("%d line breaks, want 3", n)
	}
	has(t, p["word/styles.xml"], `w:styleId="VerbatimChar"`)
	lacks(t, doc, "&amp;amp;")
}

func TestLists(t *testing.T) {
	p := build(t, `- one
  - nested
- two

3. three
4. four

Text between.

1. first
2. second

   continued paragraph
`, Options{})
	doc := p["word/document.xml"]
	num := p["word/numbering.xml"]
	has(t, doc, `<w:ilvl w:val="1"/>`)
	has(t, num, `w:numFmt w:val="bullet"`, `w:numFmt w:val="decimal"`)
	has(t, p["word/_rels/document.xml.rels"], "numbering.xml")
	has(t, p["[Content_Types].xml"], "/word/numbering.xml")
	// two ordered lists: two instances, one starting at 3, one at 1
	has(t, num, `<w:startOverride w:val="3"/>`)
	if n := strings.Count(num, `<w:num w:numId=`); n != 3 { // bullets + 2 ordered
		t.Errorf("%d num instances, want 3", n)
	}
	ids := regexp.MustCompile(`<w:numId w:val="(\d+)"/>`).FindAllStringSubmatch(doc, -1)
	seen := map[string]bool{}
	for _, m := range ids {
		seen[m[1]] = true
	}
	if len(seen) != 3 {
		t.Errorf("document uses numIds %v, want 3 distinct", seen)
	}
	has(t, doc, "continued paragraph")
}

func TestBlockQuoteAndCode(t *testing.T) {
	p := build(t, "> quoted\n\n```go\nline1\n\n\tindented\n```\n\n    indented code block\n", Options{})
	doc := p["word/document.xml"]
	has(t, doc, `<w:pStyle w:val="Quote"/>`, `<w:pStyle w:val="SourceCode"/>`,
		`>line1</w:t>`, `<w:tab/>`, `>indented</w:t>`, `indented code block`)
	if n := strings.Count(doc, `<w:pStyle w:val="SourceCode"/>`); n != 2 {
		t.Errorf("%d code paragraphs, want 2", n)
	}
	has(t, p["word/styles.xml"], `w:styleId="SourceCode"`, "Consolas")
}

func TestTable(t *testing.T) {
	p := build(t, "| Left | Mid | Right |\n|:--|:-:|--:|\n| a | **b** | c |\n| d | e | f |\n", Options{})
	doc := p["word/document.xml"]
	has(t, doc, "<w:tbl>", "<w:tblHeader/>", `<w:tblBorders>`, `<w:jc w:val="center"/>`, `<w:jc w:val="right"/>`)
	if n := strings.Count(doc, "<w:tr>"); n != 3 {
		t.Errorf("%d rows, want 3", n)
	}
	if n := strings.Count(doc, "<w:gridCol "); n != 3 {
		t.Errorf("%d grid columns, want 3", n)
	}
	// header cells are bold, body cell "a" is not
	has(t, doc, `<w:b/></w:rPr><w:t xml:space="preserve">Left`)
	has(t, doc, `<w:r><w:t xml:space="preserve">a</w:t>`)
}

func TestThematicBreakAndPageBreak(t *testing.T) {
	p := build(t, "one\n\n---\n\n::: {.pagebreak}\n:::\n\ntwo\n", Options{})
	doc := p["word/document.xml"]
	has(t, doc, `<w:pBdr><w:bottom w:val="single"`, `<w:br w:type="page"/>`)
}

func TestDefinitionsDivsSpans(t *testing.T) {
	p := build(t, "Term\n: The *definition*\n\n::: {.custom #box}\nInside [span]{.x #sp} text\n:::\n\n[x](#box) [y](#sp)\n", Options{})
	doc := p["word/document.xml"]
	has(t, doc, `<w:keepNext/>`, `<w:b/>`, `<w:ind w:left="720"/>`, "Inside ", "span", "text",
		`w:name="box"`, `w:name="sp"`, `<w:hyperlink w:anchor="box"`, `<w:hyperlink w:anchor="sp"`)
	if a, b := strings.Count(doc, "<w:bookmarkStart"), strings.Count(doc, "<w:bookmarkEnd"); a != b {
		t.Errorf("%d bookmark starts, %d ends", a, b)
	}
}

func TestFootnotes(t *testing.T) {
	p := build(t, "Text[^a] and more[^b].\n\n[^a]: First note with [a link](https://example.org).\n[^b]: Second.\n", Options{})
	doc := p["word/document.xml"]
	fn := p["word/footnotes.xml"]
	if n := strings.Count(doc, "<w:footnoteReference "); n != 2 {
		t.Errorf("%d footnote references, want 2", n)
	}
	has(t, doc, `<w:footnoteReference w:id="1"/>`, `<w:footnoteReference w:id="2"/>`, `w:val="FootnoteReference"`)
	has(t, fn, `w:type="separator"`, `w:type="continuationSeparator"`, `<w:footnote w:id="1">`, `<w:footnote w:id="2">`,
		"<w:footnoteRef/>", "First note with ", "Second.", "HYPERLINK")
	has(t, p["word/_rels/document.xml.rels"], "/footnotes")
	has(t, p["[Content_Types].xml"], "/word/footnotes.xml")
	has(t, p["word/settings.xml"], "<w:footnotePr>")
	lacks(t, doc, "[^a]")
}

func TestCallouts(t *testing.T) {
	p := build(t, `::: {.callout-warning}
## Careful
Body with **bold**.
:::

::: callout
Plain without title.
:::

::: {.callout-tip title="Own title"}
Tip body.
:::
`, Options{Lang: "de"})
	doc := p["word/document.xml"]
	if n := strings.Count(doc, "<w:tbl>"); n != 3 {
		t.Fatalf("%d tables, want 3", n)
	}
	// title row shaded in a tint of the colour, thick left border in the colour
	has(t, doc, `w:color="FFC107"`, `w:color="6C757D"`, `w:color="198754"`, `w:sz="24"`, `w:fill="FFF6DA"`,
		"Careful", "Own title", "Plain without title.")
	// the plain callout has only a body row: 2 + 1 + 2 rows
	if n := strings.Count(doc, "<w:tr>") + strings.Count(doc, "<w:tr "); n != 5 {
		t.Errorf("%d rows, want 5", n)
	}
	lacks(t, doc, "<w:pStyle w:val=\"Heading2\"/>")
}

func TestCalloutDefaultTitleGerman(t *testing.T) {
	d := markdown.Parse([]byte("::: callout-note\nText\n:::\n"))
	d.Prepare(markdown.Options{Lang: "de"})
	path := filepath.Join(t.TempDir(), "o.docx")
	if err := Write(path, d, Options{Lang: "de"}); err != nil {
		t.Fatal(err)
	}
	has(t, readParts(t, path)["word/document.xml"], "Hinweis", `w:color="0D6EFD"`)
}

func TestImages(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(root, "img", "a.png"), 200, 100)
	writePNG(t, filepath.Join(root, "big.png"), 3000, 1500)
	p := build(t, `![natural](img/a.png)

![rooted](/img/a.png){width=50%}

![abs](img/a.png){width=8cm}

![px](img/a.png){width=300px}

![inch](img/a.png){width=3in}

![big](big.png)

![vector](logo.svg)

Inline ![small](img/a.png){height=1cm} picture.
`, Options{Root: root})
	doc := p["word/document.xml"]
	exts := regexp.MustCompile(`<wp:extent cx="(\d+)" cy="(\d+)"/>`).FindAllStringSubmatch(doc, -1)
	if len(exts) != 7 {
		t.Fatalf("%d pictures, want 7:\n%s", len(exts), abbreviate(doc))
	}
	full := 9072 * 635 // A4 with 2.5 cm margins, in EMU
	want := [][2]int{
		{200 * 9525, 100 * 9525},
		{full / 2, full / 4},
		{8 * 360000, 4 * 360000},
		{300 * 9525, 150 * 9525},
		{3 * 914400, 3 * 914400 / 2},
		{full, full / 2}, // capped at the text width
		{720000, 360000}, // height=1cm keeps the aspect ratio
	}
	for i, w := range want {
		var cx, cy int
		fmt.Sscan(exts[i][1], &cx)
		fmt.Sscan(exts[i][2], &cy)
		if abs(cx-w[0]) > 1 || abs(cy-w[1]) > 1 {
			t.Errorf("picture %d is %d x %d EMU, want %d x %d", i+1, cx, cy, w[0], w[1])
		}
	}
	has(t, doc, `r:embed="rIdQmImg1"`, `descr="natural"`, "[vector]", `<w:jc w:val="center"/>`)
	has(t, p["word/_rels/document.xml.rels"], `Target="media/qm-image1.png"`)
	has(t, p["[Content_Types].xml"], `Extension="png"`)
	if _, ok := p["word/media/qm-image1.png"]; !ok {
		t.Error("media file missing")
	}
	if !strings.HasPrefix(p["word/media/qm-image1.png"], "\x89PNG") {
		t.Error("media file is not the PNG")
	}
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

func TestMissingImage(t *testing.T) {
	d := markdown.Parse([]byte("![x](nope.png)\n"))
	d.Prepare(markdown.Options{})
	err := Write(filepath.Join(t.TempDir(), "o.docx"), d, Options{Root: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "nope.png") {
		t.Errorf("err = %v, want an error naming the image", err)
	}
}

func TestRawBlocks(t *testing.T) {
	p := build(t, "before\n\n```{=openxml}\n<w:p><w:r><w:t>RAW-OOXML</w:t></w:r></w:p>\n```\n\n```{=html}\n<div>NOHTML</div>\n```\n\n```{=openxml}\n<w:p><broken\n```\n\n<div>ALSONO</div>\n\nafter\n", Options{})
	doc := p["word/document.xml"]
	has(t, doc, "<w:p><w:r><w:t>RAW-OOXML</w:t></w:r></w:p>", "before", "after")
	lacks(t, doc, "NOHTML", "ALSONO", "broken")
}

func TestTOC(t *testing.T) {
	md := "# One\n\n## Two\n\n#### Four\n"
	p := build(t, md, Options{TOC: true, Lang: "de"})
	doc := p["word/document.xml"]
	has(t, doc, "Inhaltsverzeichnis", `TOC \o "1-3" \h \z \u`, `w:fldCharType="begin"`, `w:fldCharType="separate"`, `w:fldCharType="end"`,
		`<w:hyperlink w:anchor="one"`, `<w:hyperlink w:anchor="two"`)
	lacks(t, doc, `w:anchor="four"`)
	has(t, p["word/settings.xml"], `<w:updateFields w:val="true"/>`)
	if strings.Index(doc, "Inhaltsverzeichnis") > strings.Index(doc, "Heading1") {
		t.Error("TOC is not at the start")
	}
	en := build(t, md, Options{TOC: true})
	has(t, en["word/document.xml"], ">Contents<")
	lacks(t, build(t, md, Options{})["word/document.xml"], "TOC \\o")
}

// makeTemplate creates a template with docxgo: another page size and
// margins, a header, an old body, and style IDs that differ from docxgo's
// defaults, so that the writer has to find styles by name.
func makeTemplate(t *testing.T) string {
	t.Helper()
	doc := dx.NewDocument()
	sec, err := doc.DefaultSection()
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.SetPageSize(domain.PageSize{Width: 8419, Height: 11906}); err != nil { // A5
		t.Fatal(err)
	}
	if err := sec.SetMargins(domain.Margins{Top: 1000, Right: 1000, Bottom: 1000, Left: 1000, Header: 500, Footer: 500}); err != nil {
		t.Fatal(err)
	}
	hdr, err := sec.Header(domain.HeaderDefault)
	if err != nil {
		t.Fatal(err)
	}
	hp, _ := hdr.AddParagraph()
	hr, _ := hp.AddRun()
	hr.SetText("TEMPLATE-HEADER")
	para, _ := doc.AddParagraph()
	run, _ := para.AddRun()
	run.SetText("OLD-BODY-TEXT")
	raw := filepath.Join(t.TempDir(), "raw.docx")
	if err := doc.SaveAs(raw); err != nil {
		t.Fatal(err)
	}

	// rename the Heading 1 style ID and add a custom style
	p, err := readPkg(mustRead(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	styles := string(p.get("word/styles.xml"))
	styles = strings.ReplaceAll(styles, `w:styleId="Heading1"`, `w:styleId="Titre1"`)
	styles = strings.Replace(styles, "</w:styles>", `<w:style w:type="paragraph" w:customStyle="1" w:styleId="MyCustom"><w:name w:val="My Custom"/></w:style></w:styles>`, 1)
	p.set("word/styles.xml", []byte(styles))
	data, err := p.bytes()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "template.docx")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTemplate(t *testing.T) {
	tpl := makeTemplate(t)
	p := build(t, "# Title\n\nBody with a note[^1] and `code`.\n\n- item\n\n[^1]: Note.\n", Options{Template: tpl, Lang: "de-DE", Title: "Templated"})
	doc := p["word/document.xml"]
	lacks(t, doc, "OLD-BODY-TEXT")
	has(t, doc, "Body with a note",
		`<w:pStyle w:val="Titre1"/>`, // found by name, not by ID
		`w:pgSz w:w="8419" w:h="11906"`, `w:left="1000"`, `<w:headerReference`)
	lacks(t, doc, `w:pgSz w:w="11906"`)
	styles := p["word/styles.xml"]
	has(t, styles, `w:styleId="MyCustom"`, `w:styleId="Titre1"`, `w:styleId="VerbatimChar"`, `w:lang w:val="de-DE"`)
	lacks(t, styles, `w:styleId="Heading1"`)
	// the header part and the relationship to it survive
	var header string
	for name, body := range p {
		if strings.HasPrefix(name, "word/header") && strings.HasSuffix(name, ".xml") {
			header = body
		}
	}
	has(t, header, "TEMPLATE-HEADER")
	has(t, p["word/_rels/document.xml.rels"], "/header")
	has(t, p["docProps/core.xml"], "<dc:title>Templated</dc:title>")
	has(t, p["word/footnotes.xml"], "Note.")
	// the result still opens with docxgo
	path := filepath.Join(t.TempDir(), "o.docx")
	d := markdown.Parse([]byte("# T\n"))
	d.Prepare(markdown.Options{})
	if err := Write(path, d, Options{Template: tpl}); err != nil {
		t.Fatal(err)
	}
	if _, err := dx.OpenDocument(path); err != nil {
		t.Errorf("docxgo cannot open the result: %v", err)
	}
}

func TestTemplateTwice(t *testing.T) {
	// A document written from a template can itself be a template: numbering
	// and footnotes already exist then, and IDs must not collide.
	tpl := makeTemplate(t)
	first := filepath.Join(t.TempDir(), "first.docx")
	d := markdown.Parse([]byte("- a\n\n1. b\n\nx[^1]\n\n[^1]: n\n"))
	d.Prepare(markdown.Options{})
	if err := Write(first, d, Options{Template: tpl}); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "second.docx")
	if err := Write(second, d, Options{Template: first}); err != nil {
		t.Fatal(err)
	}
	p := readParts(t, second)
	num := p["word/numbering.xml"]
	ids := regexp.MustCompile(`<w:num w:numId="(\d+)"`).FindAllStringSubmatch(num, -1)
	seen := map[string]bool{}
	for _, m := range ids {
		if seen[m[1]] {
			t.Errorf("numId %s defined twice", m[1])
		}
		seen[m[1]] = true
	}
	if n := strings.Count(p["word/footnotes.xml"], `w:type="separator"`); n != 1 {
		t.Errorf("%d separators, want 1", n)
	}
	if n := strings.Count(p["word/footnotes.xml"], `<w:footnoteRef/>`); n != 1 {
		t.Errorf("%d footnote bodies, want 1", n)
	}
}

func TestMissingTemplate(t *testing.T) {
	d := markdown.Parse([]byte("x\n"))
	d.Prepare(markdown.Options{})
	if err := Write(filepath.Join(t.TempDir(), "o.docx"), d, Options{Template: "/no/such/template.docx"}); err == nil {
		t.Error("expected an error")
	}
}
