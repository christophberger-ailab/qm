package bookmaker

import "strings"

// This file separates a page's slide content from its prose.
//
// A page carries both: `::: slide` holds the bullet points shown on a
// projector, `::: explanation`, `::: tutorial` and friends hold the text of
// the book. Both used to travel in one flat file and were sorted out at
// render time by the project's content filter, but a slide deck needs more
// than a subset of the book: its headings are what cuts the deck into
// slides, and the chapter and section headings the bookmaker generates
// around the divs have no slide to belong to. So the two are separated
// here, and the slide content becomes a document of its own.

// slideClass is the div class whose content makes up the slide deck.
const slideClass = "slide"

// isSlideFence reports whether a div fence's attributes open a slide.
// Quarto accepts the shorthand `::: slide` as well as the explicit
// `::: {.slide}`, and the project's filter compares class names without
// regard to case.
//
// The comparison is per token, so a neighbouring class that merely starts
// with the same letters -- `::: {.slide-notes}` -- is not a slide.
func isSlideFence(attrs string) bool {
	inner := strings.TrimSpace(attrs)
	if strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}") {
		inner = inner[1 : len(inner)-1]
	}
	for _, tok := range splitAttrTokens(inner) {
		if strings.EqualFold(tok, slideClass) || strings.EqualFold(tok, "."+slideClass) {
			return true
		}
	}
	return false
}

// splitSlides takes a page's lines apart into the ones that stay in the book
// and the content of its slide divs.
//
// The slide fences themselves are dropped: what is left is the slide's own
// Markdown, whose heading levels are the deck's structure and are therefore
// kept exactly as written. Divs enclosing a slide are reproduced around the
// extracted content, so a slide sitting inside `::: pol` remains marked for
// that audience once it stands on its own.
//
// The input is expected to have been through balanceFences, so every div
// opened here is closed here.
func splitSlides(lines []string) (rest, slides []string, count int) {
	var (
		code    codeTracker
		open    []string // attributes of the divs enclosing the current position
		buf     []string // the current slide's content
		inSlide bool
		removed bool // a slide fence was taken out of the page
		depth   int  // div nesting inside the current slide
	)

	// emit ends the current slide and adds it to the deck. A slide with
	// nothing in it adds nothing.
	emit := func() {
		inSlide = false
		if block := enclose(trimBlankEdges(buf), open); len(block) > 0 {
			if len(slides) > 0 {
				slides = append(slides, "")
			}
			slides = append(slides, block...)
			count++
		}
		buf = nil
	}

	for _, line := range lines {
		if code.step(line) {
			// Nothing inside a fenced code block is markup.
			if inSlide {
				buf = append(buf, line)
			} else {
				rest = append(rest, line)
			}
			continue
		}

		m := divFence.FindStringSubmatch(line)
		if m == nil {
			if inSlide {
				buf = append(buf, line)
			} else {
				rest = append(rest, line)
			}
			continue
		}

		if opensDiv(m[2]) {
			switch {
			case inSlide:
				// A div inside the slide, including a nested slide,
				// travels with it.
				depth++
				buf = append(buf, line)
			case isSlideFence(m[2]):
				inSlide, removed, depth, buf = true, true, 0, nil
			default:
				open = append(open, strings.TrimSpace(m[2]))
				rest = append(rest, line)
			}
			continue
		}

		if inSlide {
			if depth > 0 {
				depth--
				buf = append(buf, line)
				continue
			}
			// The slide ends; its fence is not part of the deck.
			emit()
			continue
		}

		if len(open) > 0 {
			open = open[:len(open)-1]
		}
		rest = append(rest, line)
	}

	// balanceFences closes what a page left open, so this is unreachable
	// for a page that went through it -- and losing the content silently
	// would be the wrong way to find out otherwise.
	if inSlide {
		emit()
	}

	if removed {
		// Lifting a block out of the page leaves the blank lines that
		// used to surround it back to back.
		rest = squeezeBlankLines(rest)
	}
	return trimBlankEdges(rest), slides, count
}

// enclose wraps extracted slide content in the divs that surrounded it in
// the source page, innermost first.
func enclose(content []string, open []string) []string {
	if len(content) == 0 {
		return nil
	}
	for i := len(open) - 1; i >= 0; i-- {
		content = strings.Split(wrapInDiv(strings.Join(content, "\n"), open[i]), "\n")
	}
	return content
}

// squeezeBlankLines reduces every run of blank lines to a single one. Blank
// lines inside a fenced code block are part of the code and are left alone.
func squeezeBlankLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	var (
		code  codeTracker
		blank bool
	)
	for _, line := range lines {
		if code.step(line) {
			out = append(out, line)
			blank = false
			continue
		}
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, line)
	}
	return out
}
