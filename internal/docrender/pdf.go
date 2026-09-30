package docrender

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

const pageW, pageH, margin = 595.28, 841.89, 48.0

type pdfWriter struct {
	p        *gopdf.GoPdf
	y        float64
	page     int
	root     string
	headings []tocEntry
}
type tocEntry struct {
	title, id   string
	level, page int
	y           float64
}
type piece struct {
	Run
	width float64
}

// PDF uses embedded Unicode fonts and measured, paginated text. It never invokes
// a typesetting process. Unsupported AST constructs are errors, not lost text.
func PDF(path string, doc *Document, o Options) error {
	bb, err := blocks(doc.Blocks, 0)
	if err != nil {
		return err
	}
	p := &gopdf.GoPdf{}
	p.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageW, H: pageH}})
	for name, data := range map[string][]byte{"regular": goregular.TTF, "bold": gobold.TTF, "italic": goitalic.TTF, "bolditalic": gobolditalic.TTF, "mono": gomono.TTF} {
		if err = p.AddTTFFontData(name, data); err != nil {
			return err
		}
	}
	w := &pdfWriter{p: p, root: o.Root}
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
		if err = p.Image(o.Cover, (pageW-iw)/2, (pageH-ih)/2, &gopdf.Rect{W: iw, H: ih}); err != nil {
			return err
		}
	}
	// Reserve a measured contents section, then fill in its page numbers after
	// laying out the body. Long titles wrap without colliding with page numbers.
	var entries []tocEntry
	for _, b := range bb {
		if b.Level > 0 && b.Level <= 3 && strings.TrimSpace(text(b.Runs)) != "" {
			entries = append(entries, tocEntry{title: text(b.Runs), id: b.ID, level: b.Level})
		}
	}
	tocStart, tocPages := 0, 0
	if o.TOC && len(entries) > 0 {
		w.newPage()
		tocStart = w.page
		if err = w.paragraph([]Run{{Text: "Inhalt", Bold: true}}, 22, margin, pageW-2*margin); err != nil {
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
		if err = w.paragraph([]Run{{Text: o.Title, Bold: true}}, 24, margin, pageW-2*margin); err != nil {
			return err
		}
		w.y += 12
	}
	for _, b := range bb {
		if b.Break {
			w.newPage()
			continue
		}
		if len(b.Rows) > 0 {
			if err = w.table(b); err != nil {
				return err
			}
			continue
		}
		size := 11.0
		left := margin + float64(b.Indent)*15
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
				b.Runs[i].Bold = true
			}
		}
		if b.Code {
			size = 9
		}
		if err = w.paragraph(b.Runs, size, left, pageW-margin-left); err != nil {
			return err
		}
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
			if err = p.SetFont("regular", "", 10); err != nil {
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
		p.SetFont("regular", "", 9)
		p.SetXY(pageW-margin-25, pageH-28)
		if err = p.Cell(nil, fmt.Sprint(page)); err != nil {
			return err
		}
	}
	return p.WritePdf(path)
}
func (w *pdfWriter) newPage() { w.p.AddPage(); w.page++; w.y = margin }
func (w *pdfWriter) ensure(h float64) {
	if w.y+h > pageH-margin {
		w.newPage()
	}
}
func (w *pdfWriter) font(r Run, size float64) error {
	f := "regular"
	if r.Bold && r.Italic {
		f = "bolditalic"
	} else if r.Bold {
		f = "bold"
	} else if r.Italic {
		f = "italic"
	}
	if r.Code {
		f = "mono"
	}
	return w.p.SetFont(f, "", size)
}

func (w *pdfWriter) paragraph(runs []Run, size, left, width float64) error {
	var line []piece
	used := 0.0
	lh := size * 1.35
	flush := func() error {
		if len(line) == 0 {
			return nil
		}
		w.ensure(lh)
		x := left
		for _, s := range line {
			if err := w.font(s.Run, size); err != nil {
				return err
			}
			w.p.SetXY(x, w.y)
			w.p.SetTextColor(30, 35, 42)
			if s.Link != "" {
				w.p.SetTextColor(24, 84, 139)
			}
			if err := w.p.Cell(nil, s.Text); err != nil {
				return err
			}
			if strings.HasPrefix(s.Link, "#") {
				w.p.AddInternalLink(strings.TrimPrefix(s.Link, "#"), x, w.y, s.width, lh)
			} else if s.Link != "" {
				w.p.AddExternalLink(s.Link, x, w.y, s.width, lh)
			}
			x += s.width
		}
		w.y += lh
		line = nil
		used = 0
		return nil
	}
	for _, r := range runs {
		if r.Image != "" {
			if err := flush(); err != nil {
				return err
			}
			path, err := resource(w.root, r.Image)
			if err != nil {
				return err
			}
			c, err := imageSize(path)
			if err != nil {
				return fmt.Errorf("image %s: %w", r.Image, err)
			}
			scale := math.Min(width/float64(c.Width), (pageH-2*margin-20)/float64(c.Height))
			scale = math.Min(scale, 0.75)
			if desired := imageWidth(r.ImageWidth, width); desired > 0 {
				scale = math.Min(desired/float64(c.Width), math.Min(width/float64(c.Width), (pageH-2*margin-20)/float64(c.Height)))
			}
			iw, ih := float64(c.Width)*scale, float64(c.Height)*scale
			w.ensure(ih + 10)
			if err = w.p.Image(path, left, w.y, &gopdf.Rect{W: iw, H: ih}); err != nil {
				return err
			}
			w.y += ih + 10
			continue
		}
		if err := w.font(r, size); err != nil {
			return err
		}
		// Tokens preserve spaces and explicit breaks; oversized words are split
		// by rune, so long URLs and identifiers cannot escape the page.
		var tokens []string
		var word strings.Builder
		for _, c := range r.Text {
			if unicode.IsSpace(c) {
				if word.Len() > 0 {
					tokens = append(tokens, word.String())
					word.Reset()
				}
				tokens = append(tokens, string(c))
			} else {
				word.WriteRune(c)
			}
		}
		if word.Len() > 0 {
			tokens = append(tokens, word.String())
		}
		for _, token := range tokens {
			if token == "\n" {
				if len(line) == 0 {
					w.ensure(lh)
					w.y += lh
				} else if err := flush(); err != nil {
					return err
				}
				continue
			}
			if err := w.font(r, size); err != nil {
				return err
			}
			tw, err := w.p.MeasureTextWidth(token)
			if err != nil {
				return err
			}
			if used+tw > width && len(line) > 0 {
				if err = flush(); err != nil {
					return err
				}
			}
			if strings.TrimSpace(token) == "" && len(line) == 0 {
				continue
			}
			if tw > width {
				for _, c := range token {
					s := r
					s.Text = string(c)
					w.font(r, size)
					cw, err := w.p.MeasureTextWidth(s.Text)
					if err != nil {
						return err
					}
					if used+cw > width {
						if err = flush(); err != nil {
							return err
						}
					}
					line = append(line, piece{s, cw})
					used += cw
				}
				continue
			}
			s := r
			s.Text = token
			line = append(line, piece{s, tw})
			used += tw
		}
	}
	if err := flush(); err != nil {
		return err
	}
	w.y += 7
	return nil
}

func (w *pdfWriter) table(b Block) error {
	n := len(b.Rows[0])
	if n == 0 {
		return nil
	}
	width := pageW - 2*margin
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
	for rowIndex, row := range b.Rows {
		if len(row) != n {
			return fmt.Errorf("inconsistent PDF table column count")
		}
		lines := make([][]string, n)
		maxLines := 0
		for i, s := range row {
			r := Run{Bold: rowIndex == 0}
			w.font(r, 9)
			for _, part := range strings.Split(s, "\n") {
				if part == "" {
					lines[i] = append(lines[i], "")
					continue
				}
				ll, err := w.p.SplitTextWithWordWrap(part, widths[i]-10)
				if err != nil {
					return err
				}
				lines[i] = append(lines[i], ll...)
			}
			maxLines = max(maxLines, len(lines[i]))
		}
		// Split tall rows across pages rather than clipping their contents.
		for offset := 0; offset < maxLines; {
			w.ensure(22)
			count := min(maxLines-offset, int((pageH-margin-w.y-8)/12))
			if count < 1 {
				w.newPage()
				continue
			}
			h := float64(count)*12 + 8
			x := margin
			for i, ll := range lines {
				w.p.SetStrokeColor(175, 185, 195)
				w.p.SetLineWidth(.4)
				w.p.RectFromUpperLeftWithStyle(x, w.y, widths[i], h, "D")
				for j := 0; j < count && offset+j < len(ll); j++ {
					w.font(Run{Bold: rowIndex == 0}, 9)
					w.p.SetTextColor(30, 35, 42)
					w.p.SetXY(x+5, w.y+4+float64(j)*12)
					if ll[offset+j] == "" {
						continue
					}
					if err := w.p.Cell(nil, ll[offset+j]); err != nil {
						return err
					}
				}
				x += widths[i]
			}
			w.y += h
			offset += count
			if offset < maxLines {
				w.newPage()
			}
		}
	}
	w.y += 10
	return nil
}

func imageWidth(value string, available float64) float64 {
	for _, unit := range []struct {
		suffix string
		factor float64
	}{{"cm", 72 / 2.54}, {"mm", 72 / 25.4}, {"in", 72}, {"pt", 1}, {"px", .75}, {"%", available / 100}} {
		if strings.HasSuffix(value, unit.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(value, unit.suffix), 64)
			if err == nil {
				return n * unit.factor
			}
			return 0
		}
	}
	return 0
}
