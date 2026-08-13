package bookmaker

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Options controls how a book folder is flattened.
type Options struct {
	// ProjectRoot is the Quarto project root. Page paths are expressed
	// relative to it, which is what makes website-absolute media paths such
	// as `/assets/images/x.png` keep working in the generated file.
	ProjectRoot string

	// Title overrides the book title, which the flattened document carries
	// as its first level-1 heading. When empty the title is taken from the
	// book root's index.qmd.
	Title string

	// RewriteLinks turns links that point at another page of this book into
	// anchor references. Links leaving the book are never touched.
	RewriteLinks bool

	// WrapAudience wraps content coming from a folder with an `_FW` or
	// `_POL` suffix in a matching `::: fw` / `::: pol` div, so that the
	// project's targeted-content filter can select the right variant at
	// render time.
	WrapAudience bool

	// ExtractSlides moves the content of every `::: slide` div out of the
	// book and into Result.Slides, a document of its own. Without it the
	// slide divs stay in the book like any other div.
	ExtractSlides bool
}

// Result is the outcome of flattening a book folder.
type Result struct {
	// Content is the complete generated .qmd document.
	Content string
	// Slides is the generated slide document: the content of every
	// `::: slide` div of the book, its fences removed, in book order. It
	// is empty when the book has no slides or ExtractSlides is off.
	Slides string
	// Pages is the number of source pages that contributed content.
	Pages int
	// Links is the number of in-book links rewritten into anchors.
	Links int
	// SlideBlocks is the number of slide divs moved into Slides.
	SlideBlocks int
	// Warnings collects non-fatal problems found while flattening.
	Warnings []string
}

// bookTitleClasses are the classes put on the book's own title heading --
// the document's first level-1 heading. It names the book rather than a
// chapter of it, so it must stay out of the chapter numbering.
var bookTitleClasses = []string{"unnumbered"}

// Flatten renders a book tree into a single Quarto document.
//
// With Options.ExtractSlides the book's slide content is separated out into
// a second document, Result.Slides: a deck's structure is its own headings,
// which the chapter and section headings generated around the divs would
// only get in the way of.
//
// The documents deliberately carry no YAML front matter. These files are
// meant to be pulled into a book chapter with `{{< include >}}`, and Quarto
// reads a chapter's opening heading with a line scanner that does not know
// it is looking at an included file: the `---` closing an inlined
// front-matter block reads as a Setext underline, and the book ends up
// titled `title: <whatever the first include declared>`. The book title
// belongs in the project's `book:` configuration; here it is the first
// level-1 heading.
func Flatten(root *Node, opts Options) (*Result, error) {
	res := &Result{}
	assignAnchors(root)
	root.bookRoot = true
	root.titleOverride = opts.Title

	var targets linkTargets
	if opts.RewriteLinks {
		targets = buildLinkTargets(root)
	}

	content, slides := renderNode(root, "", targets, opts, res)
	res.Content = content + "\n"
	if slides != "" {
		res.Slides = slides + "\n"
	}

	sort.Strings(res.Warnings)
	return res, nil
}

// renderNode renders a node and its descendants, deepest structure last,
// returning the book text and the slide text side by side. parentAudience
// carries the agency marker already applied by an ancestor so that a `_FW`
// folder nested inside another `_FW` folder is wrapped once.
func renderNode(n *Node, parentAudience string, targets linkTargets, opts Options, res *Result) (book, slides string) {
	bookBlocks := make([]string, 0, len(n.Children)+1)
	slideBlocks := make([]string, 0, len(n.Children)+1)

	page, pageSlides := renderPage(n, targets, opts, res)
	if page != "" {
		bookBlocks = append(bookBlocks, page)
	}
	if pageSlides != "" {
		slideBlocks = append(slideBlocks, pageSlides)
	}

	audience := parentAudience
	if opts.WrapAudience && n.Audience != "" {
		audience = n.Audience
	}

	for _, child := range n.Children {
		sub, subSlides := renderNode(child, audience, targets, opts, res)
		if sub != "" {
			bookBlocks = append(bookBlocks, sub)
		}
		if subSlides != "" {
			slideBlocks = append(slideBlocks, subSlides)
		}
	}

	book = strings.Join(bookBlocks, "\n\n")
	slides = strings.Join(slideBlocks, "\n\n")

	if opts.WrapAudience && n.Audience != "" && n.Audience != parentAudience {
		// The deck is built per agency just as the book is, so the
		// slides lifted out of an `_FW` / `_POL` folder keep the marker
		// that tells the content filter whom they are for.
		if book != "" {
			book = wrapInDiv(book, n.Audience)
		}
		if slides != "" {
			slides = wrapInDiv(slides, n.Audience)
		}
	}
	return book, slides
}

// renderPage produces the flattened Markdown for a single node's own page:
// markup balanced, in-book links resolved and headings normalised to the
// node's level with an anchor attached. Its second return value is the
// page's slide content, which none of that applies to.
func renderPage(n *Node, targets linkTargets, opts Options, res *Result) (page, slides string) {
	lines, slideLines := preparePage(n, targets, opts, res)
	slides = strings.Join(slideLines, "\n")

	headings := findHeadings(lines)
	base := minHeadingLevel(headings)

	// Decide whether the page already carries its own title heading.
	//
	// Quarto's rule for a book chapter is that the `title:` front-matter
	// field is the chapter heading, unless the body supplies one itself.
	// Pages come in both shapes: some repeat the title as a heading inside
	// every content div, others give only sub-headings and rely on the
	// front matter. Treating the body's shallowest heading as the page
	// title in the second case would wrongly promote a section to chapter
	// rank, so a declared title counts as present in the body only when a
	// heading at that shallowest level actually spells it out.
	declared := ""
	if n.Page != nil {
		declared = n.Page.Title
	}

	edit := titleEdit{ID: n.Anchor, Text: n.titleOverride}
	if n.bookRoot {
		edit.Classes = bookTitleClasses
	}

	if base == 0 || !echoesTitle(headings, base, declared) {
		head := strings.Repeat("#", n.Level) + " " + n.Title() +
			" " + mergeAttrs("", n.Anchor, edit.Classes)
		if base == 0 {
			if len(lines) == 0 {
				return head, slides
			}
			return head + "\n\n" + strings.Join(lines, "\n"), slides
		}
		// Body headings are subordinate to the generated title heading.
		lines = shiftHeadings(lines, headings, n, n.Level+1, base, titleEdit{}, res)
		return head + "\n\n" + strings.Join(lines, "\n"), slides
	}

	lines = shiftHeadings(lines, headings, n, n.Level, base, edit, res)
	return strings.Join(lines, "\n"), slides
}

// preparePage turns a node's source body into the lines that will appear in
// the flattened document, before any heading adjustment, and hands back the
// slide content taken out of it.
func preparePage(n *Node, targets linkTargets, opts Options, res *Result) (lines, slides []string) {
	if n.Page == nil {
		if n.IsDir() {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"%s: directory has no index.qmd; a heading was generated from its name", n.Rel))
		}
		return nil, nil
	}

	lines = trimBlankEdges(strings.Split(n.Page.Body, "\n"))

	lines, repair := balanceFences(lines)
	if !repair.clean() {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%s: unbalanced markup repaired (%s); the page renders on its own but would leak into the rest of the book",
			n.Page.Rel, describeRepair(repair)))
	}

	if opts.ExtractSlides {
		// Slides are taken out before the page is touched any further:
		// heading levels are what cuts a deck into slides, and the
		// anchors an in-book link needs do not exist in the deck.
		var count int
		lines, slides, count = splitSlides(lines)
		res.SlideBlocks += count
	}

	if opts.RewriteLinks {
		var changed int
		lines, changed = rewriteLinks(lines, targets, path.Dir(n.Page.Rel))
		res.Links += changed
	}

	for _, line := range lines {
		if strings.Contains(line, "{{< include") {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"%s: contains an include shortcode; Quarto resolves it at render time, "+
					"so its path must be project-absolute", n.Page.Rel))
			break
		}
	}

	res.Pages++
	return lines, slides
}

// echoesTitle reports whether one of the page's shallowest headings restates
// the declared front-matter title, meaning the body already provides the page
// heading.
func echoesTitle(headings []heading, base int, declared string) bool {
	want := normaliseTitle(declared)
	if want == "" {
		// Without a declared title the body's shallowest heading is the
		// page heading by definition, which is Quarto's own rule.
		return true
	}
	for _, h := range headings {
		if h.Level == base && normaliseTitle(h.Text()) == want {
			return true
		}
	}
	return false
}

func normaliseTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '*', '_', '`', '"', '\'', '“', '”', '„':
			return -1
		}
		return r
	}, s)
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// shiftHeadings rewrites every heading so that the shallowest one lands on
// target, preserving the relative depth of the rest, and applies edit to the
// page's own title heading.
//
// The anchor id is attached to the page's own title heading. Sibling content
// divs (`::: explanation`, `::: tutorial`, ...) typically repeat that heading
// verbatim and only some of them survive the project's content filter, so
// every repetition claims the id -- once per div. Pandoc renames the
// duplicates that do survive, leaving the anchor on the first one. Headings
// at the same level that say something else are ordinary sections and are
// left alone.
func shiftHeadings(lines []string, headings []heading, n *Node, target, base int, edit titleEdit, res *Result) []string {
	delta := target - base
	out := append([]string(nil), lines...)

	titleText := ""
	for _, h := range headings {
		if h.Level == base {
			titleText = normaliseTitle(h.Text())
			break
		}
	}
	claimed := map[int]bool{}

	for _, h := range headings {
		level := h.Level + delta
		if level > 6 {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"%s: heading %q nests deeper than level 6 and was clamped", n.Rel, h.Text()))
			level = 6
		}

		applied := titleEdit{}
		if !edit.empty() && level == target && !claimed[h.Block] && normaliseTitle(h.Text()) == titleText {
			applied = edit
			claimed[h.Block] = true
		}
		out[h.Index] = renderHeading(h, level, applied)
	}
	return out
}

// describeRepair renders a fenceRepair for a warning message.
func describeRepair(r fenceRepair) string {
	var parts []string
	if r.Code {
		parts = append(parts, "closed an unterminated code fence")
	}
	if r.Divs > 0 {
		parts = append(parts, fmt.Sprintf("closed %d unterminated div(s)", r.Divs))
	}
	if r.Stray > 0 {
		parts = append(parts, fmt.Sprintf("dropped %d stray closing fence(s)", r.Stray))
	}
	if r.Glued > 0 {
		parts = append(parts, fmt.Sprintf(
			"separated %d div fence(s) from the text above, which Pandoc would have read as part of it", r.Glued))
	}
	return strings.Join(parts, ", ")
}

// wrapInDiv encloses content in a fenced div with the given class, using a
// fence wider than anything the content itself contains.
func wrapInDiv(content, class string) string {
	lines := strings.Split(content, "\n")
	width := maxFenceWidth(lines) + 1
	if width < 3 {
		width = 3
	}
	fence := strings.Repeat(":", width)
	return fence + " " + class + "\n" + content + "\n" + fence
}

// trimBlankEdges removes leading and trailing blank lines.
func trimBlankEdges(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}
