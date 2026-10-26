package docrender

import (
	"bytes"
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func testWriter(t *testing.T) *pdfWriter {
	t.Helper()
	p := &gopdf.GoPdf{}
	p.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageW, H: pageH}})
	for name, data := range map[string][]byte{"regular": goregular.TTF, "bold": gobold.TTF} {
		if err := p.AddTTFFontData(name, data); err != nil {
			t.Fatal(err)
		}
	}
	w := &pdfWriter{p: p}
	w.newPage()
	return w
}

// within fails the test instead of hanging the suite if f does not return.
func within(t *testing.T, d time.Duration, f func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		t.Fatalf("did not return within %s", d)
		return nil
	}
}

// Regression: the dispatcher handout has pipe tables whose "Nr." column
// gets ~2% of the width, i.e. a wrap width below one glyph. gopdf's word
// wrap then loops forever and the render maxes out every core.
func TestTableWithSubGlyphColumnTerminates(t *testing.T) {
	w := testWriter(t)
	b := Block{
		Widths: []float64{0.0225, 0.105, 0.451, 0.421},
		Rows: [][]Cell{
			textCells("Nr.", "Rolle", "Tätigkeit", "Systemreaktion / Ergebnis"),
			textCells("12", "Disponent", "Einsatz anlegen", "Einsatz wird angezeigt"),
		},
	}
	if err := within(t, 5*time.Second, func() error { return w.table(b) }); err != nil {
		t.Fatal(err)
	}
}

func textCells(ss ...string) []Cell {
	var cells []Cell
	for _, s := range ss {
		cells = append(cells, Cell{{Text: s}})
	}
	return cells
}

func TestCellContentKeepsImagesInOrder(t *testing.T) {
	got := cellContent([]Block{
		{Runs: []Run{{Text: "before "}, {Image: "a.png", ImageWidth: "50%"}, {Text: " after"}}},
		{Runs: []Run{{Image: "b.png"}}},
		{Runs: []Run{{Text: "last"}}},
	})
	want := Cell{{Text: "before"}, {Image: "a.png", ImageWidth: "50%"}, {Text: "after"}, {Image: "b.png"}, {Text: "last"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTableWithImage(t *testing.T) {
	w := testWriter(t)
	img := coverPNG(t) // 200x100
	b := Block{Rows: [][]Cell{
		textCells("Bild", "Beschreibung"),
		{{{Image: img}}, {{Text: "Ein Bild"}}},
	}}
	// Start low on the page: the image row must move to the next page whole.
	w.y = pageH - margin - 40
	if err := w.table(b); err != nil {
		t.Fatal(err)
	}
	if w.page != 2 {
		t.Fatalf("image row not moved to the next page, page %d", w.page)
	}
	// The image (0.75 × 100pt high) plus padding sits on the new page.
	if want := margin + 75 + 4 + 8 + 10; math.Abs(w.y-want) > 1e-9 {
		t.Fatalf("y = %v, want %v", w.y, want)
	}
	var out bytes.Buffer
	if _, err := w.p.WriteTo(&out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/Subtype /Image")) {
		t.Fatal("image not embedded")
	}
}

func TestTableImageScaledToColumn(t *testing.T) {
	w := testWriter(t)
	b := Block{Widths: []float64{1, 9}, Rows: [][]Cell{{{{Image: coverPNG(t)}}, {{Text: "x"}}}}}
	start := w.y
	if err := w.table(b); err != nil {
		t.Fatal(err)
	}
	// Column is 1/10 of the text width; the 2:1 image fits its inner width.
	col := (pageW-2*margin)/10 - 2*cellPadding
	if want := start + col/2 + 4 + 8 + 10; math.Abs(w.y-want) > 1e-9 {
		t.Fatalf("y = %v, want %v", w.y, want)
	}
}

func TestWrapNarrowerThanGlyph(t *testing.T) {
	w := testWriter(t)
	w.font(Run{Bold: true}, 9)
	var lines []string
	err := within(t, 5*time.Second, func() (err error) {
		lines, err = w.wrap("Nr. 1", 1.26)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(lines); got != 4 {
		t.Fatalf("want one line per character, got %q", lines)
	}
}

func TestEnforceMinimums(t *testing.T) {
	got := enforceMinimums([]float64{10, 50, 440}, []float64{20, 30, 30})
	if got[0] != 20 || got[1] < 30 || got[2] < 30 {
		t.Fatalf("minimums not honoured: %v", got)
	}
	if s := got[0] + got[1] + got[2]; math.Abs(s-500) > 1e-9 {
		t.Fatalf("total width changed: %v", s)
	}
	// Minimums exceeding the page are scaled, not overflowed.
	got = enforceMinimums([]float64{50, 50}, []float64{100, 300})
	if math.Abs(got[0]+got[1]-100) > 1e-9 || got[1] != 3*got[0] {
		t.Fatalf("unexpected scaling: %v", got)
	}
}

func TestRunPDFHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &Document{Blocks: []Node{{T: "Para", C: []byte(`[{"t":"Str","c":"x"}]`)}}}
	err := RunPDF(ctx, time.Minute, filepath.Join(t.TempDir(), "x.pdf"), d, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation error, got %v", err)
	}
}

func TestRunPDFTimesOut(t *testing.T) {
	// A deadline already in the past behaves like an expired timeout.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	d := &Document{Blocks: []Node{{T: "Para", C: []byte(`[{"t":"Str","c":"x"}]`)}}}
	err := RunPDF(ctx, time.Minute, filepath.Join(t.TempDir(), "x.pdf"), d, Options{})
	if !errors.Is(err, ErrPDFTimeout) {
		t.Fatalf("want ErrPDFTimeout, got %v", err)
	}
}
