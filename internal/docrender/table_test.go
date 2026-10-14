package docrender

import (
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
		Rows: [][]string{
			{"Nr.", "Rolle", "Tätigkeit", "Systemreaktion / Ergebnis"},
			{"12", "Disponent", "Einsatz anlegen", "Einsatz wird angezeigt"},
		},
	}
	if err := within(t, 5*time.Second, func() error { return w.table(b) }); err != nil {
		t.Fatal(err)
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
