package bookmaker

import (
	"reflect"
	"testing"
)

func TestOpensDiv(t *testing.T) {
	tests := []struct {
		attrs string
		opens bool
	}{
		{attrs: "", opens: false},
		{attrs: "   ", opens: false},
		{attrs: "slide", opens: true},
		{attrs: "{.slide}", opens: true},
		{attrs: `{.content-visible when-profile="dispatcher-fw"}`, opens: true},
		{attrs: `{.callout-warning title="Achtung"}`, opens: true},
	}

	for _, tc := range tests {
		t.Run(tc.attrs, func(t *testing.T) {
			if got := opensDiv(tc.attrs); got != tc.opens {
				t.Errorf("opensDiv(%q) = %v, want %v", tc.attrs, got, tc.opens)
			}
		})
	}
}

func TestCodeTracker(t *testing.T) {
	in := []string{
		"text",   // false
		"```sql", // true (opening)
		"::: not a div",
		"```",     // true (closing)
		"text",    // false
		"~~~",     // true
		"``` mid", // true, inside tildes
		"~~~",     // true (closing)
		"done",    // false
	}
	want := []bool{false, true, true, true, false, true, true, true, false}

	var tracker codeTracker
	for i, line := range in {
		if got := tracker.step(line); got != want[i] {
			t.Errorf("line %d (%q): in code = %v, want %v", i, line, got, want[i])
		}
	}
}

func TestBalanceFences(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		repair fenceRepair
	}{
		{
			name: "balanced input is untouched",
			in:   "::: a\ntext\n:::",
			want: "::: a\ntext\n:::",
		},
		{
			name:   "closes an unterminated div",
			in:     ":::: explanation\ntext",
			want:   ":::: explanation\ntext\n::::",
			repair: fenceRepair{Divs: 1},
		},
		{
			name:   "closes an unterminated code fence",
			in:     "```sql\nSELECT 1",
			want:   "```sql\nSELECT 1\n```",
			repair: fenceRepair{Code: true},
		},
		{
			name:   "drops a stray closing fence",
			in:     "text\n:::\nmore",
			want:   "text\nmore",
			repair: fenceRepair{Stray: 1},
		},
		{
			name:   "closes code before divs",
			in:     "::: a\n```\ncode",
			want:   "::: a\n```\ncode\n```\n:::",
			repair: fenceRepair{Code: true, Divs: 1},
		},
		{
			// Pandoc reads a fence glued to the line above as more of that
			// paragraph, so the div never opens -- while the fence closing
			// it still closes something. The blank line restores the
			// author's structure and keeps the count honest.
			name:   "separates an opening fence from the text above",
			in:     "- a\n- \n:::: {.notes}\ntext\n::::",
			want:   "- a\n- \n\n:::: {.notes}\ntext\n::::",
			repair: fenceRepair{Glued: 1},
		},
		{
			name: "a fence after a heading or another fence is left alone",
			in:   "## H\n::: a\n:::\n::: b\n:::",
			want: "## H\n::: a\n:::\n::: b\n:::",
		},
		{
			name: "a closing fence may stay glued to its content",
			in:   "::: a\ntext\n:::",
			want: "::: a\ntext\n:::",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, repair := balanceFences(lines(tc.in))
			if want := lines(tc.want); !reflect.DeepEqual(got, want) {
				t.Errorf("got %q\nwant %q", got, want)
			}
			if repair != tc.repair {
				t.Errorf("repair = %+v, want %+v", repair, tc.repair)
			}
			if tc.repair.clean() != repair.clean() {
				t.Errorf("clean() mismatch")
			}
		})
	}
}

func TestFindHeadings(t *testing.T) {
	src := `
# One

::: explanation
## Two

` + "```" + `
### not a heading
` + "```" + `
:::

::: tutorial
## Three
:::

#### Four
#NotAHeading`

	got := findHeadings(lines(src))
	want := []struct {
		level int
		text  string
		block int
	}{
		{1, "One", 0},
		{2, "Two", 1},
		{2, "Three", 2},
		{4, "Four", 0},
	}

	if len(got) != len(want) {
		t.Fatalf("found %d headings, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Level != w.level || got[i].Text() != w.text || got[i].Block != w.block {
			t.Errorf("heading %d = {level %d, text %q, block %d}, want {%d, %q, %d}",
				i, got[i].Level, got[i].Text(), got[i].Block, w.level, w.text, w.block)
		}
	}
}

func TestHeadingText(t *testing.T) {
	tests := []struct{ rest, want string }{
		{" Title", "Title"},
		{" Title {#sec-x .unnumbered}", "Title"},
		{" Title ##", "Title"},
		// A `{...}` not preceded by `]` is a heading attribute block even
		// with no whitespace at all before it -- confirmed against Pandoc:
		// `## Title{#id}` renders as <h2 id="id">Title</h2>.
		{" Title{#sec-x}", "Title"},
		// A span glued to the end of the heading text is content, not an
		// attribute block: Pandoc resolves a `{...}` immediately preceded
		// by `]` as that span's own attributes, regardless of whitespace.
		{" [Die Wartenwand]{.fw}[Die Unit-Matrix]{.pol}", "[Die Wartenwand]{.fw}[Die Unit-Matrix]{.pol}"},
	}
	for _, tc := range tests {
		h := heading{Rest: tc.rest}
		if got := h.Text(); got != tc.want {
			t.Errorf("Text(%q) = %q, want %q", tc.rest, got, tc.want)
		}
	}
}

func TestRenderHeading(t *testing.T) {
	tests := []struct {
		name  string
		rest  string
		level int
		edit  titleEdit
		want  string
	}{
		{name: "plain", rest: " Title", level: 2, edit: titleEdit{ID: "bm-x"}, want: "## Title {#bm-x}"},
		{name: "no edit", rest: " Title", level: 3, want: "### Title"},
		{name: "clamped", rest: " Title", level: 9, want: "###### Title"},
		{
			name: "keeps an existing identifier", rest: " Title {#own}", level: 1, edit: titleEdit{ID: "bm-x"},
			want: "# Title {#own}",
		},
		{
			name: "merges into other attributes", rest: " Title {.unnumbered}", level: 1, edit: titleEdit{ID: "bm-x"},
			want: "# Title {#bm-x .unnumbered}",
		},
		{
			// Same merge, but with no whitespace before the brace -- must
			// still be recognised as the attribute block (not a span,
			// since it is not preceded by `]`) and merged into.
			name: "merges into a no-space attribute block", rest: " Title{.unnumbered}", level: 1, edit: titleEdit{ID: "bm-x"},
			want: "# Title{#bm-x .unnumbered}",
		},
		{
			name: "trailing span is not an attribute block", rest: " [A]{.fw}[B]{.pol}", level: 2, edit: titleEdit{ID: "bm-x"},
			want: "## [A]{.fw}[B]{.pol} {#bm-x}",
		},
		{
			// No whitespace before the brace, and not bracket-preceded:
			// still a real attribute block per Pandoc (`## Title{#own}`
			// renders with id="own"), so it must be merged into, not
			// duplicated after.
			name: "no-space attribute block is still recognised", rest: " Title{#own}", level: 1, edit: titleEdit{ID: "bm-x"},
			want: "# Title{#own}",
		},
		{
			name: "adds a class", rest: " Title", level: 1,
			edit: titleEdit{ID: "bm-x", Classes: []string{"unnumbered"}},
			want: "# Title {#bm-x .unnumbered}",
		},
		{
			name: "does not repeat a class the heading already has", rest: " Title {.unnumbered}", level: 1,
			edit: titleEdit{Classes: []string{"unnumbered"}},
			want: "# Title {.unnumbered}",
		},
		{
			name: "replaces the text", rest: " Title {#own .fw}", level: 1,
			edit: titleEdit{Text: "Anders", Classes: []string{"unnumbered"}},
			want: "# Anders {#own .fw .unnumbered}",
		},
		{
			name: "keeps quoted attribute values intact", rest: ` Title {.callout-note title="A B"}`, level: 2,
			edit: titleEdit{ID: "bm-x"},
			want: `## Title {#bm-x .callout-note title="A B"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := heading{Rest: tc.rest}
			if got := renderHeading(h, tc.level, tc.edit); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTitleEditEmpty(t *testing.T) {
	if !(titleEdit{}).empty() {
		t.Error("the zero titleEdit must be empty")
	}
	for _, e := range []titleEdit{{ID: "x"}, {Text: "x"}, {Classes: []string{"x"}}} {
		if e.empty() {
			t.Errorf("%+v reported as empty", e)
		}
	}
}

func TestMaxFenceWidth(t *testing.T) {
	if got := maxFenceWidth(lines("::: a\n::::: b\n:::::\n:::")); got != 5 {
		t.Errorf("maxFenceWidth = %d, want 5", got)
	}
	if got := maxFenceWidth(lines("no fences here")); got != 0 {
		t.Errorf("maxFenceWidth = %d, want 0", got)
	}
	if got := maxFenceWidth(lines("```\n::::::: x\n```")); got != 0 {
		t.Errorf("fences inside code must be ignored, got %d", got)
	}
}

func TestMinHeadingLevel(t *testing.T) {
	if got := minHeadingLevel(nil); got != 0 {
		t.Errorf("minHeadingLevel(nil) = %d, want 0", got)
	}
	hs := []heading{{Level: 3}, {Level: 2}, {Level: 4}}
	if got := minHeadingLevel(hs); got != 2 {
		t.Errorf("minHeadingLevel = %d, want 2", got)
	}
}

func TestSplitAttrTokens(t *testing.T) {
	got := splitAttrTokens(`.content-visible when-profile="a b" #id`)
	want := []string{".content-visible", `when-profile="a b"`, "#id"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
