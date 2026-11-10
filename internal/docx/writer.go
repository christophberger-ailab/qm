package docx

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"

	"github.com/christophberger-ailab/qm/internal/markdown"
)

// writer turns a syntax tree into WordprocessingML.
type writer struct {
	doc    *markdown.Document
	src    []byte
	opt    Options
	styles *styleSet
	textW  int // text area width in twips

	out  *bytes.Buffer // where blocks are written; swapped to capture
	lead string        // XML to put at the start of the next paragraph
	err  error

	rels     []relationship
	relIDs   map[string]string // external URL -> relationship ID
	media    []mediaFile
	bmID     int
	drawID   int
	imgTypes map[string]string // extension -> content type

	// numbering
	hasNumbering bool
	absBase      int // first free abstractNumId
	numBase      int // last used numId
	bulletNum    int
	orderedAbs   int
	absDefs      []string
	nums         []string
	numCount     int

	// footnotes
	fnDefs   map[int]*east.Footnote
	fnBase   int // last used footnote ID
	fnCount  int
	fnXML    []string
	fnSource *pkg
}

type mediaFile struct {
	name string
	data []byte
}

// ctx is what nested blocks inherit.
type ctx struct {
	style  string // paragraph style that overrides the body style
	indent int    // extra left indent in twips
	quote  int    // block quote depth
	depth  int    // list depth
	cell   bool   // inside a table cell: compact paragraphs
	note   bool   // inside a footnote: no relationships available
}

func newWriter(d *markdown.Document, o Options, p *pkg, st *styleSet, textW int) *writer {
	w := &writer{
		doc: d, src: d.Source, opt: o, styles: st, textW: textW,
		out: &bytes.Buffer{}, relIDs: map[string]string{}, imgTypes: map[string]string{},
		fnDefs: map[int]*east.Footnote{}, fnSource: p,
	}
	numPart := p.get(numberingPart(p))
	w.hasNumbering = numPart != nil
	w.absBase = maxMatch(numPart, reAbsID) + 1
	w.numBase = maxMatch(numPart, reNumID)
	if fn := p.get(footnotePart(p)); fn != nil {
		w.fnBase = max(maxMatch(fn, reFootnoteID), 0)
	}
	return w
}

var (
	reAbsID      = regexp.MustCompile(`<w:abstractNum\s[^>]*?w:abstractNumId="(\d+)"`)
	reNumID      = regexp.MustCompile(`<w:num\s[^>]*?w:numId="(\d+)"`)
	reFootnoteID = regexp.MustCompile(`<w:footnote\s[^>]*?w:id="(-?\d+)"`)
)

func maxMatch(data []byte, re *regexp.Regexp) int {
	m := 0
	for _, s := range re.FindAllSubmatch(data, -1) {
		if v, err := strconv.Atoi(string(s[1])); err == nil {
			m = max(m, v)
		}
	}
	return m
}

func (w *writer) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

// capture runs f with a fresh output buffer and returns what it wrote.
func (w *writer) capture(f func()) string {
	saved, lead := w.out, w.lead
	w.out = &bytes.Buffer{}
	w.lead = ""
	f()
	s := w.out.String()
	w.out, w.lead = saved, lead
	return s
}

// document writes the whole body.
func (w *writer) document() {
	_ = ast.Walk(w.doc.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if f, ok := n.(*east.Footnote); ok && entering {
			w.fnDefs[f.Index] = f
		}
		return ast.WalkContinue, nil
	})
	if w.opt.TOC {
		w.toc()
	}
	w.blocks(w.doc.Root, ctx{})
}

func (w *writer) blocks(parent ast.Node, c ctx) {
	for ch := parent.FirstChild(); ch != nil; ch = ch.NextSibling() {
		w.block(ch, c)
	}
}

func (w *writer) block(n ast.Node, c ctx) {
	switch n := n.(type) {
	case *ast.Heading:
		w.heading(n, c)
	case *ast.Paragraph, *ast.TextBlock:
		w.paragraph(n, c)
	case *ast.Blockquote:
		c.style = w.styles.quote()
		c.quote++
		w.blocks(n, c)
	case *ast.List:
		w.list(n, c)
	case *ast.FencedCodeBlock:
		info := markdown.CodeInfo(n, w.src)
		switch {
		case info.Raw == "openxml":
			w.rawXML(markdown.Code(n, w.src))
		case info.Raw != "":
			// raw HTML and other formats do not belong in a DOCX
		default:
			w.code(markdown.Code(n, w.src), c)
		}
	case *ast.CodeBlock:
		w.code(markdown.Code(n, w.src), c)
	case *ast.ThematicBreak:
		w.para(pPr{style: c.style, indent: c.indent, border: true, before: 120, after: 120, spacing: true}, "")
	case *ast.HTMLBlock, *east.FootnoteList:
		// raw HTML is ignored; footnotes are placed where they are cited
	case *east.Table:
		w.table(n, c)
	case *east.DefinitionList:
		w.definitions(n, c)
	case *markdown.Div:
		w.div(n, c)
	default:
		w.blocks(n, c)
	}
}

func (w *writer) div(n *markdown.Div, c ctx) {
	if markdown.IsPageBreak(n) {
		w.para(pPr{}, `<w:r><w:br w:type="page"/></w:r>`)
		return
	}
	if id := n.Attrs.ID; id != "" {
		w.lead += w.bookmarkStart(id)
		bm := w.bmID
		defer func() { w.lead += fmt.Sprintf(`<w:bookmarkEnd w:id="%d"/>`, bm) }()
	}
	if cal, ok := markdown.CalloutOf(n); ok {
		w.callout(n, cal, c)
		return
	}
	w.blocks(n, c)
}

func (w *writer) heading(n *ast.Heading, c ctx) {
	level := min(max(n.Level, 1), 6)
	var content bytes.Buffer
	id := markdown.HeadingID(n)
	if id != "" {
		content.WriteString(w.bookmarkStart(id))
	}
	w.inlines(&content, n, runProps{}, c)
	if id != "" {
		fmt.Fprintf(&content, `<w:bookmarkEnd w:id="%d"/>`, w.bmID)
	}
	w.para(pPr{style: w.styles.heading(level)}, content.String())
}

// bookmarkStart returns the start tag of a new bookmark; the matching end
// tag uses w.bmID, which stays until the next bookmark starts.
func (w *writer) bookmarkStart(id string) string {
	w.bmID++
	return fmt.Sprintf(`<w:bookmarkStart w:id="%d" w:name="%s"/>`, w.bmID, esc(bookmarkName(id)))
}

// bookmarkName is the bookmark for an identifier; Word limits names to 40
// characters.
func bookmarkName(id string) string {
	r := []rune(id)
	if len(r) > 40 {
		r = r[:40]
	}
	return string(r)
}

func (w *writer) paragraph(n ast.Node, c ctx) {
	var content bytes.Buffer
	w.inlines(&content, n, runProps{}, c)
	pp := w.paraProps(c)
	if onlyImage(n, w.src) {
		pp.jc = "center"
	}
	w.para(pp, content.String())
}

// onlyImage reports whether a paragraph holds nothing but images.
func onlyImage(n ast.Node, src []byte) bool {
	img := false
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch ch := ch.(type) {
		case *ast.Image:
			img = true
		case *ast.Text:
			if strings.TrimSpace(string(ch.Segment.Value(src))) != "" {
				return false
			}
		default:
			return false
		}
	}
	return img
}

// paraProps are the properties of an ordinary paragraph in a context.
func (w *writer) paraProps(c ctx) pPr {
	pp := pPr{style: c.style, indent: c.indent + 567*max(c.quote-1, 0)}
	if pp.style == "" {
		pp.style = w.styles.body()
	}
	if c.cell {
		pp.spacing, pp.before, pp.after = true, 40, 40
	}
	return pp
}

// pPr are paragraph properties, written in the order the schema demands.
type pPr struct {
	style    string
	keepNext bool
	numID    int // 0: not numbered
	ilvl     int
	border   bool // bottom rule
	shade    string
	spacing  bool
	before   int
	after    int
	indent   int
	hanging  int
	jc       string
}

func (p pPr) String() string {
	var b strings.Builder
	if p.style != "" {
		fmt.Fprintf(&b, `<w:pStyle w:val="%s"/>`, esc(p.style))
	}
	if p.keepNext {
		b.WriteString(`<w:keepNext/>`)
	}
	if p.numID > 0 {
		fmt.Fprintf(&b, `<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, p.ilvl, p.numID)
	}
	if p.border {
		b.WriteString(`<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="808080"/></w:pBdr>`)
	}
	if p.shade != "" {
		fmt.Fprintf(&b, `<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, p.shade)
	}
	if p.spacing {
		fmt.Fprintf(&b, `<w:spacing w:before="%d" w:after="%d"/>`, p.before, p.after)
	}
	if p.indent > 0 || p.hanging > 0 {
		fmt.Fprintf(&b, `<w:ind w:left="%d"`, p.indent)
		if p.hanging > 0 {
			fmt.Fprintf(&b, ` w:hanging="%d"`, p.hanging)
		}
		b.WriteString(`/>`)
	}
	if p.jc != "" {
		fmt.Fprintf(&b, `<w:jc w:val="%s"/>`, p.jc)
	}
	if b.Len() == 0 {
		return ""
	}
	return "<w:pPr>" + b.String() + "</w:pPr>"
}

// para writes one paragraph, with whatever lead-in XML is pending (the
// bookmark of a div, the reference mark of a footnote).
func (w *writer) para(pp pPr, content string) {
	w.out.WriteString("<w:p>")
	w.out.WriteString(pp.String())
	w.out.WriteString(w.lead)
	w.lead = ""
	w.out.WriteString(content)
	w.out.WriteString("</w:p>")
}

// spacer separates a table from what follows it; Word merges adjacent
// tables and ends a cell or the body only with a paragraph.
func (w *writer) spacer() {
	w.out.WriteString(`<w:p><w:pPr><w:spacing w:before="0" w:after="0" w:line="120" w:lineRule="exact"/></w:pPr></w:p>`)
}

// code writes a code block as one monospace paragraph with line breaks.
func (w *writer) code(text string, c ctx) {
	text = strings.TrimRight(text, "\r\n")
	var content strings.Builder
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if i > 0 {
			content.WriteString(`<w:r><w:br/></w:r>`)
		}
		if line == "" {
			continue
		}
		content.WriteString(`<w:r>`)
		for j, part := range strings.Split(line, "\t") {
			if j > 0 {
				content.WriteString(`<w:tab/>`)
			}
			if part != "" {
				fmt.Fprintf(&content, `<w:t xml:space="preserve">%s</w:t>`, esc(part))
			}
		}
		content.WriteString(`</w:r>`)
	}
	w.para(pPr{style: w.styles.code(), indent: c.indent}, content.String())
}

// rawXML inserts a raw `{=openxml}` block when it is well-formed.
func (w *writer) rawXML(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	probe := `<root xmlns:w="` + nsW + `" xmlns:r="` + nsR + `" xmlns:wp="` + nsWP + `" xmlns:a="` + nsA + `" xmlns:pic="` + nsPic + `">` + s + `</root>`
	dec := xml.NewDecoder(strings.NewReader(probe))
	for {
		if _, err := dec.Token(); err != nil {
			if err != io.EOF {
				return // not well-formed: skip rather than corrupt the document
			}
			break
		}
	}
	w.out.WriteString(w.lead)
	w.lead = ""
	w.out.WriteString(s)
}

func (w *writer) definitions(n *east.DefinitionList, c ctx) {
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch ch.(type) {
		case *east.DefinitionTerm:
			var content bytes.Buffer
			w.inlines(&content, ch, runProps{bold: true}, c)
			pp := w.paraProps(c)
			pp.keepNext = true
			w.para(pp, content.String())
		case *east.DefinitionDescription:
			d := c
			d.indent += 720
			w.blocks(ch, d)
		default:
			w.block(ch, c)
		}
	}
}

// toc writes the table of contents: a heading and a TOC field whose result
// is pre-filled with the headings (without page numbers), which Word
// replaces when it updates the field on opening.
func (w *writer) toc() {
	title := "Contents"
	if strings.HasPrefix(strings.ToLower(w.opt.Lang), "de") {
		title = "Inhaltsverzeichnis"
	}
	w.para(pPr{style: w.styles.tocHeading()}, w.run(runProps{}, title))

	type entry struct {
		level int
		id    string
		text  string
	}
	var entries []entry
	_ = ast.Walk(w.doc.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			if h.Level <= 3 && markdown.HeadingID(h) != "" {
				entries = append(entries, entry{h.Level, markdown.HeadingID(h), markdown.PlainText(h, w.src)})
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	begin := `<w:r><w:fldChar w:fldCharType="begin" w:dirty="true"/></w:r>` +
		`<w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>`
	end := `<w:r><w:fldChar w:fldCharType="end"/></w:r>`
	if len(entries) == 0 {
		w.para(pPr{}, begin+end)
		return
	}
	for i, e := range entries {
		var content string
		if i == 0 {
			content = begin
		}
		content += fmt.Sprintf(`<w:hyperlink w:anchor="%s" w:history="1">%s</w:hyperlink>`,
			esc(bookmarkName(e.id)), w.run(runProps{}, e.text))
		w.para(pPr{style: w.styles.toc(e.level)}, content)
	}
	w.para(pPr{}, end)
}
