package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Document is a parsed page or book.
type Document struct {
	// Root is the goldmark document node.
	Root ast.Node
	// Source is the text the node segments point into.
	Source []byte
}

// New returns the configured goldmark instance. Its HTML renderer knows
// divs, spans, callouts and attributes (see html.go).
func New() goldmark.Markdown {
	blocks := parser.DefaultBlockParsers()
	for i, p := range blocks {
		if isFencedCode(p.Value) {
			blocks[i] = util.Prioritized(codeTracker{p.Value.(parser.BlockParser)}, p.Priority)
		}
	}
	blocks = append(blocks, util.Prioritized(divParser{}, 50))

	md := goldmark.New(
		goldmark.WithParser(parser.NewParser(
			parser.WithBlockParsers(blocks...),
			parser.WithInlineParsers(parser.DefaultInlineParsers()...),
			parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
		)),
		goldmark.WithExtensions(
			extension.Table,
			extension.Strikethrough,
			extension.Footnote,
			extension.DefinitionList,
		),
		goldmark.WithParserOptions(
			parser.WithASTTransformers(util.Prioritized(pandocTransformer{}, 100)),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			html.WithXHTML(),
		),
	)
	md.Renderer().AddOptions(htmlRenderers())
	return md
}

func isFencedCode(p any) bool {
	return fmt.Sprintf("%T", p) == "*parser.fencedCodeBlockParser"
}

// Parse parses Markdown source.
func Parse(src []byte) *Document {
	root := New().Parser().Parse(text.NewReader(src))
	return &Document{Root: root, Source: src}
}

// HTML renders a document as an HTML fragment.
func HTML(d *Document) (string, error) {
	var b bytes.Buffer
	if err := New().Renderer().Render(&b, d.Source, d.Root); err != nil {
		return "", err
	}
	return b.String(), nil
}

// AttrsOf returns the Pandoc attributes the parser attached to a node.
func AttrsOf(n ast.Node) Attrs {
	switch n := n.(type) {
	case *Div:
		return n.Attrs
	case *Span:
		return n.Attrs
	}
	if v, ok := n.Attribute(attrKey); ok {
		if a, ok := v.(Attrs); ok {
			return a
		}
	}
	return Attrs{}
}

func setAttrs(n ast.Node, a Attrs) { n.SetAttribute(attrKey, a) }

var attrKey = []byte("qm-attrs")

// PlainText is the text content of a node and its descendants.
func PlainText(n ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		switch t := n.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(src))
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
			return
		case *ast.String:
			b.Write(t.Value)
			return
		case *ast.CodeSpan:
			for c := t.FirstChild(); c != nil; c = c.NextSibling() {
				if tx, ok := c.(*ast.Text); ok {
					b.Write(tx.Segment.Value(src))
				}
			}
			return
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
