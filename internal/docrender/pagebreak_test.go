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

// An empty heading (`# ` in a chapter that only pulls in an include) is laid
// out like any other heading. At the foot of a page it opens a page of its
// own and the break after it another, which leaves the first of them blank.
// That blank page must still count: gopdf's SetPage counts pages with
// content, so every page after it would be addressed one too early.
func TestBlankPageKeepsPageCount(t *testing.T) {
	bb, err := blocks([]Node{
		node("Header", []any{1, []any{"", []string{}, [][]string{}}, []Node{}}),
		raw(quartoPagebreak),
		paraOf(str("Next chapter.")),
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	w := testWriter(t)
	w.y = w.bottom() - 5
	var pages []int
	w.trace = func(line []piece) { pages = append(pages, w.page) }
	if err = w.body(bb); err != nil {
		t.Fatal(err)
	}
	// Page 1: the foot. Page 2: the heading, nothing drawn. Page 3: text.
	if len(pages) != 1 || pages[0] != 3 {
		t.Errorf("pages of the lines = %v, want [3]", pages)
	}
	assertPagesAddressable(t, w)
}

// Page numbers, contents entries and callout boxes address pages with
// SetPage, which must find a page even if nothing was drawn on it.
func TestEveryPageCanBeAddressed(t *testing.T) {
	w := testWriter(t)
	w.newPage()
	w.newPage()
	w.newPage()
	assertPagesAddressable(t, w)
}

// assertPagesAddressable checks that gopdf knows exactly the w.page pages
// that the writer has opened.
func assertPagesAddressable(t *testing.T, w *pdfWriter) {
	t.Helper()
	for page := 1; page <= w.page; page++ {
		if err := w.p.SetPage(page); err != nil {
			t.Errorf("SetPage(%d) of %d pages: %v", page, w.page, err)
		}
	}
	if err := w.p.SetPage(w.page + 1); err == nil {
		t.Errorf("SetPage(%d) found a page beyond the %d that were opened", w.page+1, w.page)
	}
}
