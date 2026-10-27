package docrender

import (
	"testing"
)

// Quarto's {{< pagebreak >}} shortcode in DOCX output
// (src/resources/filters/quarto-pre/shortcodes-handlers.lua).
const quartoPagebreak = `<w:p><w:r><w:br w:type="page"/></w:r></w:p>`

func TestPagebreakStartsNewPage(t *testing.T) {
	bb, err := blocks([]Node{
		raw(quartoPagebreak), // at the top of a page: no blank page
		paraOf(str("First page.")),
		raw(quartoPagebreak),
		raw(quartoPagebreak), // a second break in a row: no blank page
		paraOf(str("Second page.")),
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	w := testWriter(t)
	var pages []int
	w.trace = func(line []piece) { pages = append(pages, w.page) }
	if err = w.body(bb); err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Errorf("pages of the two lines = %v, want [1 2]", pages)
	}
}

func TestPagebreakInsideCalloutAndList(t *testing.T) {
	item := []Node{raw(quartoPagebreak), paraOf(str("Item"))}
	bb, err := blocks([]Node{
		paraOf(str("Before.")),
		node("BulletList", [][]Node{item}),
		div("docx-callout", raw(defaultPrefix), paraOf(str("Note")), div("", raw(defaultMiddle)),
			paraOf(str("Body.")), raw(quartoPagebreak), paraOf(str("More body.")), raw(defaultSuffix)),
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(bb) != 4 || !bb[1].Break || text(bb[2].Runs) != "• Item" {
		t.Fatalf("list item with a leading page break = %+v", bb[1:3])
	}
	w := testWriter(t)
	var pages []int
	w.trace = func(line []piece) { pages = append(pages, w.page) }
	if err = w.body(bb); err != nil {
		t.Fatal(err)
	}
	// Before | Item, Note, Body. | More body.
	want := []int{1, 2, 2, 2, 3}
	if len(pages) != len(want) {
		t.Fatalf("pages of the lines = %v, want %v", pages, want)
	}
	for i := range want {
		if pages[i] != want[i] {
			t.Fatalf("pages of the lines = %v, want %v", pages, want)
		}
	}
}

func TestPagebreakInTableCellIgnored(t *testing.T) {
	bb, err := blocks([]Node{raw(quartoPagebreak), paraOf(str("Cell"))}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c := cellContent(bb); len(c) != 1 || c[0].Text != "Cell" {
		t.Errorf("cell = %+v", c)
	}
}
