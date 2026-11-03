package markdown

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// Keep decides whether content carrying the given classes survives a
// render. Writers get it from the selection (config.Selection.Keep).
type Keep func(classes []string) bool

// alwaysDropped are classes no document or website output shows: the
// content of a slide deck and its speaker notes.
var alwaysDropped = map[string]bool{"slide": true, "notes": true}

// Filter removes every div and span that keep rejects, and unwraps the
// survivors that exist only to carry a condition. It runs before
// AssignIDs, so that only identifiers of surviving headings are counted.
func Filter(d *Document, keep Keep) { filter(d, keep, nil) }

func filter(d *Document, keep Keep, formats map[string]bool) {
	var drop []ast.Node
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var a Attrs
		switch x := n.(type) {
		case *Div:
			a = x.Attrs
		case *Span:
			a = x.Attrs
		default:
			return ast.WalkContinue, nil
		}
		for _, c := range a.Classes {
			if alwaysDropped[c] {
				drop = append(drop, n)
				return ast.WalkSkipChildren, nil
			}
		}
		if !quartoVisible(a, formats) || (keep != nil && !keep(a.Classes)) {
			drop = append(drop, n)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	for _, n := range drop {
		if p := n.Parent(); p != nil {
			p.RemoveChild(p, n)
		}
	}
}

// quartoVisible reads Quarto's `.content-visible` / `.content-hidden`
// conditions on the output format: `when-format="html"` and
// `unless-format="docx"`. Profile and metadata conditions are not
// supported; such content is kept.
func quartoVisible(a Attrs, formats map[string]bool) bool {
	visible, hidden := a.Has("content-visible"), a.Has("content-hidden")
	if !visible && !hidden {
		return true
	}
	cond := true
	if f, ok := a.Get("when-format"); ok {
		cond = matchFormat(f, formats)
	} else if f, ok := a.Get("unless-format"); ok {
		cond = !matchFormat(f, formats)
	}
	if visible {
		return cond
	}
	return !cond
}

func matchFormat(f string, formatTypes map[string]bool) bool {
	for _, x := range strings.FieldsFunc(f, func(r rune) bool { return r == ',' || r == ' ' }) {
		if formatTypes[strings.ToLower(x)] {
			return true
		}
	}
	return false
}

// Options say how Prepare readies a document for one output.
type Options struct {
	// Keep decides the conditional content; nil keeps everything.
	Keep Keep
	// Formats are the output's types and format name (`html`, `docx`,
	// `handout`), which Quarto's `when-format` conditions compare against.
	Formats []string
	// Lang is the document language, for default callout titles.
	Lang string
	// Demote is added to every heading level.
	Demote int
}

// Prepare readies a parsed document for a writer: conditional content
// removed, headings demoted, callout titles settled and every heading
// given an identifier.
func (d *Document) Prepare(o Options) {
	formats := map[string]bool{}
	for _, t := range o.Formats {
		t = strings.ToLower(t)
		formats[t] = true
		if t == "html" {
			formats["html:js"] = true
		}
	}
	filter(d, o.Keep, formats)
	Demote(d, o.Demote)
	settleCallouts(d, o.Lang)
	AssignIDs(d)
}

// settleCallouts gives every callout its title attribute: the one it
// declares, else its first heading (removed from the body), else the
// type's default title.
func settleCallouts(d *Document, lang string) {
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		div, ok := n.(*Div)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		c, ok := CalloutOf(div)
		if !ok {
			return ast.WalkContinue, nil
		}
		if _, has := div.Attrs.Get("title"); has {
			return ast.WalkContinue, nil
		}
		title := ""
		if h, ok := div.FirstChild().(*ast.Heading); ok {
			title = PlainText(h, d.Source)
			div.RemoveChild(div, h)
		}
		if title == "" && c.Type != "" {
			title = CalloutTitle(c.Type, lang)
		}
		if title != "" {
			div.Attrs.KV = append(div.Attrs.KV, [2]string{"title", title})
		}
		return ast.WalkContinue, nil
	})
}

// AssignIDs gives every heading an identifier: the one it declares, else
// one made from its text. Duplicates get -1, -2, … appended, as Pandoc
// does, so that the first heading keeps the identifier links point to.
func AssignIDs(d *Document) {
	seen := map[string]int{}
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		a := AttrsOf(h)
		id := a.ID
		if id == "" {
			id = Slug(PlainText(h, d.Source))
		}
		if k, dup := seen[id]; dup {
			seen[id] = k + 1
			id = fmt.Sprintf("%s-%d", id, k+1)
			for seen[id] > 0 {
				id += "-1"
			}
		}
		seen[id]++
		a.ID = id
		setAttrs(h, a)
		h.SetAttributeString("id", id)
		return ast.WalkSkipChildren, nil
	})
}

// Demote adds delta to every heading level, capped at 6.
func Demote(d *Document, delta int) {
	if delta == 0 {
		return
	}
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			h.Level = min(max(h.Level+delta, 1), 6)
		}
		return ast.WalkContinue, nil
	})
}

// HeadingID is the identifier AssignIDs gave a heading.
func HeadingID(h *ast.Heading) string { return AttrsOf(h).ID }

// IsPageBreak reports whether a div is the page break `{{< pagebreak >}}`
// turns into.
func IsPageBreak(d *Div) bool { return d.Attrs.Has("pagebreak") || d.Attrs.Has("qm-pagebreak") }

// Callout is a Quarto callout block.
type Callout struct {
	// Type is note, tip, warning, important or caution; "" for a plain
	// `.callout`.
	Type  string
	Title string
}

var calloutTypes = []string{"note", "tip", "warning", "important", "caution"}

// CalloutOf reports whether a div is a callout, and its title once
// Prepare has settled it.
func CalloutOf(d *Div) (Callout, bool) {
	var c Callout
	found := false
	for _, cl := range d.Attrs.Classes {
		if cl == "callout" {
			found = true
		}
		for _, t := range calloutTypes {
			if cl == "callout-"+t {
				c.Type, found = t, true
			}
		}
	}
	if !found {
		return c, false
	}
	c.Title, _ = d.Attrs.Get("title")
	return c, true
}

// CalloutTitle is a callout type's default title in the document language
// (German or English).
func CalloutTitle(typ, lang string) string {
	de := map[string]string{"note": "Hinweis", "tip": "Tipp", "warning": "Warnung", "important": "Wichtig", "caution": "Vorsicht"}
	en := map[string]string{"note": "Note", "tip": "Tip", "warning": "Warning", "important": "Important", "caution": "Caution"}
	if strings.HasPrefix(strings.ToLower(lang), "de") {
		return de[typ]
	}
	return en[typ]
}
