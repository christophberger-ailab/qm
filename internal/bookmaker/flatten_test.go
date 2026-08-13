package bookmaker

import (
	"os"
	"strings"
	"testing"
)

const (
	goldenPath       = "../../testdata/book.golden.qmd"
	slidesGoldenPath = "../../testdata/slides.golden.qmd"
)

func flattenTestBook(t *testing.T) *Result {
	t.Helper()
	root := loadTestTree(t)
	res, err := Flatten(root, Options{
		ProjectRoot:   testProject,
		RewriteLinks:  true,
		WrapAudience:  true,
		ExtractSlides: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestFlattenGolden compares the two flattened documents of the fixture book
// against checked-in expectations. Run `go test ./... -update` to refresh
// them after an intentional change.
func TestFlattenGolden(t *testing.T) {
	res := flattenTestBook(t)

	for _, tc := range []struct{ path, got string }{
		{goldenPath, res.Content},
		{slidesGoldenPath, res.Slides},
	} {
		if *update {
			if err := os.WriteFile(tc.path, []byte(tc.got), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s updated", tc.path)
			continue
		}

		want, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if tc.got != string(want) {
			t.Errorf("output differs from %s\n--- got ---\n%s", tc.path, tc.got)
		}
	}
}

func TestFlattenStats(t *testing.T) {
	res := flattenTestBook(t)

	if res.Pages != 8 {
		t.Errorf("Pages = %d, want 8", res.Pages)
	}
	if res.Links != 1 {
		t.Errorf("Links = %d, want 1", res.Links)
	}
	if res.SlideBlocks != 3 {
		t.Errorf("SlideBlocks = %d, want 3", res.SlideBlocks)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}
}

// TestFlattenHasNoFrontMatter guards the fix for a Quarto trap: when the
// document is pulled into a chapter with `{{< include >}}`, Quarto scans the
// merged chapter for its opening heading line by line. An inlined
// front-matter block's closing `---` reads as a Setext underline, and the
// book comes out titled `title: <the included document's title>`.
func TestFlattenHasNoFrontMatter(t *testing.T) {
	res := flattenTestBook(t)

	for _, doc := range []string{res.Content, res.Slides} {
		if strings.HasPrefix(doc, "---") {
			t.Errorf("the document starts with front matter:\n%s", firstLines(doc, 4))
		}
		if strings.Contains(doc, "title:") {
			t.Errorf("the document declares a title:\n%s", doc)
		}
	}
}

// TestFlattenBookTitleIsUnnumbered checks that the document's first level-1
// heading names the book rather than chapter 1, and so is excluded from the
// chapter numbering, while the chapters themselves are not.
func TestFlattenBookTitleIsUnnumbered(t *testing.T) {
	res := flattenTestBook(t)

	if !strings.Contains(res.Content, "# Test Book {#bm-book .unnumbered}") {
		t.Errorf("the book title heading is not unnumbered:\n%s", res.Content)
	}
	if strings.Count(res.Content, ".unnumbered") != 1 {
		t.Errorf("only the book title may be unnumbered:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "# Erster Abschnitt {#bm-book-erster}") {
		t.Errorf("a chapter lost its numbering:\n%s", res.Content)
	}
}

// TestFlattenBookTitleIsUnnumberedWhenGenerated covers the other shape: a
// book root whose body never spells out its title, so the title heading is
// generated rather than found.
func TestFlattenBookTitleIsUnnumberedWhenGenerated(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/_quarto.yml", "project:\n  type: website\n")
	writeFile(t, dir+"/book/index.qmd", "---\ntitle: Das Buch\n---\n\n## Ein Abschnitt\n")

	root, err := LoadTree(dir+"/book", dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Flatten(root, Options{ProjectRoot: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Content, "# Das Buch {#bm-book .unnumbered}") {
		t.Errorf("generated book title is not unnumbered:\n%s", res.Content)
	}
}

func TestFlattenTitleFromRootIndex(t *testing.T) {
	res := flattenTestBook(t)
	if !containsHeading(res.Content, "# ", "Test Book") {
		t.Errorf("book title heading missing:\n%s", firstLines(res.Content, 4))
	}
}

func TestFlattenTitleOverride(t *testing.T) {
	root := loadTestTree(t)
	res, err := Flatten(root, Options{ProjectRoot: testProject, Title: "Andere: Überschrift"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "# Andere: Überschrift {#bm-book .unnumbered}") {
		t.Errorf("title not overridden:\n%s", firstLines(res.Content, 4))
	}
	if strings.Contains(res.Content, "# Test Book") {
		t.Errorf("the original title survived:\n%s", firstLines(res.Content, 4))
	}
}

// TestFlattenSlidesLeaveTheBook checks the split: the book keeps every div
// except `::: slide`, and the slide content turns up in the deck instead.
func TestFlattenSlidesLeaveTheBook(t *testing.T) {
	res := flattenTestBook(t)

	for _, marker := range []string{"::: slide", "nur für Folien", "Sprechernotizen", "Folie nur für die Feuerwehr"} {
		if strings.Contains(res.Content, marker) {
			t.Errorf("slide content %q stayed in the book:\n%s", marker, res.Content)
		}
	}
	for _, marker := range []string{"nur für Folien", "Sprechernotizen", "Folie nur für die Feuerwehr"} {
		if !strings.Contains(res.Slides, marker) {
			t.Errorf("slide content %q is missing from the deck:\n%s", marker, res.Slides)
		}
	}
	// The fences are what is dropped, not the divs nested inside a slide.
	if strings.Contains(res.Slides, "::: slide") {
		t.Errorf("the deck still carries slide fences:\n%s", res.Slides)
	}
	if !strings.Contains(res.Slides, ":::: {.notes}") {
		t.Errorf("a div nested in a slide was dropped:\n%s", res.Slides)
	}
	// Prose divs belong to the book alone.
	if strings.Contains(res.Slides, "::: explanation") {
		t.Errorf("book content leaked into the deck:\n%s", res.Slides)
	}
	if !balanced(res.Slides) {
		t.Errorf("the deck is not balanced:\n%s", res.Slides)
	}
}

// TestFlattenSlideHeadingsAreUntouched guards what a deck is made of: its
// headings cut it into slides, so they keep the level the author wrote and
// take no part in the book's anchor and numbering scheme.
func TestFlattenSlideHeadingsAreUntouched(t *testing.T) {
	res := flattenTestBook(t)

	if !containsHeading(res.Slides, "# ", "Erster Abschnitt") {
		t.Errorf("a slide heading was shifted:\n%s", res.Slides)
	}
	if strings.Contains(res.Slides, "{#bm-") {
		t.Errorf("the deck carries book anchors:\n%s", res.Slides)
	}
	if strings.Contains(res.Slides, ".unnumbered") {
		t.Errorf("the deck carries book numbering attributes:\n%s", res.Slides)
	}
}

// TestFlattenSlidesKeepAudienceWrapping checks that a deck is still built per
// agency: slides taken out of an `_FW` folder keep the marker the content
// filter selects them by.
func TestFlattenSlidesKeepAudienceWrapping(t *testing.T) {
	res := flattenTestBook(t)

	if !opensDivOfClass(res.Slides, "fw") {
		t.Errorf("the _FW folder's slides lost their wrapper:\n%s", res.Slides)
	}
}

// TestFlattenWithoutExtractSlides covers the other setting: the slide divs
// stay in the book and no deck is produced.
func TestFlattenWithoutExtractSlides(t *testing.T) {
	root := loadTestTree(t)
	res, err := Flatten(root, Options{ProjectRoot: testProject})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "::: slide") {
		t.Errorf("slide divs were dropped from the book:\n%s", res.Content)
	}
	if res.Slides != "" || res.SlideBlocks != 0 {
		t.Errorf("a deck was produced anyway: %d blocks\n%s", res.SlideBlocks, res.Slides)
	}
}

// TestFlattenWithoutSlideBlocks checks that a book that has no slides
// produces no deck at all rather than an empty document.
func TestFlattenWithoutSlideBlocks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/_quarto.yml", "project:\n  type: website\n")
	writeFile(t, dir+"/book/index.qmd", "---\ntitle: B\n---\n\n::: explanation\n# B\n:::\n")

	root, err := LoadTree(dir+"/book", dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Flatten(root, Options{ProjectRoot: dir, ExtractSlides: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Slides != "" {
		t.Errorf("Slides = %q, want empty", res.Slides)
	}
}

func TestFlattenHeadingLevels(t *testing.T) {
	res := flattenTestBook(t)

	tests := []struct{ heading, want string }{
		// The book root's index becomes the first chapter.
		{"Test Book", "# "},
		// First-level folders are chapters, deeper ones nest.
		{"Erster Abschnitt", "# "},
		{"Alpha-Seite", "## "},
		{"Tiefe Seite", "### "},
		// A sub-heading inside a page keeps its relative depth.
		{"Unterpunkt", "### "},
		// A page whose title lives only in the front matter gets a
		// generated chapter heading; its own sections stay below it.
		{"Zweiter Abschnitt", "# "},
		{"Ein Unterpunkt", "## "},
	}

	for _, tc := range tests {
		if !containsHeading(res.Content, tc.want, tc.heading) {
			t.Errorf("expected %q at level %q", tc.heading, strings.TrimSpace(tc.want))
		}
	}
}

func TestFlattenAudienceWrapping(t *testing.T) {
	res := flattenTestBook(t)

	if !strings.Contains(res.Content, ":::: fw\n") {
		t.Error("the _FW folder was not wrapped in a fw div")
	}
	// A page nested inside an already-wrapped folder must not be wrapped
	// a second time.
	if strings.Count(res.Content, " fw\n") != 1 {
		t.Errorf("expected exactly one fw wrapper, content:\n%s", res.Content)
	}

	root := loadTestTree(t)
	plain, err := Flatten(root, Options{ProjectRoot: testProject})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.Content, ":::: fw") {
		t.Error("WrapAudience=false still produced a wrapper")
	}
}

func TestFlattenLinkRewriting(t *testing.T) {
	res := flattenTestBook(t)

	if !strings.Contains(res.Content, "[Alpha](#bm-book-erster-alpha)") {
		t.Error("in-book link was not rewritten")
	}
	if !strings.Contains(res.Content, "[Extern](/anderes/kapitel/)") {
		t.Error("link leaving the book must not be rewritten")
	}
	if !strings.Contains(res.Content, "![Bord](/assets/images/bord.png)") {
		t.Error("media path must survive untouched")
	}

	root := loadTestTree(t)
	plain, err := Flatten(root, Options{ProjectRoot: testProject})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain.Content, "[Alpha](/book/erster/alpha/)") {
		t.Error("RewriteLinks=false still rewrote the link")
	}
}

func TestFlattenAnchorsAreUnique(t *testing.T) {
	root := loadTestTree(t)
	res, err := Flatten(root, Options{ProjectRoot: testProject, RewriteLinks: true, WrapAudience: true})
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	root.Walk(func(n *Node) {
		if n.Anchor == "" {
			t.Errorf("%s: no anchor assigned", n.Rel)
			return
		}
		if seen[n.Anchor] {
			t.Errorf("%s: anchor %q is not unique", n.Rel, n.Anchor)
		}
		seen[n.Anchor] = true
		if !hasAnchor(res.Content, n.Anchor) {
			t.Errorf("%s: anchor %q missing from the output", n.Rel, n.Anchor)
		}
	})
}

func TestFlattenRepairsUnbalancedPage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/_quarto.yml", "project:\n  type: website\n")
	writeFile(t, dir+"/book/index.qmd", "---\ntitle: B\norder: 1\n---\n\n# B\n")
	// This page forgets to close both a code fence and a div.
	writeFile(t, dir+"/book/kaputt/index.qmd", "---\norder: 1\n---\n\n::: explanation\n## Kaputt\n\n```sql\nSELECT 1\n")
	writeFile(t, dir+"/book/danach/index.qmd", "---\norder: 2\n---\n\n::: explanation\n## Danach\n:::\n")

	root, err := LoadTree(dir+"/book", dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Flatten(root, Options{ProjectRoot: dir})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "unbalanced markup repaired") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	// The following chapter must still be a heading, not code or div content.
	if !containsHeading(res.Content, "# ", "Danach") {
		t.Errorf("the next page was swallowed:\n%s", res.Content)
	}
	if !balanced(res.Content) {
		t.Errorf("output is not balanced:\n%s", res.Content)
	}
}

func TestFlattenDirectoryWithoutIndex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/_quarto.yml", "project:\n  type: website\n")
	writeFile(t, dir+"/book/index.qmd", "---\ntitle: B\n---\n\n# B\n")
	writeFile(t, dir+"/book/ohne-index/seite.qmd", "---\norder: 1\n---\n\n## Seite\n")

	root, err := LoadTree(dir+"/book", dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Flatten(root, Options{ProjectRoot: dir})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "no index.qmd") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	// The folder still contributes a heading so its child stays nested.
	if !containsHeading(res.Content, "# ", "Ohne index") {
		t.Errorf("missing generated folder heading:\n%s", res.Content)
	}
	if !containsHeading(res.Content, "## ", "Seite") {
		t.Errorf("child page is at the wrong level:\n%s", res.Content)
	}
}

func TestFlattenPageWithoutHeadingOrTitle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/_quarto.yml", "project:\n  type: website\n")
	writeFile(t, dir+"/book/index.qmd", "---\ntitle: B\n---\n\n# B\n")
	// A page made of nothing but a slide: the chapter it stands for is
	// still needed in the book, and neither a heading nor a title says
	// what to call it, so the folder name does.
	writeFile(t, dir+"/book/nur-folien/index.qmd", "---\norder: 1\n---\n\n::: slide\n- x\n:::\n")

	root, err := LoadTree(dir+"/book", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, extract := range []bool{false, true} {
		res, err := Flatten(root, Options{ProjectRoot: dir, ExtractSlides: extract})
		if err != nil {
			t.Fatal(err)
		}
		if !containsHeading(res.Content, "# ", "Nur folien") {
			t.Errorf("ExtractSlides=%v: expected a generated heading:\n%s", extract, res.Content)
		}
	}
}

func TestNormaliseTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Die  Wartenwand", "die wartenwand"},
		{"**Fett**", "fett"},
		{`„Zitat"`, "zitat"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := normaliseTitle(tc.in); got != tc.want {
			t.Errorf("normaliseTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWrapInDiv(t *testing.T) {
	got := wrapInDiv("::: a\n:::::: b\n::::::\n:::", "fw")
	want := "::::::: fw\n::: a\n:::::: b\n::::::\n:::\n:::::::"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTrimBlankEdges(t *testing.T) {
	got := trimBlankEdges([]string{"", "  ", "a", "", "b", "", ""})
	want := []string{"a", "", "b"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
}
