package docrender

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"

	"github.com/christophberger-ailab/qm/internal/markdown"
)

// PDFMarkdown lays out a parsed and prepared Markdown document as a PDF.
// It is the same layout PDF uses for a Pandoc document; only the reading
// of the input differs.
func PDFMarkdown(ctx context.Context, path string, d *markdown.Document, o Options) error {
	o.Markdown = d
	return PDFContext(ctx, path, nil, o)
}

// FromMarkdown converts a Markdown syntax tree into the PDF layout's
// blocks.
func FromMarkdown(d *markdown.Document) ([]Block, error) {
	c := &mdConverter{src: d.Source, notes: map[int]ast.Node{}}
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if fn, ok := n.(*east.Footnote); ok && entering {
			c.notes[fn.Index] = fn
		}
		return ast.WalkContinue, nil
	})
	return c.blocks(d.Root, 0)
}

type mdConverter struct {
	src   []byte
	notes map[int]ast.Node
}

// Quarto's callout colours, as border and title fill.
var calloutColors = map[string][2][3]uint8{
	"note":      {{0x0d, 0x6e, 0xfd}, {0xda, 0xe6, 0xfb}},
	"tip":       {{0x19, 0x87, 0x54}, {0xd6, 0xec, 0xe1}},
	"warning":   {{0xff, 0xc1, 0x07}, {0xff, 0xf2, 0xcd}},
	"important": {{0xdc, 0x35, 0x45}, {0xf7, 0xd8, 0xdb}},
	"caution":   {{0xfd, 0x7e, 0x14}, {0xff, 0xe5, 0xd0}},
}

func (c *mdConverter) blocks(parent ast.Node, indent int) ([]Block, error) {
	var out []Block
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		bb, err := c.block(n, indent)
		if err != nil {
			return nil, err
		}
		out = append(out, bb...)
	}
	return out, nil
}

func (c *mdConverter) block(n ast.Node, indent int) ([]Block, error) {
	b := Block{Indent: indent}
	switch x := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		rr, err := c.inlines(n, Run{})
		if err != nil || len(rr) == 0 {
			return nil, err
		}
		b.Runs = rr
	case *ast.Heading:
		rr, err := c.inlines(n, Run{})
		if err != nil {
			return nil, err
		}
		b.Level, b.ID, b.Runs = x.Level, markdown.HeadingID(x), rr
	case *markdown.Div:
		if markdown.IsPageBreak(x) {
			b.Break = true
			break
		}
		if co, ok := markdown.CalloutOf(x); ok {
			body, err := c.blocks(x, 0)
			if err != nil {
				return nil, err
			}
			cl := &Callout{Border: [3]uint8{0x90, 0x90, 0x90}, Body: body}
			if col, ok := calloutColors[co.Type]; ok {
				cl.Border, cl.Fill, cl.HasFill = col[0], col[1], true
			}
			if co.Title != "" {
				cl.Title = []Block{{Runs: []Run{{Text: co.Title, Bold: true}}}}
			}
			b.Callout = cl
			break
		}
		return c.blocks(x, indent)
	case *ast.Blockquote:
		return c.blocks(x, indent+1)
	case *ast.List:
		var out []Block
		i := 0
		for item := x.FirstChild(); item != nil; item = item.NextSibling() {
			bb, err := c.blocks(item, indent+1)
			if err != nil {
				return nil, err
			}
			first := 0
			for first < len(bb)-1 && bb[first].Break {
				first++
			}
			if len(bb) > 0 && !bb[first].Break {
				prefix := "• "
				if x.IsOrdered() {
					prefix = fmt.Sprintf("%d. ", x.Start+i)
				}
				bb[first].Runs = append([]Run{{Text: prefix}}, bb[first].Runs...)
			}
			out = append(out, bb...)
			i++
		}
		return out, nil
	case *east.DefinitionList:
		var out []Block
		for item := x.FirstChild(); item != nil; item = item.NextSibling() {
			switch item.(type) {
			case *east.DefinitionTerm:
				rr, err := c.inlines(item, Run{Bold: true})
				if err != nil {
					return nil, err
				}
				out = append(out, Block{Runs: rr, Indent: indent})
			default:
				bb, err := c.blocks(item, indent+1)
				if err != nil {
					return nil, err
				}
				out = append(out, bb...)
			}
		}
		return out, nil
	case *ast.FencedCodeBlock:
		info := markdown.CodeInfo(x, c.src)
		if info.Raw != "" {
			raw := markdown.Code(x, c.src)
			if info.Raw == "openxml" && strings.Contains(raw, `w:type="page"`) {
				b.Break = true
				break
			}
			return nil, nil
		}
		b.Code = true
		b.Runs = []Run{{Text: strings.TrimRight(markdown.Code(x, c.src), "\n"), Code: true}}
	case *ast.CodeBlock:
		b.Code = true
		b.Runs = []Run{{Text: strings.TrimRight(markdown.Code(x, c.src), "\n"), Code: true}}
	case *ast.ThematicBreak:
		b.Runs = []Run{{Text: "────────────────────"}}
	case *ast.HTMLBlock:
		return nil, nil
	case *east.Table:
		for row := x.FirstChild(); row != nil; row = row.NextSibling() {
			var cells []Cell
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				style := Run{}
				if _, head := row.(*east.TableHeader); head {
					style.Bold = true
				}
				rr, err := c.inlines(cell, style)
				if err != nil {
					return nil, err
				}
				cells = append(cells, cellContent([]Block{{Runs: rr}}))
			}
			b.Rows = append(b.Rows, cells)
		}
		if len(b.Rows) > 0 {
			b.Widths = make([]float64, len(b.Rows[0]))
		}
	case *east.FootnoteList:
		var out []Block
		for fn := x.FirstChild(); fn != nil; fn = fn.NextSibling() {
			note, ok := fn.(*east.Footnote)
			if !ok {
				continue
			}
			bb, err := c.blocks(note, indent)
			if err != nil {
				return nil, err
			}
			if len(bb) > 0 {
				bb[0].Runs = append([]Run{{Text: fmt.Sprintf("[%d] ", note.Index)}}, bb[0].Runs...)
			}
			out = append(out, bb...)
		}
		if len(out) > 0 {
			out = append([]Block{{Runs: []Run{{Text: "────────"}}}}, out...)
		}
		return out, nil
	default:
		if n.Type() == ast.TypeBlock {
			return c.blocks(n, indent)
		}
		return nil, fmt.Errorf("unsupported Markdown block %s", n.Kind())
	}
	return []Block{b}, nil
}

func (c *mdConverter) inlines(parent ast.Node, style Run) ([]Run, error) {
	var out []Run
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		r := style
		descend := false
		switch x := n.(type) {
		case *ast.Text:
			r.Text = string(x.Segment.Value(c.src))
			out = append(out, r)
			if x.HardLineBreak() {
				out = append(out, Run{Text: "\n"})
			} else if x.SoftLineBreak() {
				out = append(out, Run{Text: " "})
			}
			continue
		case *ast.String:
			r.Text = string(x.Value)
		case *ast.CodeSpan:
			r.Text = markdown.PlainText(x, c.src)
			r.Code = true
		case *ast.Emphasis:
			if x.Level >= 2 {
				r.Bold = true
			} else {
				r.Italic = true
			}
			descend = true
		case *ast.Link:
			r.Link = string(x.Destination)
			descend = true
		case *ast.AutoLink:
			r.Text = string(x.Label(c.src))
			r.Link = string(x.URL(c.src))
		case *ast.Image:
			r.Image = string(x.Destination)
			a := markdown.AttrsOf(x)
			r.ImageWidth, _ = a.Get("width")
			r.ImageHeight, _ = a.Get("height")
		case *ast.RawHTML:
			var raw strings.Builder
			for i := 0; i < x.Segments.Len(); i++ {
				seg := x.Segments.At(i)
				raw.Write(seg.Value(c.src))
			}
			s := strings.ToLower(strings.TrimSpace(raw.String()))
			if s == "<br>" || s == "<br/>" || s == "<br />" {
				r.Text = "\n"
				break
			}
			continue
		case *east.FootnoteLink:
			r.Text = fmt.Sprintf("[%d]", x.Index)
		case *east.FootnoteBacklink:
			continue
		default:
			descend = true
		}
		if descend {
			rr, err := c.inlines(n, r)
			if err != nil {
				return nil, err
			}
			out = append(out, rr...)
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
