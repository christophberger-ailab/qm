// Package docrender lays out filtered Pandoc documents in Go and adds DOCX covers.
package docrender

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Node struct {
	T string          `json:"t"`
	C json.RawMessage `json:"c"`
}
type Document struct {
	Blocks []Node          `json:"blocks"`
	Meta   map[string]Node `json:"meta"`
}
type Options struct {
	Root, Cover, Title string
	TOC                bool
}
type Run struct {
	Text, Link, Image  string
	ImageWidth         string
	Bold, Italic, Code bool
}
type Block struct {
	Runs          []Run
	Level, Indent int
	ID            string
	Rows          [][]string
	Widths        []float64
	Break         bool
	Code          bool
}

func Read(r io.Reader) (*Document, error) {
	var d Document
	if err := json.NewDecoder(r).Decode(&d); err != nil {
		return nil, err
	}
	if d.Blocks == nil {
		return nil, fmt.Errorf("missing Pandoc blocks")
	}
	return &d, nil
}
func decode[T any](b json.RawMessage) T { var v T; _ = json.Unmarshal(b, &v); return v }
func parts(n Node) []json.RawMessage    { return decode[[]json.RawMessage](n.C) }
func text(runs []Run) string {
	var s strings.Builder
	for _, r := range runs {
		s.WriteString(r.Text)
	}
	return s.String()
}

func inlines(nodes []Node, style Run) ([]Run, error) {
	var out []Run
	for _, n := range nodes {
		r := style
		p := parts(n)
		var children []Node
		switch n.T {
		case "Str":
			r.Text = decode[string](n.C)
		case "Space", "SoftBreak":
			r.Text = " "
		case "LineBreak":
			r.Text = "\n"
		case "Code":
			r.Text = decode[string](p[1])
			r.Code = true
		case "Strong":
			r.Bold = true
			children = decode[[]Node](n.C)
		case "Emph":
			r.Italic = true
			children = decode[[]Node](n.C)
		case "Underline", "Strikeout", "SmallCaps", "Superscript", "Subscript":
			children = decode[[]Node](n.C)
		case "Span", "Cite":
			children = decode[[]Node](p[1])
		case "Quoted":
			children = decode[[]Node](p[1])
			children = append([]Node{{T: "Str", C: json.RawMessage(`"“"`)}}, children...)
			children = append(children, Node{T: "Str", C: json.RawMessage(`"”"`)})
		case "Link":
			children = decode[[]Node](p[1])
			r.Link = decode[[]string](p[2])[0]
		case "Image":
			r.Image = decode[[]string](p[2])[0]
			attrs := decode[[]json.RawMessage](p[0])
			for _, pair := range decode[[][]string](attrs[2]) {
				if len(pair) == 2 && pair[0] == "width" {
					r.ImageWidth = pair[1]
				}
			}
		case "RawInline":
			if decode[string](p[0]) == "html" {
				raw := strings.ToLower(strings.TrimSpace(decode[string](p[1])))
				if raw == "<br/>" || raw == "<br />" || raw == "<br>" {
					r.Text = "\n"
					break
				}
			}
			if decode[string](p[0]) == "openxml" && layoutOnlyXML(decode[string](p[1])) {
				continue
			}
			if decode[string](p[0]) == "html" && strings.HasPrefix(strings.TrimSpace(decode[string](p[1])), "<!--") {
				continue
			}
			return nil, fmt.Errorf("unsupported raw inline %s: %.500s", decode[string](p[0]), decode[string](p[1]))
		default:
			return nil, fmt.Errorf("unsupported Pandoc inline %s", n.T)
		}
		if children != nil {
			rr, err := inlines(children, r)
			if err != nil {
				return nil, err
			}
			out = append(out, rr...)
		} else {
			out = append(out, r)
		}
	}
	return out, nil
}

func blocks(nodes []Node, indent int) ([]Block, error) {
	var out []Block
	for _, n := range nodes {
		b := Block{Indent: indent}
		p := parts(n)
		var children []Node
		var inline []Node
		switch n.T {
		case "Para", "Plain":
			inline = decode[[]Node](n.C)
		case "Header":
			b.Level = decode[int](p[0])
			attr := decode[[]json.RawMessage](p[1])
			b.ID = decode[string](attr[0])
			inline = decode[[]Node](p[2])
		case "Div":
			children = decode[[]Node](p[1])
		case "BlockQuote":
			children = decode[[]Node](n.C)
			b.Indent++
		case "Figure":
			children = decode[[]Node](p[2])
			cap := decode[[]json.RawMessage](p[1])
			children = append(children, decode[[]Node](cap[1])...)
		case "BulletList", "OrderedList":
			items := decode[[][]Node](n.C)
			start := 1
			if n.T == "OrderedList" {
				items = decode[[][]Node](p[1])
				attrs := decode[[]json.RawMessage](p[0])
				start = decode[int](attrs[0])
			}
			for i, item := range items {
				bb, err := blocks(item, indent+1)
				if err != nil {
					return nil, err
				}
				if len(bb) > 0 {
					prefix := "• "
					if n.T == "OrderedList" {
						prefix = fmt.Sprintf("%d. ", start+i)
					}
					bb[0].Runs = append([]Run{{Text: prefix}}, bb[0].Runs...)
				}
				out = append(out, bb...)
			}
			continue
		case "DefinitionList":
			for _, entry := range p {
				pair := decode[[]json.RawMessage](entry)
				rr, err := inlines(decode[[]Node](pair[0]), Run{Bold: true})
				if err != nil {
					return nil, err
				}
				out = append(out, Block{Runs: rr, Indent: indent})
				for _, def := range decode[[][]Node](pair[1]) {
					bb, err := blocks(def, indent+1)
					if err != nil {
						return nil, err
					}
					out = append(out, bb...)
				}
			}
			continue
		case "CodeBlock":
			b.Code = true
			b.Runs = []Run{{Text: decode[string](p[1]), Code: true}}
		case "HorizontalRule":
			b.Runs = []Run{{Text: "────────────────────"}}
		case "RawBlock":
			format, raw := decode[string](p[0]), strings.TrimSpace(decode[string](p[1]))
			if format == "html" && strings.HasPrefix(raw, "<!--") {
				continue
			}
			if format == "openxml" && strings.Contains(raw, `w:type="page"`) {
				b.Break = true
			} else if format == "openxml" && layoutOnlyXML(raw) {
				// Quarto surrounds callout title/body blocks with raw DOCX table
				// fragments. Their actual content remains normal Pandoc blocks.
				continue
			} else {
				return nil, fmt.Errorf("unsupported raw block %s: %.250s", format, raw)
			}
		case "Table":
			cols := decode[[][]json.RawMessage](p[2])
			for _, col := range cols {
				v := decode[Node](col[1])
				b.Widths = append(b.Widths, decode[float64](v.C))
			}
			var rows []json.RawMessage
			head := decode[[]json.RawMessage](p[3])
			rows = append(rows, decode[[]json.RawMessage](head[1])...)
			for _, body := range decode[[][]json.RawMessage](p[4]) {
				rows = append(rows, decode[[]json.RawMessage](body[2])...)
				rows = append(rows, decode[[]json.RawMessage](body[3])...)
			}
			foot := decode[[]json.RawMessage](p[5])
			rows = append(rows, decode[[]json.RawMessage](foot[1])...)
			for _, row := range rows {
				fields := decode[[]json.RawMessage](row)
				var cells []string
				for _, cell := range decode[[][]json.RawMessage](fields[1]) {
					if decode[int](cell[2]) != 1 || decode[int](cell[3]) != 1 {
						return nil, fmt.Errorf("PDF tables with merged cells are not supported")
					}
					bb, err := blocks(decode[[]Node](cell[4]), 0)
					if err != nil {
						return nil, err
					}
					var lines []string
					for _, cb := range bb {
						for _, r := range cb.Runs {
							if r.Image != "" {
								return nil, fmt.Errorf("PDF images inside tables are not supported")
							}
						}
						lines = append(lines, text(cb.Runs))
					}
					cells = append(cells, strings.Join(lines, "\n"))
				}
				b.Rows = append(b.Rows, cells)
			}
		default:
			return nil, fmt.Errorf("unsupported Pandoc block %s", n.T)
		}
		if children != nil {
			bb, err := blocks(children, b.Indent)
			if err != nil {
				return nil, err
			}
			out = append(out, bb...)
			continue
		}
		if inline != nil {
			rr, err := inlines(inline, Run{})
			if err != nil {
				return nil, err
			}
			b.Runs = rr
		}
		out = append(out, b)
	}
	return out, nil
}

func layoutOnlyXML(raw string) bool {
	// Fragments can open/close a table across intervening Pandoc blocks, so
	// permit unbalanced tags, but only a closed list of layout elements.
	d := xml.NewDecoder(strings.NewReader(raw))
	d.Strict = false
	allowed := map[string]bool{}
	for _, name := range strings.Fields("tbl tblPr tblStyle tblLook tblBorders top left bottom right tblCellMar tr trPr cantSplit tc tcPr shd tcMar p spacing pPr rPr sz color b i jc textAlignment") {
		allowed[name] = true
	}
	for {
		token, err := d.RawToken()
		if err == io.EOF {
			return true
		}
		if err != nil {
			return false
		}
		switch t := token.(type) {
		case xml.StartElement:
			if !allowed[t.Name.Local] {
				return false
			}
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return false
			}
		}
	}
}

func imageSize(path string) (image.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Config{}, err
	}
	defer f.Close()
	c, _, err := image.DecodeConfig(f)
	return c, err
}
func ValidateCover(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return fmt.Errorf("cover must be a PNG: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	c, format, err := image.DecodeConfig(f)
	if err != nil {
		return fmt.Errorf("cover %s: %w", path, err)
	}
	if format != "png" || c.Width <= 0 || c.Height <= 0 {
		return fmt.Errorf("invalid PNG cover: %s", path)
	}
	return nil
}
func resource(root, src string) (string, error) {
	u, err := url.Parse(src)
	if err != nil {
		return "", err
	}
	if u.Scheme != "" {
		return "", fmt.Errorf("PDF requires local image, got %s", src)
	}
	p, err := url.PathUnescape(u.Path)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(p) {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	// Quarto project-absolute resources use a leading slash.
	return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, "/"))), nil
}
