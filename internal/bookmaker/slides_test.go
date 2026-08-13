package bookmaker

import (
	"strings"
	"testing"
)

func TestIsSlideFence(t *testing.T) {
	tests := []struct {
		attrs string
		want  bool
	}{
		{"slide", true},
		{"Slide", true},
		{"{.slide}", true},
		{"{.slide #folie-1}", true},
		{"{#folie-1 .slide}", true},
		{"{.SLIDE}", true},
		// A class that merely starts the same way is a different class:
		// `.slide-notes` rides along inside a slide.
		{"slide-notes", false},
		{"{.slide-notes}", false},
		{"explanation", false},
		{"{.callout-note}", false},
		{`{.content-visible when-profile="slides"}`, false},
		{"", false},
	}
	for _, tc := range tests {
		if got := isSlideFence(tc.attrs); got != tc.want {
			t.Errorf("isSlideFence(%q) = %v, want %v", tc.attrs, got, tc.want)
		}
	}
}

// splitDoc runs splitSlides over a document written as one string and hands
// the two halves back the same way.
func splitDoc(t *testing.T, doc string) (rest, slides string, count int) {
	t.Helper()
	restLines, slideLines, count := splitSlides(strings.Split(doc, "\n"))
	return strings.Join(restLines, "\n"), strings.Join(slideLines, "\n"), count
}

func TestSplitSlides(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantRest   string
		wantSlides string
		wantCount  int
	}{
		{
			name:       "the fences go, the content stays",
			in:         "::: slide\n## Folie\n\n- Punkt\n:::\n\n::: explanation\nText.\n:::",
			wantRest:   "::: explanation\nText.\n:::",
			wantSlides: "## Folie\n\n- Punkt",
			wantCount:  1,
		},
		{
			name:       "several slides are concatenated in order",
			in:         "::: slide\nEins\n:::\n\n::: explanation\nText.\n:::\n\n::: slide\nZwei\n:::",
			wantRest:   "::: explanation\nText.\n:::",
			wantSlides: "Eins\n\nZwei",
			wantCount:  2,
		},
		{
			name:       "a div inside a slide travels with it",
			in:         "::: slide\n## Folie\n\n:::: {.notes}\nNotiz\n::::\n:::",
			wantRest:   "",
			wantSlides: "## Folie\n\n:::: {.notes}\nNotiz\n::::",
			wantCount:  1,
		},
		{
			// Nothing inside a code block is markup, so a `::: slide`
			// shown as an example must not be acted on.
			name:       "fences inside code are text",
			in:         "```markdown\n::: slide\nBeispiel\n:::\n```",
			wantRest:   "```markdown\n::: slide\nBeispiel\n:::\n```",
			wantSlides: "",
			wantCount:  0,
		},
		{
			// Lifting the slide out leaves the blank line that separated
			// it from the text above; Pandoc ignores it.
			name:       "a slide keeps the divs it was nested in",
			in:         "::: pol\nText.\n\n:::: slide\nNur Polizei\n::::\n:::",
			wantRest:   "::: pol\nText.\n\n:::",
			wantSlides: "::: pol\nNur Polizei\n:::",
			wantCount:  1,
		},
		{
			name:       "the explicit spelling is a slide too",
			in:         "::: {.slide}\nFolie\n:::\n\nText.",
			wantRest:   "Text.",
			wantSlides: "Folie",
			wantCount:  1,
		},
		{
			// `.slide-notes` is a different class and stays put.
			name:       "a look-alike class is left alone",
			in:         ":::: {.slide-notes}\nNotiz\n::::",
			wantRest:   ":::: {.slide-notes}\nNotiz\n::::",
			wantSlides: "",
			wantCount:  0,
		},
		{
			name:       "an empty slide contributes nothing",
			in:         "::: slide\n:::\n\nText.",
			wantRest:   "Text.",
			wantSlides: "",
			wantCount:  0,
		},
		{
			name:       "a page without slides is handed back unchanged",
			in:         "::: explanation\n# A\n\n\nText.\n:::",
			wantRest:   "::: explanation\n# A\n\n\nText.\n:::",
			wantSlides: "",
			wantCount:  0,
		},
		{
			// balanceFences closes what a page left open before the
			// split ever sees it; content must not go missing if it
			// somehow does not.
			name:       "an unclosed slide is still extracted",
			in:         "Text.\n\n::: slide\nFolie",
			wantRest:   "Text.",
			wantSlides: "Folie",
			wantCount:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rest, slides, count := splitDoc(t, tc.in)
			if rest != tc.wantRest {
				t.Errorf("rest:\n%s\nwant:\n%s", rest, tc.wantRest)
			}
			if slides != tc.wantSlides {
				t.Errorf("slides:\n%s\nwant:\n%s", slides, tc.wantSlides)
			}
			if count != tc.wantCount {
				t.Errorf("count = %d, want %d", count, tc.wantCount)
			}
		})
	}
}

// TestSplitSlidesClosesTheGap checks that lifting a slide out does not leave
// the blank lines that used to surround it stacked on top of each other.
func TestSplitSlidesClosesTheGap(t *testing.T) {
	rest, _, _ := splitDoc(t, "::: explanation\nA\n:::\n\n::: slide\nFolie\n:::\n\n::: explanation\nB\n:::")
	want := "::: explanation\nA\n:::\n\n::: explanation\nB\n:::"
	if rest != want {
		t.Errorf("rest:\n%s\nwant:\n%s", rest, want)
	}
}

func TestSqueezeBlankLines(t *testing.T) {
	// Blank lines between blocks collapse to one, but blank lines inside a
	// fenced code block are part of the code.
	in := []string{"A", "", "", "B", "```", "", "", "x", "```", "", "", "C"}
	want := []string{"A", "", "B", "```", "", "", "x", "```", "", "C"}
	got := squeezeBlankLines(in)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
}
