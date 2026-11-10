package docx

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // register decoders for image.DecodeConfig
	_ "image/jpeg" //
	_ "image/png"  //
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"

	"github.com/christophberger-ailab/qm/internal/markdown"
)

const (
	emuPerPx    = 9525 // 96 dpi
	emuPerTwip  = 635
	emuPerInch  = 914400
	emuPerCm    = 360000
	emuPerMm    = 36000
	emuPerPoint = 12700
)

// image writes an inline picture. SVG, remote and undecodable pictures
// become their alternative text; a local file that cannot be read is an
// error.
func (w *writer) image(buf *bytes.Buffer, n *ast.Image, rp runProps, c ctx) {
	alt := markdown.PlainText(n, w.src)
	placeholder := func() {
		if alt == "" {
			alt = "image"
		}
		ip := rp
		ip.italic = true
		buf.WriteString(w.run(ip, "["+alt+"]"))
	}
	dest := w.dest(n.Destination)
	lower := strings.ToLower(dest)
	if c.note || dest == "" || strings.Contains(lower, "://") || strings.HasPrefix(lower, "data:") ||
		strings.HasSuffix(lower, ".svg") {
		placeholder()
		return
	}
	data, err := os.ReadFile(w.imagePath(dest))
	if err != nil {
		w.fail(fmt.Errorf("docx: image %q: %w", dest, err))
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		placeholder() // SVG in disguise, WebP, ...
		return
	}
	ext := format // png, jpeg or gif
	w.imgTypes[ext] = "image/" + format

	cx, cy := w.imageSize(markdown.AttrsOf(n), int64(cfg.Width)*emuPerPx, int64(cfg.Height)*emuPerPx)
	w.drawID++
	name := fmt.Sprintf("qm-image%d.%s", w.drawID, ext)
	w.media = append(w.media, mediaFile{name: name, data: data})
	rid := fmt.Sprintf("rIdQmImg%d", w.drawID)
	w.rels = append(w.rels, relationship{rid, relImage, "media/" + name})

	fmt.Fprintf(buf, `<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0">`+
		`<wp:extent cx="%[1]d" cy="%[2]d"/><wp:docPr id="%[3]d" name="Picture %[3]d" descr="%[4]s"/>`+
		`<wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr>`+
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic><pic:nvPicPr><pic:cNvPr id="%[3]d" name="%[5]s" descr="%[4]s"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="%[6]s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%[1]d" cy="%[2]d"/></a:xfrm>`+
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic>`+
		`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, w.drawID, esc(alt), esc(filepath.Base(dest)), rid)
}

// imagePath resolves an image reference against the project root. A query
// or fragment and percent-escapes are tried only when the name as written
// is not a file.
func (w *writer) imagePath(dest string) string {
	cands := []string{dest}
	if i := strings.IndexAny(dest, "?#"); i >= 0 {
		cands = append(cands, dest[:i])
	}
	for _, c := range cands[:len(cands):len(cands)] {
		if u, err := url.PathUnescape(c); err == nil && u != c {
			cands = append(cands, u)
		}
	}
	for _, c := range cands {
		if _, err := os.Stat(w.join(c)); err == nil {
			return w.join(c)
		}
	}
	return w.join(dest)
}

func (w *writer) join(dest string) string {
	if strings.HasPrefix(dest, "/") && w.opt.Root == "" {
		return filepath.FromSlash(dest)
	}
	return filepath.Join(w.opt.Root, filepath.FromSlash(dest))
}

// imageSize applies the width and height attributes to the natural size
// (in EMU), and caps the result at the text width.
func (w *writer) imageSize(a markdown.Attrs, nw, nh int64) (int64, int64) {
	full := int64(w.textW) * emuPerTwip
	cw, ch := nw, nh
	wv, hasW := a.Get("width")
	hv, hasH := a.Get("height")
	width, okW := int64(0), false
	height, okH := int64(0), false
	if hasW {
		width, okW = dimension(wv, full)
	}
	if hasH {
		height, okH = dimension(hv, full)
	}
	switch {
	case okW && okH:
		cw, ch = width, height
	case okW:
		cw, ch = width, nh*width/nw
	case okH:
		cw, ch = nw*height/nh, height
	}
	if cw > full {
		ch = ch * full / cw
		cw = full
	}
	return max(cw, 1), max(ch, 1)
}

// dimension parses 50%, 300px, 8cm, 3in, 20mm, 12pt or a bare number
// (pixels) to EMU; a percentage is relative to full.
func dimension(v string, full int64) (int64, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	units := []struct {
		suffix string
		emu    float64
	}{{"%", float64(full) / 100}, {"px", emuPerPx}, {"cm", emuPerCm}, {"mm", emuPerMm}, {"in", emuPerInch}, {"pt", emuPerPoint}, {"", emuPerPx}}
	for _, u := range units {
		if !strings.HasSuffix(v, u.suffix) {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSuffix(v, u.suffix), 64)
		if err != nil || f <= 0 {
			return 0, false
		}
		return int64(f * u.emu), true
	}
	return 0, false
}
