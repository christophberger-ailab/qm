package web

import (
	"strings"
	"testing"
)

func TestSpanSymbols(t *testing.T) {
	tests := []struct {
		name string
		css  string
		want map[string]string
	}{
		{
			name: "the baked-in default stylesheet",
			css:  defaultCustomCSS,
			want: map[string]string{
				"slide":     "🖥️ SLIDE",
				"pol":       "🚔",
				"fw":        "🚒",
				"perle":     "[⛲PERLE]",
				"tutorial":  "🎓 TUTORIAL",
				"howto":     "🔨 HOWTO",
				"reference": "📃 REFERENCE",
			},
		},
		{
			name: "css3 spelling and single quotes",
			css:  ".quarto.pol::before { content: '🚔'; }",
			want: map[string]string{"pol": "🚔"},
		},
		{
			name: "other declarations in the same rule",
			css:  ".quarto.fw:before { font-weight: bold; content: \"🚒\"; margin: 0 }",
			want: map[string]string{"fw": "🚒"},
		},
		{
			name: "escaped quote in the content",
			css:  `.quarto.q:before { content: "\"" }`,
			want: map[string]string{"q": `"`},
		},
		{
			name: "a commented-out rule contributes nothing",
			css:  "/* .quarto.pol:before { content: \"🚔\" } */",
			want: map[string]string{},
		},
		{
			name: "a rule without content contributes nothing",
			css:  ".quarto.pol { background: lightblue }",
			want: map[string]string{},
		},
		{
			name: "empty content contributes nothing",
			css:  `.quarto.pol:before { content: "" }`,
			want: map[string]string{},
		},
		{
			name: "no stylesheet",
			css:  "",
			want: map[string]string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := spanSymbols(tc.css)
			if len(got) != len(tc.want) {
				t.Fatalf("spanSymbols() = %v, want %v", got, tc.want)
			}
			for class, sym := range tc.want {
				if got[class] != sym {
					t.Errorf("spanSymbols()[%q] = %q, want %q", class, got[class], sym)
				}
			}
		})
	}
}

func TestFlattenSpans(t *testing.T) {
	symbols := map[string]string{"pol": "🚔", "fw": "🚒"}
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "two spans and plain text",
			title: "[Polizei-]{.pol}[Feuerwehr-]{.fw}Spickzettel",
			want:  "🚔Polizei-🚒Feuerwehr-Spickzettel",
		},
		{
			name:  "a span in the middle of the title",
			title: "Der [Polizei]{.pol} Teil",
			want:  "Der 🚔Polizei Teil",
		},
		{
			name:  "shorthand class without the dot",
			title: "[Polizei]{pol}",
			want:  "🚔Polizei",
		},
		{
			name:  "id and key attributes carry no symbol",
			title: "[Polizei]{#id .pol lang=\"de\"}",
			want:  "🚔Polizei",
		},
		{
			name:  "a class with no symbol is left as written",
			title: "[Text]{.foo}",
			want:  "[Text]{.foo}",
		},
		{
			name:  "a title without spans is untouched",
			title: "PERLE OnCall-Schulungen",
			want:  "PERLE OnCall-Schulungen",
		},
		{
			name:  "a Markdown link is not a span",
			title: "See [the page](other.qmd)",
			want:  "See [the page](other.qmd)",
		},
		{
			name:  "empty span text keeps only the symbol",
			title: "[]{.pol}Spickzettel",
			want:  "🚔Spickzettel",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := flattenSpans(tc.title, symbols); got != tc.want {
				t.Errorf("flattenSpans() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFlattenSpansWithoutSymbols(t *testing.T) {
	const title = "[Polizei-]{.pol}Spickzettel"
	if got := flattenSpans(title, nil); got != title {
		t.Errorf("flattenSpans() = %q, want it unchanged as %q", got, title)
	}
}

// TestTreeShowsHeadingTitlesFlattened is the end-to-end check: a page
// whose title comes from its heading -- there being no `title:` in the
// frontmatter -- reaches the tree with its Quarto attribute block gone and
// its spans replaced by the symbols the active stylesheet gives them.
func TestTreeShowsHeadingTitlesFlattened(t *testing.T) {
	srv, root := testServer(t)
	writeFile(t, root, "chapter2/second.qmd",
		"---\norder: 1\n---\n# PERLE OnCall-Schulungen {.unnumbered .unlisted}\n")
	writeFile(t, root, "chapter2/third.qmd",
		"---\norder: 2\n---\n# [Polizei-]{.pol}[Feuerwehr-]{.fw}Spickzettel\n")

	body := get(t, srv, "/tree").Body.String()
	for _, want := range []string{
		"PERLE OnCall-Schulungen",
		"🚔Polizei-🚒Feuerwehr-Spickzettel",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("tree does not contain %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"unnumbered", "unlisted", "{.pol}", "[Polizei-]"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("tree still contains %q:\n%s", unwanted, body)
		}
	}
}

// The preview is rendered in the browser, out of a Go test's reach, so
// what can be checked here is that the script still carries the pass that
// keeps a heading's attribute block -- `{.unnumbered .unlisted}` -- out of
// the previewed heading, and that it runs before the Markdown is parsed.
func TestPreviewStripsHeadingAttributes(t *testing.T) {
	preview, err := assets.ReadFile("assets/static/preview.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"function stripHeadingAttrs(body)",
		"marked.parse(convertDivs(stripHeadingAttrs(page.body)))",
	} {
		if !strings.Contains(string(preview), want) {
			t.Errorf("preview.js does not strip heading attributes, missing %q", want)
		}
	}
}
