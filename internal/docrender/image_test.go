package docrender

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// imageLines lays out runs as one paragraph and returns each line, with
// inline images shown as [WxH], and the height the paragraph took.
func imageLines(t *testing.T, runs []Run, width float64) ([]string, float64) {
	t.Helper()
	w := testWriter(t)
	var out []string
	w.trace = func(line []piece) {
		var s strings.Builder
		for _, p := range line {
			if p.Image != "" {
				fmt.Fprintf(&s, "[%gx%g]", p.width, p.height)
				continue
			}
			s.WriteString(p.Text)
		}
		out = append(out, strings.TrimSpace(s.String()))
	}
	start := w.y
	if err := w.paragraph(runs, 11, margin, width); err != nil {
		t.Fatal(err)
	}
	return out, w.y - start
}

func TestInlineImageFlowsWithText(t *testing.T) {
	img := coverPNG(t) // 200x100
	runs := []Run{{Text: "Press "}, {Image: img, ImageHeight: "12pt"}, {Text: " to save."}}
	got, h := imageLines(t, runs, pageW-2*margin)
	if len(got) != 1 || got[0] != "Press [24x12] to save." {
		t.Fatalf("lines %q", got)
	}
	// 12pt is above the text's ascent, so the line grows by the difference.
	if want := 12 + 11*1.35 - 11*testWriter(t).fonts["regular"].ascent + 7; math.Abs(h-want) > 1e-9 {
		t.Errorf("height %v, want %v", h, want)
	}
}

func TestSmallInlineImageKeepsLineHeight(t *testing.T) {
	runs := []Run{{Text: "a "}, {Image: coverPNG(t), ImageWidth: "8px"}, {Text: " b"}}
	got, h := imageLines(t, runs, pageW-2*margin)
	if len(got) != 1 || got[0] != "a [6x3] b" {
		t.Fatalf("lines %q", got)
	}
	if want := 11*1.35 + 7; math.Abs(h-want) > 1e-9 {
		t.Errorf("height %v, want %v", h, want)
	}
}

func TestInlineImageWrapsLikeAWord(t *testing.T) {
	img := coverPNG(t)
	runs := []Run{{Text: "lead "}, {Image: img, ImageWidth: "40pt"}, {Text: "."}}
	got, _ := imageLines(t, runs, width(t, "lead ")+20)
	// The image and the period touching it move to the next line together.
	if len(got) != 2 || got[0] != "lead" || got[1] != "[40x20]." {
		t.Fatalf("lines %q", got)
	}
}

func TestSoleImageStaysABlock(t *testing.T) {
	w := testWriter(t)
	var traced int
	w.trace = func([]piece) { traced++ }
	start := w.y
	if err := w.paragraph([]Run{{Image: coverPNG(t), ImageHeight: "50pt"}}, 11, margin, pageW-2*margin); err != nil {
		t.Fatal(err)
	}
	if traced != 0 {
		t.Errorf("sole image laid out as a text line")
	}
	if want := 50.0 + 10 + 7; math.Abs(w.y-start-want) > 1e-9 {
		t.Errorf("height %v, want %v", w.y-start, want)
	}
}

func TestFitImageDimensions(t *testing.T) {
	w := testWriter(t)
	img := coverPNG(t) // 200x100
	for _, c := range []struct {
		width, height string
		w, h          float64
	}{
		{"", "", 100, 50}, // 0.75pt per pixel, shrunk to the width
		{"100pt", "", 100, 50},
		{"", "30pt", 60, 30},
		{"100pt", "30pt", 100, 30},
		{"80", "", 60, 30},
		{"50%", "", 50, 25},
		{"", "2em", 22, 11},
		{"400pt", "", 100, 50}, // shrinks to fit, keeping its proportions
	} {
		_, iw, ih, err := w.fitImage(Run{Image: img, ImageWidth: c.width, ImageHeight: c.height}, 5.5, 100)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(iw-c.w) > 1e-9 || math.Abs(ih-c.h) > 1e-9 {
			t.Errorf("width %q height %q: %gx%g, want %gx%g", c.width, c.height, iw, ih, c.w, c.h)
		}
	}
}

func TestImageHeightAttribute(t *testing.T) {
	runs, err := inlines([]Node{{T: "Image", C: []byte(`[["",[],[["width","2cm"],["height","1cm"]]],[],["a.png",""]]`)}}, Run{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ImageWidth != "2cm" || runs[0].ImageHeight != "1cm" {
		t.Fatalf("runs %+v", runs)
	}
}
