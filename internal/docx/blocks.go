package docx

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"

	"github.com/christophberger-ailab/qm/internal/markdown"
)

// ---- lists ----------------------------------------------------------------

func (w *writer) list(l *ast.List, c ctx) {
	ilvl := min(c.depth, 8)
	var numID int
	if l.IsOrdered() {
		numID = w.orderedNum(l.Start, ilvl)
	} else {
		numID = w.bulletID()
	}
	textIndent := c.indent + 720*(c.depth+1)
	for item := l.FirstChild(); item != nil; item = item.NextSibling() {
		numbered := pPr{style: w.styles.listPara(), numID: numID, ilvl: ilvl}
		if c.indent > 0 {
			numbered.indent, numbered.hanging = textIndent, 360
		}
		if c.cell {
			numbered.spacing, numbered.before, numbered.after = true, 20, 20
		}
		first := item.FirstChild()
		switch first.(type) {
		case *ast.Paragraph, *ast.TextBlock:
		default:
			w.para(numbered, "") // an item that starts with something else still gets its marker
		}
		for ch := first; ch != nil; ch = ch.NextSibling() {
			isPara := false
			switch ch.(type) {
			case *ast.Paragraph, *ast.TextBlock:
				isPara = true
			}
			switch {
			case isPara && ch == first:
				var content bytes.Buffer
				w.inlines(&content, ch, runProps{}, c)
				w.para(numbered, content.String())
			case isPara:
				var content bytes.Buffer
				w.inlines(&content, ch, runProps{}, c)
				pp := w.paraProps(c)
				pp.indent = textIndent
				w.para(pp, content.String())
			default:
				if _, ok := ch.(*ast.List); ok {
					d := c
					d.depth++
					w.list(ch.(*ast.List), d)
					continue
				}
				d := c
				d.indent, d.depth = textIndent, 0
				w.block(ch, d)
			}
		}
	}
}

// bulletID is the numbering instance all bullet lists share.
func (w *writer) bulletID() int {
	if w.bulletNum == 0 {
		abs := w.newAbstract(true)
		w.bulletNum = w.newNum(abs, "")
	}
	return w.bulletNum
}

// orderedNum makes a numbering instance for one ordered list, so that it
// starts at its own start number.
func (w *writer) orderedNum(start, ilvl int) int {
	if w.orderedAbs == 0 {
		w.orderedAbs = w.newAbstract(false)
	}
	var o strings.Builder
	for i := 0; i < 9; i++ {
		s := 1
		if i == ilvl {
			s = start
		}
		fmt.Fprintf(&o, `<w:lvlOverride w:ilvl="%d"><w:startOverride w:val="%d"/></w:lvlOverride>`, i, s)
	}
	return w.newNum(w.orderedAbs, o.String())
}

var (
	orderedFormats = []string{"decimal", "lowerLetter", "lowerRoman"}
	bulletChars    = []string{"•", "◦", "▪"}
)

func (w *writer) newAbstract(bullet bool) int {
	id := w.absBase
	w.absBase++
	var b strings.Builder
	fmt.Fprintf(&b, `<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="hybridMultilevel"/>`, id)
	for i := 0; i < 9; i++ {
		format, text := orderedFormats[i%3], fmt.Sprintf("%%%d.", i+1)
		if bullet {
			format, text = "bullet", bulletChars[i%3]
		}
		fmt.Fprintf(&b, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%s"/><w:lvlJc w:val="left"/>`+
			`<w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`, i, format, text, 720*(i+1))
	}
	b.WriteString(`</w:abstractNum>`)
	w.absDefs = append(w.absDefs, b.String())
	return id
}

func (w *writer) newNum(abs int, overrides string) int {
	w.numBase++
	w.nums = append(w.nums, fmt.Sprintf(`<w:num w:numId="%d"><w:abstractNumId w:val="%d"/>%s</w:num>`, w.numBase, abs, overrides))
	return w.numBase
}

// ---- tables ---------------------------------------------------------------

const (
	gridColor = "A6A6A6"
	thinGray  = "D0D7DE"
)

func border(side, val string, sz int, color string) string {
	return fmt.Sprintf(`<w:%s w:val="%s" w:sz="%d" w:space="0" w:color="%s"/>`, side, val, sz, color)
}

func (w *writer) table(t *east.Table, c ctx) {
	ncols := len(t.Alignments)
	for ch := t.FirstChild(); ch != nil; ch = ch.NextSibling() {
		ncols = max(ncols, ch.ChildCount())
	}
	if ncols == 0 {
		return
	}
	width := max(w.textW-c.indent, 1440)
	colW := width / ncols
	cc := c
	cc.cell, cc.style = true, ""

	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/>`)
	if c.indent > 0 {
		fmt.Fprintf(&b, `<w:tblInd w:w="%d" w:type="dxa"/>`, c.indent)
	}
	b.WriteString(`<w:tblBorders>`)
	for _, s := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(border(s, "single", 4, gridColor))
	}
	b.WriteString(`</w:tblBorders><w:tblCellMar><w:left w:w="100" w:type="dxa"/><w:right w:w="100" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid>`)
	for i := 0; i < ncols; i++ {
		fmt.Fprintf(&b, `<w:gridCol w:w="%d"/>`, colW)
	}
	b.WriteString(`</w:tblGrid>`)

	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		_, header := row.(*east.TableHeader)
		b.WriteString(`<w:tr><w:trPr><w:cantSplit/>`)
		if header {
			b.WriteString(`<w:tblHeader/>`)
		}
		b.WriteString(`</w:trPr>`)
		n := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			al := east.AlignNone
			if tc, ok := cell.(*east.TableCell); ok {
				al = tc.Alignment
			}
			if al == east.AlignNone && n < len(t.Alignments) {
				al = t.Alignments[n]
			}
			n++
			rp := runProps{bold: header}
			var content bytes.Buffer
			w.inlines(&content, cell, rp, cc)
			pp := pPr{spacing: true, before: 40, after: 40}
			switch al {
			case east.AlignCenter:
				pp.jc = "center"
			case east.AlignRight:
				pp.jc = "right"
			}
			fmt.Fprintf(&b, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>`, colW)
			if header {
				b.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>`)
			}
			b.WriteString(`</w:tcPr><w:p>` + pp.String() + content.String() + `</w:p></w:tc>`)
		}
		for ; n < ncols; n++ { // short rows get empty cells
			fmt.Fprintf(&b, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/></w:tcPr><w:p/></w:tc>`, colW)
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	w.out.WriteString(w.lead)
	w.lead = ""
	w.out.WriteString(b.String())
	w.spacer()
}

// ---- callouts -------------------------------------------------------------

var calloutColors = map[string]string{
	"note": "0D6EFD", "tip": "198754", "warning": "FFC107", "important": "DC3545", "caution": "FD7E14", "": "6C757D",
}

// tint mixes a colour with white.
func tint(hex string, keep float64) string {
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return "F2F2F2"
	}
	mix := func(c uint64) uint64 { return uint64(float64(c)*keep + 255*(1-keep) + 0.5) }
	return fmt.Sprintf("%02X%02X%02X", mix(v>>16&255), mix(v>>8&255), mix(v&255))
}

// callout writes a callout as a one-column table: an optional title row in a
// light tint of the type colour, and the body row. The left border is thick
// and in the type colour.
func (w *writer) callout(n *markdown.Div, cal markdown.Callout, c ctx) {
	color := calloutColors[cal.Type]
	width := max(w.textW-c.indent, 1440)
	borders := func(extra string) string {
		return `<w:tcBorders>` + border("top", "single", 4, thinGray) + border("left", "single", 24, color) +
			border("bottom", "single", 4, thinGray) + border("right", "single", 4, thinGray) + `</w:tcBorders>` + extra
	}
	cc := c
	cc.cell, cc.style, cc.indent, cc.depth, cc.quote = true, "", 0, 0, 0

	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/>`)
	if c.indent > 0 {
		fmt.Fprintf(&b, `<w:tblInd w:w="%d" w:type="dxa"/>`, c.indent)
	}
	b.WriteString(`<w:tblBorders>`)
	for _, s := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(border(s, "nil", 0, "auto"))
	}
	b.WriteString(`</w:tblBorders><w:tblCellMar><w:top w:w="60" w:type="dxa"/><w:left w:w="160" w:type="dxa"/>` +
		`<w:bottom w:w="60" w:type="dxa"/><w:right w:w="160" w:type="dxa"/></w:tblCellMar></w:tblPr>`)
	fmt.Fprintf(&b, `<w:tblGrid><w:gridCol w:w="%d"/></w:tblGrid>`, width)

	if cal.Title != "" {
		title := w.run(runProps{bold: true}, cal.Title)
		pp := pPr{keepNext: true, spacing: true, before: 40, after: 40}
		fmt.Fprintf(&b, `<w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s</w:tcPr><w:p>%s%s</w:p></w:tc></w:tr>`,
			width, borders(`<w:shd w:val="clear" w:color="auto" w:fill="`+tint(color, 0.15)+`"/>`), pp.String(), title)
	}
	body := w.capture(func() { w.blocks(n, cc) })
	if body == "" {
		body = `<w:p/>`
	}
	fmt.Fprintf(&b, `<w:tr><w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s</w:tcPr>%s</w:tc></w:tr></w:tbl>`,
		width, borders(""), body)

	w.out.WriteString(w.lead)
	w.lead = ""
	w.out.WriteString(b.String())
	w.spacer()
}

// ---- footnotes ------------------------------------------------------------

// footnoteRef writes a footnote reference and, in the footnotes part, the
// note itself. A note cited twice is written twice; Word has no way to cite
// one note from two places.
func (w *writer) footnoteRef(buf *bytes.Buffer, n *east.FootnoteLink, c ctx) {
	def := w.fnDefs[n.Index]
	if def == nil || c.note {
		buf.WriteString(w.run(runProps{sup: true}, strconv.Itoa(n.Index)))
		return
	}
	w.fnBase++
	id := w.fnBase
	refStyle := esc(w.styles.footnoteRef())
	body := w.capture(func() {
		w.lead = fmt.Sprintf(`<w:r><w:rPr><w:rStyle w:val="%s"/></w:rPr><w:footnoteRef/></w:r><w:r><w:t xml:space="preserve"> </w:t></w:r>`, refStyle)
		w.blocks(def, ctx{style: w.styles.footnoteText(), note: true})
		if w.lead != "" {
			w.para(pPr{style: w.styles.footnoteText()}, "")
		}
	})
	w.fnXML = append(w.fnXML, fmt.Sprintf(`<w:footnote w:id="%d">%s</w:footnote>`, id, body))
	fmt.Fprintf(buf, `<w:r><w:rPr><w:rStyle w:val="%s"/></w:rPr><w:footnoteReference w:id="%d"/></w:r>`, refStyle, id)
}

// ---- package parts --------------------------------------------------------

func numberingPart(p *pkg) string {
	if t := relTarget(p, relNumber); t != "" {
		return t
	}
	return "word/numbering.xml"
}

func footnotePart(p *pkg) string {
	if t := relTarget(p, relFootnote); t != "" {
		return t
	}
	return "word/footnotes.xml"
}

const xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

var reSeparators = regexp.MustCompile(`(?s)<w:footnote\b[^>]*w:type="(?:separator|continuationSeparator)"[^>]*>.*?</w:footnote>`)

const defaultSeparators = `<w:footnote w:type="separator" w:id="-1"><w:p><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:r><w:separator/></w:r></w:p></w:footnote>` +
	`<w:footnote w:type="continuationSeparator" w:id="0"><w:p><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:r><w:continuationSeparator/></w:r></w:p></w:footnote>`

// finishParts writes everything the body needs besides document.xml: media,
// numbering, footnotes, relationships, content types and settings.
func (w *writer) finishParts(p *pkg) {
	rels := w.rels
	for _, m := range w.media {
		p.set("word/media/"+m.name, m.data)
	}
	for ext, ct := range w.imgTypes {
		ensureContentType(p, ext, ct, false)
	}

	if len(w.nums) > 0 {
		name := numberingPart(p)
		abs, nums := strings.Join(w.absDefs, ""), strings.Join(w.nums, "")
		if x := p.get(name); x != nil {
			s := insertBeforeFirst(string(x), []string{"<w:num ", "<w:numIdMacAtCleanup"}, "</w:numbering>", abs)
			s = insertBeforeFirst(s, []string{"<w:numIdMacAtCleanup"}, "</w:numbering>", nums)
			p.set(name, []byte(s))
		} else {
			p.set(name, []byte(xmlDecl+`<w:numbering xmlns:w="`+nsW+`">`+abs+nums+`</w:numbering>`))
			ensureContentType(p, "/"+name, "application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml", true)
			rels = append(rels, relationship{"rIdQmNum", relNumber, "numbering.xml"})
		}
	}

	if len(w.fnXML) > 0 {
		name := footnotePart(p)
		seps := defaultSeparators
		if x := p.get(name); x != nil {
			if found := reSeparators.FindAll(x, -1); len(found) > 0 {
				seps = string(bytes.Join(found, nil))
			}
		} else {
			ensureContentType(p, "/"+name, "application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml", true)
			rels = append(rels, relationship{"rIdQmFootnotes", relFootnote, "footnotes.xml"})
		}
		p.set(name, []byte(xmlDecl+`<w:footnotes xmlns:w="`+nsW+`" xmlns:r="`+nsR+`">`+seps+strings.Join(w.fnXML, "")+`</w:footnotes>`))
	}

	addRels(p, rels)
	patchSettings(p, w.opt.TOC, len(w.fnXML) > 0)
}
