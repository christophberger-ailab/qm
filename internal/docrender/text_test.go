package docrender

import (
	"strings"
	"testing"
)

// lines lays out runs as one paragraph of the given width and returns the
// text of each line.
func lines(t *testing.T, runs []Run, width float64) []string {
	t.Helper()
	w := testWriter(t)
	var out []string
	w.trace = func(line []piece) {
		var s strings.Builder
		for _, p := range line {
			s.WriteString(p.Text)
		}
		out = append(out, strings.TrimSpace(s.String()))
	}
	if err := w.paragraph(runs, 11, margin, width); err != nil {
		t.Fatal(err)
	}
	return out
}

func width(t *testing.T, s string) float64 {
	t.Helper()
	cw, err := testWriter(t).measure(Run{Text: s}, 11)
	if err != nil {
		t.Fatal(err)
	}
	return cw
}

func TestQuoteMarksStayWithTheirWord(t *testing.T) {
	// Pandoc's Quoted becomes separate runs for the marks.
	runs := []Run{{Text: "a longer lead-in "}, {Text: "“"}, {Text: "word"}, {Text: "”"}, {Text: " end"}}
	for _, fits := range []string{"a longer lead-in “", "a longer lead-in “word"} {
		got := lines(t, runs, width(t, fits)+0.1)
		if len(got) != 2 || got[0] != "a longer lead-in" || got[1] != "“word” end" {
			t.Errorf("width of %q: lines %q, want [a longer lead-in “word” end]", fits, got)
		}
	}
}

func TestEmphasisTouchingTextIsOneWord(t *testing.T) {
	runs := []Run{{Text: "see "}, {Text: "bold", Bold: true}, {Text: "."}}
	got := lines(t, runs, width(t, "see bold")+0.1)
	if len(got) != 2 || got[1] != "bold." {
		t.Errorf("lines %q, want [see bold.]", got)
	}
}

func TestEmojiUseEmojiFont(t *testing.T) {
	segs, err := segments("ok 👍🏽 ❤️ 👨‍👩‍👧", "regular")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range segs {
		got = append(got, s.font+":"+s.text)
	}
	want := []string{"regular:ok ", "emoji:👍", "regular: ", "emoji:❤", "regular: ", "emoji:👨👩👧"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("segments %q, want %q", got, want)
	}
	// Measured with the emoji font, an emoji is wider than the space gopdf
	// would substitute for a missing glyph.
	if e, s := width(t, "👍"), width(t, " "); e <= 2*s {
		t.Errorf("emoji width %v, space %v", e, s)
	}
}

func TestSingleQuotes(t *testing.T) {
	q := func(kind string) string {
		runs, err := inlines([]Node{{T: "Quoted", C: []byte(`[{"t":"` + kind + `"},[{"t":"Str","c":"x"}]]`)}}, Run{})
		if err != nil {
			t.Fatal(err)
		}
		return text(runs)
	}
	if got := q("SingleQuote"); got != "‘x’" {
		t.Errorf("single: %q", got)
	}
	if got := q("DoubleQuote"); got != "“x”" {
		t.Errorf("double: %q", got)
	}
}
