package docrender

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/signintech/gopdf"
)

const pageW, pageH, margin = 595.28, 841.89, 48.0

type pdfWriter struct {
	p        *gopdf.GoPdf
	y        float64
	page     int
	root     string
	headings []tocEntry
	ctx      context.Context
	fonts    fontSet
	// trace, if set, receives each paragraph line as it is drawn (tests).
	trace func([]piece)
	// left and right bound the text column; inset keeps text clear of the
	// top and bottom page margins further, inside a callout box.
	left, right, inset float64
	// dry lays text out without drawing it or breaking pages, to measure.
	dry bool
}
type tocEntry struct {
	title, id   string
	level, page int
	y           float64
}

// piece is a measured part of a line: text, or an inline image whose Image
// is the resolved path and height is its drawn height.
type piece struct {
	Run
	width, height float64
}

// PDF uses embedded Unicode fonts and measured, paginated text. It never invokes
// a typesetting process. Unsupported AST constructs are errors, not lost text.
func PDF(path string, doc *Document, o Options) error {
	return PDFContext(context.Background(), path, doc, o)
}

// PDFContext is PDF with cancellation: layout stops with an error once ctx
// is done. Callers that must not block indefinitely should additionally run
// it with a timeout of their own, since a call into gopdf cannot be
// interrupted (see RunPDF).
func PDFContext(ctx context.Context, path string, doc *Document, o Options) error {
	bb, err := blocks(doc.Blocks, 0)
	if err != nil {
		return err
	}
	fonts, err := loadFonts(o.Fonts)
	if err != nil {
		return err
	}
	p := &gopdf.GoPdf{}
	p.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageW, H: pageH}})
	if err = fonts.register(p); err != nil {
		return err
	}
	w := &pdfWriter{p: p, root: o.Root, ctx: ctx, fonts: fonts, left: margin, right: pageW - margin}
	if o.Cover != "" {
		if err = ValidateCover(o.Cover); err != nil {
			return err
		}
		p.AddPage()
		w.page++
		c, err := imageSize(o.Cover)
		if err != nil {
			return err
		}
		scale := math.Max(pageW/float64(c.Width), pageH/float64(c.Height))
		iw, ih := float64(c.Width)*scale, float64(c.Height)*scale
		// Anchor bottom-right: the brand graphic ends at those edges, so any
		// overflow from cover-scaling is cropped at the top/left instead.
		if err = p.Image(o.Cover, pageW-iw, pageH-ih, &gopdf.Rect{W: iw, H: ih}); err != nil {
			return err
		}
	}
	// Reserve a measured contents section, then fill in its page numbers after
	// laying out the body. Long titles wrap without colliding with page numbers.
	var entries []tocEntry
	var collect func([]Block)
	collect = func(bb []Block) {
		for _, b := range bb {
			if b.Callout != nil {
				collect(b.Callout.Title)
				collect(b.Callout.Body)
			}
			if b.Level > 0 && b.Level <= 3 && strings.TrimSpace(text(b.Runs)) != "" {
				entries = append(entries, tocEntry{title: text(b.Runs), id: b.ID, level: b.Level})
			}
		}
	}
	collect(bb)
	tocStart, tocPages := 0, 0
	if o.TOC && len(entries) > 0 {
		w.newPage()
		tocStart = w.page
		if err = w.paragraph([]Run{{Text: "Inhalt", Heading: true}}, 22, margin, pageW-2*margin); err != nil {
			return err
		}
		for i := range entries {
			e := &entries[i]
			w.ensure(13.5)
			e.page, e.y = w.page, w.y
			if err = w.paragraph([]Run{{Text: e.title}}, 10, margin+float64(e.level-1)*12, pageW-2*margin-45-float64(e.level-1)*12); err != nil {
				return err
			}
		}
		tocPages = w.page - tocStart + 1
	}
	w.newPage()
	if o.Title != "" && o.Cover == "" {
		if err = w.paragraph([]Run{{Text: o.Title, Heading: true}}, 24, margin, pageW-2*margin); err != nil {
			return err
		}
		w.y += 12
	}
	if err = w.body(bb); err != nil {
		return err
	}
	last := w.page
	if tocPages > 0 {
		// Fill numbers at the positions recorded during the actual text layout.
		index := 0
		for _, heading := range w.headings {
			if heading.level > 3 || strings.TrimSpace(heading.title) == "" {
				continue
			}
			e := entries[index]
			index++
			if err = p.SetPage(e.page); err != nil {
				return err
			}
			if err = p.SetFont(fonts["regular"].name, "", 10); err != nil {
				return err
			}
			p.SetXY(pageW-margin-25, e.y)
			if err = p.Cell(nil, fmt.Sprint(heading.page)); err != nil {
				return err
			}
			if e.id != "" {
				p.AddInternalLink(e.id, margin, e.y, pageW-2*margin, 13.5)
			}
		}
	}
	// Cover has neither header nor page number.
	first := 1
	if o.Cover != "" {
		first = 2
	}
	for page := first; page <= last; page++ {
		if err = p.SetPage(page); err != nil {
			return err
		}
		p.SetFont(fonts["regular"].name, "", 9)
		p.SetXY(pageW-margin-25, pageH-28)
		if err = p.Cell(nil, fmt.Sprint(page)); err != nil {
			return err
		}
	}
	return p.WritePdf(path)
}
func (w *pdfWriter) newPage() { w.p.AddPage(); w.page++; w.y = w.top() }
func (w *pdfWriter) ensure(h float64) {
	if !w.dry && w.y+h > w.bottom() {
		w.newPage()
	}
}

// top and bottom bound the text on a page.
func (w *pdfWriter) top() float64    { return margin + w.inset }
func (w *pdfWriter) bottom() float64 { return pageH - margin - w.inset }

// body lays out blocks in the text column.
func (w *pdfWriter) body(bb []Block) error {
	p := w.p
	for _, b := range bb {
		if err := w.checkDeadline(); err != nil {
			return err
		}
		if b.Break {
			// Like LaTeX's \newpage, a break on a page that holds nothing yet
			// does not add a blank page.
			if w.y > w.top() {
				w.newPage()
			}
			continue
		}
		if b.Callout != nil {
			if err := w.callout(b); err != nil {
				return err
			}
			continue
		}
		if len(b.Rows) > 0 {
			if err := w.table(b); err != nil {
				return err
			}
			continue
		}
		size := 11.0
		left := w.left + float64(b.Indent)*15
		if b.Level > 0 {
			size = math.Max(12, 23-float64(b.Level)*2)
			w.y += 12
			w.ensure(size * 2.8)
			p.SetXY(left, w.y)
			if b.ID != "" {
				p.SetAnchor(b.ID)
			}
			p.AddOutline(text(b.Runs))
			w.headings = append(w.headings, tocEntry{title: text(b.Runs), id: b.ID, level: b.Level, page: w.page})
			for i := range b.Runs {
				b.Runs[i].Heading = true
			}
		}
		if b.Code {
			size = 9
		}
		if err := w.paragraph(b.Runs, size, left, w.right-left); err != nil {
			return err
		}
	}
	return nil
}

const (
	calloutPad = 6.0
	calloutBar = 3.0
)

// callout draws a box around a callout, in its border color with a thick
// left bar like Quarto's, so that it is clear where the callout ends. The
// title sits on the shaded header and stays on the page of the body's
// first lines. A body that runs over pages keeps its sides open there.
func (w *pdfWriter) callout(b Block) error {
	c := b.Callout
	left, right := w.left+float64(b.Indent)*15, w.right
	oldLeft, oldRight, oldInset := w.left, w.right, w.inset
	defer func() { w.left, w.right, w.inset = oldLeft, oldRight, oldInset }()
	w.left, w.right = left+calloutBar+calloutPad, right-calloutPad
	headerH := 0.0
	if len(c.Title) > 0 {
		y := w.y
		w.dry = true
		for _, t := range c.Title {
			if err := w.paragraph(t.Runs, 11, w.left, w.right-w.left); err != nil {
				w.dry = false
				return err
			}
		}
		w.dry = false
		// paragraph leaves 7pt below; the header pads its text evenly.
		headerH = w.y - y - 7 + 2*calloutPad
		w.y = y
	}
	w.y += 3
	w.ensure(headerH + 2*calloutPad + 11*1.35)
	top, first := w.y, w.page
	if headerH > 0 {
		w.p.SetFillColor(c.Fill[0], c.Fill[1], c.Fill[2])
		w.p.RectFromUpperLeftWithStyle(left, top, right-left, headerH, "F")
		w.y += calloutPad
		for _, t := range c.Title {
			if err := w.paragraph(t.Runs, 11, w.left, w.right-w.left); err != nil {
				return err
			}
		}
		w.y = top + headerH
	}
	w.y += calloutPad
	w.inset = calloutPad
	if err := w.body(c.Body); err != nil {
		return err
	}
	// The last block's spacing below closes the box.
	end, last := w.y, w.page
	for page := first; page <= last; page++ {
		if err := w.p.SetPage(page); err != nil {
			return err
		}
		y0, y1 := margin, pageH-margin
		if page == first {
			y0 = top
		}
		if page == last {
			y1 = end
		}
		w.p.SetStrokeColor(c.Border[0], c.Border[1], c.Border[2])
		w.p.SetLineWidth(.5)
		if page == first {
			w.p.Line(left, y0, right, y0)
		}
		if page == last {
			w.p.Line(left, y1, right, y1)
		}
		w.p.Line(right, y0, right, y1)
		w.p.SetFillColor(c.Border[0], c.Border[1], c.Border[2])
		w.p.RectFromUpperLeftWithStyle(left, y0, calloutBar, y1-y0, "F")
	}
	w.y = end + 7
	return nil
}

// fontRole returns the role of r's font in FontRoles.
func fontRole(r Run) string {
	f := "regular"
	if r.Heading {
		f = "heading"
	} else if r.Bold && r.Italic {
		f = "bolditalic"
	} else if r.Bold {
		f = "bold"
	} else if r.Italic {
		f = "italic"
	}
	if r.Code {
		f = "mono"
	}
	return f
}

// measure returns the width of r.Text in r's font, with emoji in the emoji
// font.
func (w *pdfWriter) measure(r Run, size float64) (float64, error) {
	width := 0.0
	for _, s := range w.fonts.segments(r.Text, fontRole(r)) {
		if err := w.p.SetFont(s.font.name, "", size); err != nil {
			return 0, err
		}
		sw, err := w.p.MeasureTextWidth(s.text)
		if err != nil {
			return 0, err
		}
		width += sw
	}
	return width, nil
}

// ascent is the distance from the top of a line of r to its baseline.
func (w *pdfWriter) ascent(r Run, size float64) float64 {
	return w.fonts[fontRole(r)].ascent * size
}

// draw writes r.Text with its baseline at x, baseline like measure lays it
// out. Faces differ in ascent, so each segment is placed by its own. The
// caller sets the text color.
func (w *pdfWriter) draw(r Run, size, x, baseline float64) error {
	for _, s := range w.fonts.segments(r.Text, fontRole(r)) {
		if err := w.p.SetFont(s.font.name, "", size); err != nil {
			return err
		}
		sw, err := w.p.MeasureTextWidth(s.text)
		if err != nil {
			return err
		}
		w.p.SetXY(x, baseline-s.font.ascent*size)
		if err = w.p.Cell(nil, s.text); err != nil {
			return err
		}
		x += sw
	}
	return nil
}

func (w *pdfWriter) paragraph(runs []Run, size, left, width float64) error {
	var line []piece
	used := 0.0
	lh := size * 1.35
	// The baseline sits below the line's top by the largest ascent among
	// the paragraph's faces, so that runs in different faces share it.
	ascent := 0.0
	for _, r := range runs {
		if r.Image == "" {
			ascent = max(ascent, w.ascent(r, size))
		}
	}
	if ascent == 0 {
		ascent = w.ascent(Run{}, size)
	}
	flush := func() error {
		if len(line) == 0 {
			return nil
		}
		// Inline images sit on the baseline; a tall one makes its line
		// taller above the baseline.
		above := ascent
		for _, s := range line {
			above = max(above, s.height)
		}
		lineH := above + lh - ascent
		w.ensure(lineH)
		if w.trace != nil && !w.dry {
			w.trace(line)
		}
		x := left
		for _, s := range line {
			if w.dry {
				break
			}
			y, h := w.y+above-ascent, lh
			if s.Image != "" {
				y, h = w.y+above-s.height, s.height
				if err := w.p.Image(s.Image, x, y, &gopdf.Rect{W: s.width, H: s.height}); err != nil {
					return err
				}
			} else {
				w.p.SetTextColor(30, 35, 42)
				if s.Link != "" {
					w.p.SetTextColor(24, 84, 139)
				}
				if err := w.draw(s.Run, size, x, w.y+above); err != nil {
					return err
				}
			}
			if strings.HasPrefix(s.Link, "#") {
				w.p.AddInternalLink(strings.TrimPrefix(s.Link, "#"), x, y, s.width, h)
			} else if s.Link != "" {
				w.p.AddExternalLink(s.Link, x, y, s.width, h)
			}
			x += s.width
		}
		w.y += lineH
		line = nil
		used = 0
		return nil
	}
	// A word may span runs: a quote mark, emphasis or a link that touches
	// the text beside it must not open a line break, so pieces collect in
	// word until a space or the end and are placed as one unit.
	var word []piece
	wordW := 0.0
	place := func() error {
		defer func() { word, wordW = nil, 0 }()
		if len(word) == 0 {
			return nil
		}
		if used+wordW > width && len(line) > 0 {
			if err := flush(); err != nil {
				return err
			}
		}
		if wordW <= width {
			line = append(line, word...)
			used += wordW
			return nil
		}
		// Oversized words are split by rune, so long URLs and identifiers
		// cannot escape the page.
		for _, p := range word {
			if p.Image != "" {
				if used+p.width > width && len(line) > 0 {
					if err := flush(); err != nil {
						return err
					}
				}
				line = append(line, p)
				used += p.width
				continue
			}
			for _, c := range p.Text {
				s := p.Run
				s.Text = string(c)
				cw, err := w.measure(s, size)
				if err != nil {
					return err
				}
				if used+cw > width && len(line) > 0 {
					if err = flush(); err != nil {
						return err
					}
				}
				line = append(line, piece{Run: s, width: cw})
				used += cw
			}
		}
		return nil
	}
	// An image alone in its paragraph is a picture of its own; images among
	// text flow with it, like a word.
	alone := soleImage(runs)
	for _, r := range runs {
		if r.Image != "" && !alone {
			path, iw, ih, err := w.fitImage(r, size, width)
			if err != nil {
				return err
			}
			s := r
			s.Image = path
			word = append(word, piece{Run: s, width: iw, height: ih})
			wordW += iw
			continue
		}
		if r.Image != "" {
			if err := place(); err != nil {
				return err
			}
			if err := flush(); err != nil {
				return err
			}
			path, iw, ih, err := w.fitImage(r, size, width)
			if err != nil {
				return err
			}
			w.ensure(ih + 10)
			if !w.dry {
				if err = w.p.Image(path, left, w.y, &gopdf.Rect{W: iw, H: ih}); err != nil {
					return err
				}
			}
			w.y += ih + 10
			continue
		}
		var text strings.Builder
		addText := func() error {
			if text.Len() == 0 {
				return nil
			}
			s := r
			s.Text = text.String()
			text.Reset()
			tw, err := w.measure(s, size)
			if err != nil {
				return err
			}
			word = append(word, piece{Run: s, width: tw})
			wordW += tw
			return nil
		}
		// Spaces and explicit breaks are kept; leading spaces are dropped.
		for _, c := range r.Text {
			if !unicode.IsSpace(c) {
				text.WriteRune(c)
				continue
			}
			if err := addText(); err != nil {
				return err
			}
			if err := place(); err != nil {
				return err
			}
			if c == '\n' {
				if len(line) == 0 {
					w.ensure(lh)
					w.y += lh
				} else if err := flush(); err != nil {
					return err
				}
				continue
			}
			if len(line) == 0 {
				continue
			}
			s := r
			s.Text = string(c)
			tw, err := w.measure(s, size)
			if err != nil {
				return err
			}
			if used+tw > width {
				if err = flush(); err != nil {
					return err
				}
				continue
			}
			line = append(line, piece{Run: s, width: tw})
			used += tw
		}
		if err := addText(); err != nil {
			return err
		}
	}
	if err := place(); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	w.y += 7
	return nil
}

// soleImage reports whether runs hold one image and otherwise only blank
// text.
func soleImage(runs []Run) bool {
	images := 0
	for _, r := range runs {
		if r.Image != "" {
			images++
		} else if strings.TrimSpace(r.Text) != "" {
			return false
		}
	}
	return images == 1
}

// cellItem is one laid-out piece of a table cell: a wrapped text line or
// an image, with the vertical space it takes.
type cellItem struct {
	text, image string
	w, h        float64
}

func (w *pdfWriter) table(b Block) error {
	n := len(b.Rows[0])
	if n == 0 {
		return nil
	}
	width := w.right - w.left
	widths := make([]float64, n)
	sum := 0.0
	for _, v := range b.Widths {
		sum += v
	}
	for i := range widths {
		widths[i] = width / float64(n)
		if sum > 0 && i < len(b.Widths) && b.Widths[i] > 0 {
			widths[i] = width * b.Widths[i] / sum
		}
	}
	// Pandoc derives relative widths from the dashes of a pipe table, which
	// can leave a column (typically "Nr.") narrower than a single glyph.
	// Every column must at least hold its widest character plus padding.
	minimums := make([]float64, n)
	for rowIndex, row := range b.Rows {
		if len(row) != n {
			return fmt.Errorf("inconsistent PDF table column count")
		}
		for i, cell := range row {
			g, err := w.widestRune(text(cell), Run{Bold: rowIndex == 0}, 9)
			if err != nil {
				return err
			}
			minimums[i] = max(minimums[i], g+2*cellPadding)
		}
	}
	widths = enforceMinimums(widths, minimums)
	for rowIndex, row := range b.Rows {
		if err := w.checkDeadline(); err != nil {
			return err
		}
		items := make([][]cellItem, n)
		rowH := 0.0
		hasImage := false
		for i, cell := range row {
			if len(cell) == 0 {
				cell = Cell{{}}
			}
			for _, r := range cell {
				if r.Image != "" {
					path, iw, ih, err := w.fitImage(r, 9, widths[i]-2*cellPadding)
					if err != nil {
						return err
					}
					items[i] = append(items[i], cellItem{image: path, w: iw, h: ih + 4})
					hasImage = true
					continue
				}
				for _, part := range strings.Split(r.Text, "\n") {
					if part == "" {
						items[i] = append(items[i], cellItem{h: 12})
						continue
					}
					ll, err := w.wrap(part, Run{Bold: rowIndex == 0}, 9, widths[i]-2*cellPadding)
					if err != nil {
						return err
					}
					for _, l := range ll {
						items[i] = append(items[i], cellItem{text: l, h: 12})
					}
				}
			}
			total := 0.0
			for _, it := range items[i] {
				total += it.h
			}
			rowH = max(rowH, total)
		}
		// A row with an image moves to the next page whole when it fits
		// there; text-only rows split across pages as before.
		if hasImage && w.y+rowH+8 > w.bottom() && rowH+8 <= w.bottom()-w.top() {
			w.newPage()
		}
		// Split tall rows across pages rather than clipping their contents.
		next := make([]int, n)
		for {
			w.ensure(22)
			avail := w.bottom() - w.y - 8
			take := make([]int, n)
			used := 0.0
			pending := false
			for i := range items {
				h := 0.0
				for next[i]+take[i] < len(items[i]) && h+items[i][next[i]+take[i]].h <= avail {
					h += items[i][next[i]+take[i]].h
					take[i]++
				}
				// Nothing fits even on an empty page: draw one item anyway
				// so the layout always progresses.
				if take[i] == 0 && next[i] < len(items[i]) && w.y == w.top() {
					h = items[i][next[i]].h
					take[i] = 1
				}
				used = max(used, h)
				pending = pending || next[i]+take[i] < len(items[i])
			}
			if used == 0 {
				if !pending {
					break
				}
				w.newPage()
				continue
			}
			h := used + 8
			x := w.left
			for i := range items {
				w.p.SetStrokeColor(175, 185, 195)
				w.p.SetLineWidth(.4)
				w.p.RectFromUpperLeftWithStyle(x, w.y, widths[i], h, "D")
				y := w.y + 4
				for _, it := range items[i][next[i] : next[i]+take[i]] {
					switch {
					case it.image != "":
						if err := w.p.Image(it.image, x+cellPadding, y, &gopdf.Rect{W: it.w, H: it.h - 4}); err != nil {
							return err
						}
					case it.text != "":
						w.p.SetTextColor(30, 35, 42)
						r := Run{Text: it.text, Bold: rowIndex == 0}
						if err := w.draw(r, 9, x+5, y+w.ascent(r, 9)); err != nil {
							return err
						}
					}
					y += it.h
				}
				next[i] += take[i]
				x += widths[i]
			}
			w.y += h
			if !pending {
				break
			}
			w.newPage()
		}
	}
	w.y += 10
	return nil
}

// fitImage resolves an image run and scales it to the available width and
// page height, honouring explicit width and height attributes. With only one
// of them the other follows the aspect ratio; with both the image takes
// exactly that size, unless it must shrink to fit. An em is the font size of
// the surrounding text.
func (w *pdfWriter) fitImage(r Run, size, width float64) (path string, iw, ih float64, err error) {
	path, err = resource(w.root, r.Image)
	if err != nil {
		return "", 0, 0, err
	}
	c, err := imageSize(path)
	if err != nil {
		return "", 0, 0, fmt.Errorf("image %s: %w", r.Image, err)
	}
	maxH := w.bottom() - w.top() - 20
	cw, ch := float64(c.Width), float64(c.Height)
	dw, dh := imageLength(r.ImageWidth, size, width), imageLength(r.ImageHeight, size, maxH)
	switch {
	case dw > 0 && dh > 0:
		iw, ih = dw, dh
	case dw > 0:
		iw, ih = dw, ch*dw/cw
	case dh > 0:
		iw, ih = cw*dh/ch, dh
	default:
		iw, ih = cw*0.75, ch*0.75
	}
	if scale := math.Min(width/iw, maxH/ih); scale < 1 {
		iw, ih = iw*scale, ih*scale
	}
	return path, iw, ih, nil
}

// imageLength converts a Pandoc image dimension to points. A bare number
// is pixels, as in Pandoc; percentages are of available.
func imageLength(value string, em, available float64) float64 {
	for _, unit := range []struct {
		suffix string
		factor float64
	}{{"cm", 72 / 2.54}, {"mm", 72 / 25.4}, {"in", 72}, {"pt", 1}, {"px", .75}, {"em", em}, {"%", available / 100}} {
		if strings.HasSuffix(value, unit.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(value, unit.suffix), 64)
			if err == nil {
				return n * unit.factor
			}
			return 0
		}
	}
	if n, err := strconv.ParseFloat(value, 64); err == nil {
		return n * .75
	}
	return 0
}

const cellPadding = 5.0

// checkDeadline lets a cancelled or timed-out render stop between layout
// steps instead of running on unobserved.
func (w *pdfWriter) checkDeadline() error {
	if w.ctx == nil {
		return nil
	}
	if err := w.ctx.Err(); err != nil {
		return fmt.Errorf("PDF layout aborted: %w", err)
	}
	return nil
}

// widestRune measures the widest single character of s in r's font.
func (w *pdfWriter) widestRune(s string, r Run, size float64) (float64, error) {
	widest := 0.0
	seen := map[rune]bool{}
	for _, c := range s {
		if seen[c] || unicode.IsSpace(c) {
			continue
		}
		seen[c] = true
		r.Text = string(c)
		cw, err := w.measure(r, size)
		if err != nil {
			return 0, err
		}
		widest = max(widest, cw)
	}
	return widest, nil
}

// wrap word-wraps text in r's font. Words wider than the width are broken
// by character, and a character wider than the width gets a line of its
// own, so wrapping always progresses. (gopdf's SplitTextWithWordWrap
// v0.38.1 loops forever on such a character, and it knows no emoji font.)
func (w *pdfWriter) wrap(text string, r Run, size, width float64) ([]string, error) {
	var lines []string
	var line strings.Builder
	used := 0.0
	measure := func(s string) (float64, error) {
		r.Text = s
		return w.measure(r, size)
	}
	push := func() {
		lines = append(lines, line.String())
		line.Reset()
		used = 0
	}
	space, err := measure(" ")
	if err != nil {
		return nil, err
	}
	for _, word := range strings.Fields(text) {
		ww, err := measure(word)
		if err != nil {
			return nil, err
		}
		if line.Len() > 0 && used+space+ww <= width {
			line.WriteString(" " + word)
			used += space + ww
			continue
		}
		if line.Len() > 0 {
			push()
		}
		if ww <= width {
			line.WriteString(word)
			used = ww
			continue
		}
		for _, c := range word {
			cw, err := measure(string(c))
			if err != nil {
				return nil, err
			}
			if line.Len() > 0 && used+cw > width {
				push()
			}
			line.WriteRune(c)
			used += cw
		}
	}
	if line.Len() > 0 {
		push()
	}
	return lines, nil
}

// enforceMinimums raises every column to its minimum and takes the
// difference from the columns with room to spare, in proportion to that
// room. The total width is preserved; if the minimums alone exceed it, the
// columns get their minimums scaled down to fit (wrap still terminates).
func enforceMinimums(widths, minimums []float64) []float64 {
	total, need := 0.0, 0.0
	for i := range widths {
		total += widths[i]
		need += minimums[i]
	}
	out := make([]float64, len(widths))
	if need >= total {
		for i := range out {
			out[i] = total * minimums[i] / need
		}
		return out
	}
	deficit, spare := 0.0, 0.0
	for i := range widths {
		if widths[i] < minimums[i] {
			deficit += minimums[i] - widths[i]
		} else {
			spare += widths[i] - minimums[i]
		}
	}
	for i := range widths {
		switch {
		case widths[i] < minimums[i]:
			out[i] = minimums[i]
		case deficit > 0:
			out[i] = widths[i] - deficit*(widths[i]-minimums[i])/spare
		default:
			out[i] = widths[i]
		}
	}
	return out
}

// ErrPDFTimeout reports a PDF layout that did not finish in time.
var ErrPDFTimeout = errors.New("PDF rendering timed out")

// RunPDF renders like PDFContext but guarantees to return within timeout
// (if > 0). Cooperative cancellation stops qm's own layout loops; should a
// library call hang regardless, the render goroutine is abandoned and the
// caller gets ErrPDFTimeout instead of blocking forever.
func RunPDF(ctx context.Context, timeout time.Duration, path string, doc *Document, o Options) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	done := make(chan error, 1)
	go func() { done <- PDFContext(ctx, path, doc, o) }()
	select {
	case err := <-done:
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w after %s", ErrPDFTimeout, timeout)
		}
		return err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w after %s (layout did not respond; a table or text may be unwrappable)", ErrPDFTimeout, timeout)
		}
		return ctx.Err()
	}
}
