package markdown

import (
	"html"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

func htmlRenderers() renderer.Option {
	return renderer.WithNodeRenderers(util.Prioritized(htmlRenderer{}, 100))
}

type htmlRenderer struct{}

func (htmlRenderer) RegisterFuncs(r renderer.NodeRendererFuncRegisterer) {
	r.Register(KindDiv, renderDiv)
	r.Register(KindSpan, renderSpan)
	r.Register(ast.KindFencedCodeBlock, renderFencedCode)
}

func writeAttrs(w util.BufWriter, a Attrs, extraClasses ...string) {
	if a.ID != "" {
		w.WriteString(` id="` + html.EscapeString(a.ID) + `"`)
	}
	classes := append(append([]string(nil), extraClasses...), a.Classes...)
	if len(classes) > 0 {
		w.WriteString(` class="` + html.EscapeString(strings.Join(classes, " ")) + `"`)
	}
	for _, kv := range a.KV {
		k := kv[0]
		if k == "title" || k == "collapse" || k == "appearance" || k == "icon" {
			continue
		}
		if !strings.HasPrefix(k, "data-") && k != "style" && k != "lang" && k != "dir" {
			k = "data-" + k
		}
		w.WriteString(" " + html.EscapeString(k) + `="` + html.EscapeString(kv[1]) + `"`)
	}
}

func renderDiv(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	d := n.(*Div)
	if IsPageBreak(d) {
		if entering {
			w.WriteString("<div class=\"page-break\"></div>\n")
		}
		return ast.WalkSkipChildren, nil
	}
	if c, ok := CalloutOf(d); ok {
		if entering {
			w.WriteString(`<div`)
			writeAttrs(w, Attrs{ID: d.Attrs.ID}, "callout", "callout-"+c.Type)
			w.WriteString(">\n")
			if c.Title != "" {
				w.WriteString(`<div class="callout-title">` + html.EscapeString(c.Title) + "</div>\n")
			}
			w.WriteString(`<div class="callout-body">` + "\n")
		} else {
			w.WriteString("</div>\n</div>\n")
		}
		return ast.WalkContinue, nil
	}
	if entering {
		w.WriteString("<div")
		writeAttrs(w, d.Attrs)
		w.WriteString(">\n")
	} else {
		w.WriteString("</div>\n")
	}
	return ast.WalkContinue, nil
}

func renderSpan(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	s := n.(*Span)
	if entering {
		w.WriteString("<span")
		writeAttrs(w, s.Attrs)
		w.WriteString(">")
	} else {
		w.WriteString("</span>")
	}
	return ast.WalkContinue, nil
}

func renderFencedCode(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	cb := n.(*ast.FencedCodeBlock)
	info := CodeInfo(cb, src)
	body := codeLines(cb, src)
	if info.Raw != "" {
		if info.Raw == "html" {
			w.WriteString(body)
		}
		return ast.WalkSkipChildren, nil
	}
	w.WriteString("<pre")
	if info.Lang != "" {
		w.WriteString(` class="sourceCode ` + html.EscapeString(info.Lang) + `"`)
	}
	w.WriteString("><code")
	if info.Lang != "" {
		w.WriteString(` class="language-` + html.EscapeString(info.Lang) + `"`)
	}
	w.WriteString(">")
	w.WriteString(html.EscapeString(body))
	w.WriteString("</code></pre>\n")
	return ast.WalkSkipChildren, nil
}

func codeLines(cb ast.Node, src []byte) string {
	var b strings.Builder
	lines := cb.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(src))
	}
	return b.String()
}

// Code returns the text of a code block.
func Code(cb ast.Node, src []byte) string { return codeLines(cb, src) }

// CodeBlockInfo is what a fenced code block's info string says.
type CodeBlockInfo struct {
	// Lang is the language, from ```go, ```{python} or ```{.python}.
	Lang string
	// Raw is the target format of a raw block, ```{=html}.
	Raw   string
	Attrs Attrs
}

// CodeInfo reads a fenced code block's info string.
func CodeInfo(cb *ast.FencedCodeBlock, src []byte) CodeBlockInfo {
	var info CodeBlockInfo
	if cb.Info == nil {
		return info
	}
	s := strings.TrimSpace(string(cb.Info.Segment.Value(src)))
	if s == "" {
		return info
	}
	if strings.HasPrefix(s, "{") {
		if a, ok := ParseAttrs(s); ok {
			info.Attrs = a
			for _, c := range a.Classes {
				if strings.HasPrefix(c, "=") {
					info.Raw = c[1:]
					return info
				}
			}
			if len(a.Classes) > 0 {
				info.Lang = a.Classes[0]
			}
			return info
		}
		// ```{python} and ```{python echo=false}
		inner := strings.Fields(strings.Trim(s, "{}"))
		if len(inner) > 0 {
			info.Lang = inner[0]
		}
		return info
	}
	info.Lang = strings.Fields(s)[0]
	return info
}
