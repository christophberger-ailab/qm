package docrender

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Quarto 1.8's DOCX callout markup (src/resources/filters/quarto-post/docx.lua).
const (
	defaultPrefix = `<w:tbl>
<w:tblPr>
  <w:tblStyle w:val="Table" />
  <w:tblLook w:firstRow="0" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="0" w:val="0000" />
  <w:tblBorders>
    <w:top w:val="single" w:sz="4" w:space="0" w:color="0758E5"/>
    <w:left w:val="single" w:sz="24" w:space="0" w:color="0758E5"/>
    <w:bottom w:val="single" w:sz="4" w:space="0" w:color="0758E5"/>
    <w:right w:val="single" w:sz="4" w:space="0" w:color="0758E5"/>
  </w:tblBorders>
  <w:tblCellMar><w:left w:w="144" w:type="dxa" /><w:right w:w="144" w:type="dxa" /></w:tblCellMar>
</w:tblPr>
<w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc><w:tcPr>
  <w:shd w:color="auto" w:fill="dae6fb" w:val="clear"/>
  <w:tcMar><w:top w:w="92" w:type="dxa" /><w:bottom w:w="92" w:type="dxa" /></w:tcMar>
</w:tcPr>`
	defaultMiddle = `</w:tc></w:tr><w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc><w:tcPr><w:tcMar><w:top w:w="108" w:type="dxa" /><w:bottom w:w="108" w:type="dxa" /></w:tcMar></w:tcPr>`
	defaultSuffix = `<w:p/></w:tc></w:tr></w:tbl>`
	simplePrefix  = `<w:tbl>
<w:tblPr>
  <w:tblStyle w:val="Table" />
  <w:tblInd w:w="164" w:type="dxa" />
  <w:tblBorders><w:left w:val="single" w:sz="24" w:space="0" w:color="EB9113"/></w:tblBorders>
  <w:tblCellMar><w:left w:w="0" w:type="dxa" /><w:right w:w="0" w:type="dxa" /></w:tblCellMar>
</w:tblPr>
<w:tblGrid><w:gridCol w:w="7920" /></w:tblGrid>
<w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc>`
)

func node(t string, c any) Node {
	b, _ := json.Marshal(c)
	return Node{T: t, C: b}
}
func raw(s string) Node { return node("RawBlock", []string{"openxml", s}) }
func str(s string) Node { return node("Str", s) }
func paraOf(in ...Node) Node {
	return node("Para", in)
}
func div(class string, children ...Node) Node {
	return node("Div", []any{[]any{"", []string{class}, [][]string{}}, children})
}

func TestCalloutDefault(t *testing.T) {
	pPr := node("RawInline", []string{"openxml", `<w:pPr><w:spacing w:before="16" w:after="16"/></w:pPr>`})
	d := div("docx-callout",
		raw(defaultPrefix),
		paraOf(pPr, str("Note")),
		div("", raw(defaultMiddle)),
		paraOf(pPr, str("Body"), node("Space", nil), str("text.")),
		paraOf(str(strings.Repeat("More body text. ", 30))),
		raw(defaultSuffix))
	bb, err := blocks([]Node{d}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(bb) != 1 || bb[0].Callout == nil {
		t.Fatalf("callout not recognised: %+v", bb)
	}
	c := bb[0].Callout
	if c.Border != [3]uint8{0x07, 0x58, 0xe5} || !c.HasFill || c.Fill != [3]uint8{0xda, 0xe6, 0xfb} {
		t.Errorf("colors = %x %x %v", c.Border, c.Fill, c.HasFill)
	}
	if len(c.Title) != 1 || text(c.Title[0].Runs) != "Note" || !c.Title[0].Runs[0].Bold {
		t.Errorf("title = %+v", c.Title)
	}
	if len(c.Body) != 2 || text(c.Body[0].Runs) != "Body text." {
		t.Errorf("body = %+v", c.Body)
	}

	w := testWriter(t)
	var lefts []float64
	w.trace = func(line []piece) { lefts = append(lefts, w.y) }
	before := w.y
	if err = w.body(bb); err != nil {
		t.Fatal(err)
	}
	if len(lefts) < 3 {
		t.Fatalf("lines drawn = %d", len(lefts))
	}
	// Title row, then body inside the box; the text column is restored.
	if lefts[0] <= before || w.left != margin || w.right != pageW-margin || w.inset != 0 {
		t.Errorf("layout state after callout: y0=%v left=%v right=%v inset=%v", lefts[0], w.left, w.right, w.inset)
	}
}

func TestCalloutSimpleMergesIcon(t *testing.T) {
	icon := node("Image", []any{[]any{"", []string{}, [][]string{{"width", "16"}}}, []Node{}, []string{"warning.png", ""}})
	d := div("docx-callout",
		raw(simplePrefix),
		raw(`<w:tcPr><w:tcMar><w:left w:w="144" w:type="dxa" /><w:right w:w="144" w:type="dxa" /></w:tcMar></w:tcPr>`),
		paraOf(icon),
		raw("</w:tc>\n<w:tc>"),
		paraOf(node("Strong", []Node{str("Warning")})),
		paraOf(str("Body.")),
		raw(`</w:tc></w:tr></w:tbl>`))
	bb, err := blocks([]Node{d}, 0)
	if err != nil {
		t.Fatal(err)
	}
	c := bb[0].Callout
	if c.HasFill || c.Title != nil || c.Border != [3]uint8{0xeb, 0x91, 0x13} {
		t.Errorf("simple callout = %+v", c)
	}
	if len(c.Body) != 2 || c.Body[0].Runs[0].Image != "warning.png" || text(c.Body[0].Runs) != "  Warning" {
		t.Errorf("body = %+v", c.Body)
	}
}

// Writes sample callouts to QM_TEST_CALLOUT_PDF for visual inspection.
func TestCalloutPDF(t *testing.T) {
	pPr := node("RawInline", []string{"openxml", `<w:pPr><w:spacing w:before="16" w:after="16"/></w:pPr>`})
	var nodes []Node
	for _, k := range []struct{ title, color, fill string }{
		{"Note", "0758E5", "dae6fb"}, {"Tip", "00A047", "ccf1e3"}, {"Warning", "EB9113", "fcefdc"},
		{"Important", "CC1914", "f7dddc"}, {"Caution", "FC5300", "ffe5d0"},
	} {
		prefix := strings.NewReplacer("0758E5", k.color, "dae6fb", k.fill).Replace(defaultPrefix)
		nodes = append(nodes, paraOf(str("Text before the callout.")), div("docx-callout",
			raw(prefix), paraOf(pPr, str(k.title)), div("", raw(defaultMiddle)),
			paraOf(pPr, str(strings.Repeat(k.title+" body text. ", 12))), raw(defaultSuffix)))
	}
	nodes = append(nodes, div("docx-callout", raw(defaultPrefix), paraOf(str("Long note")), div("", raw(defaultMiddle)),
		paraOf(str(strings.Repeat("A long callout that runs over a page. ", 160))), raw(defaultSuffix)),
		paraOf(str("Text after the callout.")),
		div("docx-callout", raw(simplePrefix), paraOf(node("Strong", []Node{str("Simple warning")})), paraOf(str("Body.")), raw(`</w:tc></w:tr></w:tbl>`)))
	out := os.Getenv("QM_TEST_CALLOUT_PDF")
	if out == "" {
		out = filepath.Join(t.TempDir(), "callouts.pdf")
	}
	if err := PDF(out, &Document{Blocks: nodes}, Options{}); err != nil {
		t.Fatal(err)
	}
}
