package markdown

import (
	"strings"
	"testing"
)

func render(t *testing.T, src string, o Options) string {
	t.Helper()
	d := Parse([]byte(src))
	d.Prepare(o)
	out, err := HTML(d)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDivsNest(t *testing.T) {
	src := ":::: outer\n::: {.inner #x}\nText\n:::\n\nAfter\n::::\n\nOutside\n"
	got := render(t, src, Options{})
	want := "<div class=\"outer\">\n<div id=\"x\" class=\"inner\">\n<p>Text</p>\n</div>\n<p>After</p>\n</div>\n<p>Outside</p>\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestDivSameFenceWidthNests(t *testing.T) {
	src := "::: a\n::: b\nx\n:::\ny\n:::\nz\n"
	got := render(t, src, Options{})
	if !strings.Contains(got, "<div class=\"a\">\n<div class=\"b\">\n<p>x</p>\n</div>\n<p>y</p>\n</div>\n<p>z</p>") {
		t.Errorf("got\n%s", got)
	}
}

func TestFenceInCodeDoesNotCloseDiv(t *testing.T) {
	src := "::: a\n```\n:::\n```\nafter\n:::\n"
	got := render(t, src, Options{})
	if !strings.Contains(got, "<code>:::\n</code>") || !strings.Contains(got, "<p>after</p>\n</div>") {
		t.Errorf("got\n%s", got)
	}
}

func TestLazyFenceIsText(t *testing.T) {
	got := render(t, "para\n::: a\n", Options{})
	if strings.Contains(got, "<div") {
		t.Errorf("an opening fence under a paragraph must stay text: %s", got)
	}
}

func TestSpansAndAttrs(t *testing.T) {
	src := "Hello [a *b* c]{.fw} and ![x](y.png){width=50%} [plain] {x}\n\n## Head {#hid .c}\n\n## Second {-}\n"
	got := render(t, src, Options{})
	for _, want := range []string{
		`Hello <span class="fw">a <em>b</em> c</span> and`,
		`style="width:50%"`,
		`[plain] {x}`,
		`<h2 id="hid" class="c">Head</h2>`,
		`<h2 class="unnumbered" id="second">Second</h2>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestFilterAndIDs(t *testing.T) {
	src := "::: pol\n# Titel\n:::\n\n::: fw\n# Titel\n:::\n\nText [nur pol]{.pol}.\n"
	keep := func(classes []string) bool {
		for _, c := range classes {
			if c == "fw" {
				return false
			}
		}
		return true
	}
	got := render(t, src, Options{Keep: keep, Demote: 1})
	if strings.Contains(got, `class="fw"`) || !strings.Contains(got, `<span class="pol">nur pol</span>`) {
		t.Errorf("filter: %s", got)
	}
	if !strings.Contains(got, `<h2 id="titel">Titel</h2>`) || strings.Contains(got, "titel-1") {
		t.Errorf("ids after filtering: %s", got)
	}
}

func TestCallout(t *testing.T) {
	src := "::: {.callout-warning}\n## Achtung\n\nBody\n:::\n\n::: callout-tip\nTip body\n:::\n"
	got := render(t, src, Options{Lang: "de"})
	for _, want := range []string{
		`<div class="callout callout-warning">`, `<div class="callout-title">Achtung</div>`,
		`<div class="callout-title">Tipp</div>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestContentVisible(t *testing.T) {
	src := "::: {.content-visible when-format=\"html\"}\nweb\n:::\n\n::: {.content-hidden when-format=\"html\"}\ndoc\n:::\n"
	got := render(t, src, Options{Formats: []string{"html"}})
	if !strings.Contains(got, "web") || strings.Contains(got, "doc") {
		t.Errorf("got %s", got)
	}
}

func TestSlidesDropped(t *testing.T) {
	got := render(t, "::: slide\n# S\n:::\n\nbook\n", Options{})
	if strings.Contains(got, "S</h1>") {
		t.Errorf("slide content leaked: %s", got)
	}
}

func TestRawAndCode(t *testing.T) {
	got := render(t, "```{=html}\n<b>raw</b>\n```\n\n```{.python}\nx = 1\n```\n\n```{=openxml}\n<w:p/>\n```\n", Options{})
	if !strings.Contains(got, "<b>raw</b>") || !strings.Contains(got, `class="language-python"`) || strings.Contains(got, "w:p") {
		t.Errorf("got %s", got)
	}
}
