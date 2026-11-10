package docx

import (
	"bufio"
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"

	"github.com/christophberger-ailab/qm/internal/markdown"
)

// runProps are the character properties an inline inherits.
type runProps struct {
	bold, italic, strike bool
	code                 bool // inline code
	link                 bool // inside a hyperlink
	sup                  bool
}

func (rp runProps) String(s *styleSet) string {
	var b strings.Builder
	switch {
	case rp.link:
		fmt.Fprintf(&b, `<w:rStyle w:val="%s"/>`, esc(s.hyperlink()))
		if rp.code {
			b.WriteString(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Courier New"/>`)
		}
	case rp.code:
		fmt.Fprintf(&b, `<w:rStyle w:val="%s"/>`, esc(s.verbatim()))
	}
	if rp.bold {
		b.WriteString(`<w:b/>`)
	}
	if rp.italic {
		b.WriteString(`<w:i/>`)
	}
	if rp.strike {
		b.WriteString(`<w:strike/>`)
	}
	if rp.sup {
		b.WriteString(`<w:vertAlign w:val="superscript"/>`)
	}
	if b.Len() == 0 {
		return ""
	}
	return "<w:rPr>" + b.String() + "</w:rPr>"
}

// run is a run of text with the properties.
func (w *writer) run(rp runProps, text string) string {
	if text == "" {
		return ""
	}
	return fmt.Sprintf(`<w:r>%s<w:t xml:space="preserve">%s</w:t></w:r>`, rp.String(w.styles), esc(text))
}

func (w *writer) inlines(buf *bytes.Buffer, n ast.Node, rp runProps, c ctx) {
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		w.inline(buf, ch, rp, c)
	}
}

var reBR = regexp.MustCompile(`(?i)^<br\s*/?>$`)

func (w *writer) inline(buf *bytes.Buffer, n ast.Node, rp runProps, c ctx) {
	switch n := n.(type) {
	case *ast.Text:
		s := w.text(n.Segment.Value(w.src))
		if n.SoftLineBreak() {
			s += " "
		}
		buf.WriteString(w.run(rp, s))
		if n.HardLineBreak() {
			buf.WriteString(`<w:r><w:br/></w:r>`)
		}
	case *ast.String:
		buf.WriteString(w.run(rp, string(n.Value)))
	case *ast.CodeSpan:
		var b strings.Builder
		for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
			if t, ok := ch.(*ast.Text); ok {
				b.Write(t.Segment.Value(w.src))
			}
		}
		rp.code = true
		buf.WriteString(w.run(rp, strings.ReplaceAll(b.String(), "\n", " ")))
	case *ast.Emphasis:
		if n.Level >= 2 {
			rp.bold = true
		} else {
			rp.italic = true
		}
		w.inlines(buf, n, rp, c)
	case *east.Strikethrough:
		rp.strike = true
		w.inlines(buf, n, rp, c)
	case *ast.Link:
		w.link(buf, w.dest(n.Destination), n, rp, c)
	case *ast.AutoLink:
		dest := string(n.URL(w.src))
		if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(dest), "mailto:") {
			dest = "mailto:" + dest
		}
		label := rp
		label.link = true
		w.hyperlink(buf, dest, w.run(label, string(n.Label(w.src))), c, rp)
	case *ast.Image:
		w.image(buf, n, rp, c)
	case *ast.RawHTML:
		var b strings.Builder
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			b.Write(seg.Value(w.src))
		}
		if reBR.MatchString(strings.TrimSpace(b.String())) {
			buf.WriteString(`<w:r><w:br/></w:r>`)
		}
	case *markdown.Span:
		if id := n.Attrs.ID; id != "" {
			buf.WriteString(w.bookmarkStart(id))
			bm := w.bmID
			w.inlines(buf, n, rp, c)
			fmt.Fprintf(buf, `<w:bookmarkEnd w:id="%d"/>`, bm)
			return
		}
		w.inlines(buf, n, rp, c)
	case *east.FootnoteLink:
		w.footnoteRef(buf, n, c)
	default:
		w.inlines(buf, n, rp, c)
	}
}

// text resolves backslash escapes and character references the way
// goldmark's own renderer does.
func (w *writer) text(b []byte) string {
	var h bytes.Buffer
	bw := bufio.NewWriter(&h)
	html.DefaultWriter.Write(bw, b)
	bw.Flush()
	return unescapeHTML(h.String())
}

// unescapeHTML undoes the HTML escaping goldmark's writer applies to the
// text it has resolved.
func unescapeHTML(s string) string {
	return strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`).Replace(s)
}

// dest is a link or image destination with escapes resolved.
func (w *writer) dest(b []byte) string {
	return string(util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(b))))
}

// link writes a link whose label is the node's children.
func (w *writer) link(buf *bytes.Buffer, dest string, n ast.Node, rp runProps, c ctx) {
	if rp.link || dest == "" {
		w.inlines(buf, n, rp, c) // hyperlinks do not nest
		return
	}
	var inner bytes.Buffer
	lp := rp
	lp.link = true
	w.inlines(&inner, n, lp, c)
	w.hyperlink(buf, dest, inner.String(), c, rp)
}

// hyperlink wraps runs in a hyperlink: internal for #anchor, else external.
// Footnotes have no relationships of their own, so there it is a field.
func (w *writer) hyperlink(buf *bytes.Buffer, dest, inner string, c ctx, rp runProps) {
	switch {
	case rp.link:
		buf.WriteString(inner)
	case strings.HasPrefix(dest, "#"):
		anchor, err := url.PathUnescape(dest[1:])
		if err != nil {
			anchor = dest[1:]
		}
		fmt.Fprintf(buf, `<w:hyperlink w:anchor="%s" w:history="1">%s</w:hyperlink>`, esc(bookmarkName(anchor)), inner)
	case c.note:
		fmt.Fprintf(buf, `<w:fldSimple w:instr="%s">%s</w:fldSimple>`,
			esc(` HYPERLINK "`+strings.ReplaceAll(dest, `"`, "%22")+`" `), inner)
	default:
		fmt.Fprintf(buf, `<w:hyperlink r:id="%s" w:history="1">%s</w:hyperlink>`, w.externalRel(dest), inner)
	}
}

// externalRel returns the relationship ID of an external target.
func (w *writer) externalRel(target string) string {
	if id, ok := w.relIDs[target]; ok {
		return id
	}
	id := fmt.Sprintf("rIdQm%d", len(w.rels)+1)
	w.relIDs[target] = id
	w.rels = append(w.rels, relationship{id, relLink, target})
	return id
}
