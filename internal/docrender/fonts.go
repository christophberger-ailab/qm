package docrender

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"os"
	"sync"

	"github.com/signintech/gopdf"
	"github.com/signintech/gopdf/fontmaker/core"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// notoEmoji is the monochrome Noto Emoji font (SIL OFL 1.1, see
// fonts/OFL.txt), instanced to its regular weight. gopdf can embed only
// glyf outlines, so color emoji fonts (CBDT, COLR, sbix) are not an option.
//
//go:embed fonts/NotoEmoji-Regular.ttf
var notoEmoji []byte

// FontRoles are the faces the PDF sets text in. Options.Fonts may replace
// any of them with a TrueType file; the others stay Go fonts. Headings
// without a font of their own use the bold face.
var FontRoles = []string{"regular", "bold", "italic", "bolditalic", "heading", "mono"}

// goFonts are the default faces, by role, and the emoji font that runes a
// text face lacks fall back to.
var goFonts = sync.OnceValues(func() (fontSet, error) {
	fs := fontSet{}
	for name, data := range map[string][]byte{
		"regular": goregular.TTF, "bold": gobold.TTF, "italic": goitalic.TTF,
		"bolditalic": gobolditalic.TTF, "mono": gomono.TTF, "emoji": notoEmoji,
	} {
		f, err := parseFace(name, data)
		if err != nil {
			return nil, err
		}
		fs[name] = f
	}
	fs["heading"] = fs["bold"]
	return fs, nil
})

// face is an embeddable font under the name the PDF registers it with.
type face struct {
	name string
	data []byte
	sfnt *sfnt.Font
	// ascent is the typographic ascender per em: gopdf puts the baseline
	// this far below the top of a text cell.
	ascent float64
}

// fontSet maps each role in FontRoles, and "emoji", to its face.
type fontSet map[string]*face

func parseFace(name string, data []byte) (*face, error) {
	var p core.TTFParser
	if err := p.ParseFontData(data); err != nil {
		return nil, fmt.Errorf("not a TrueType font (%w); the PDF embeds .ttf fonts only, not OpenType CFF (.otf), WOFF or collections", err)
	}
	if _, ok := p.GetTables()["glyf"]; !ok || p.UnitsPerEm() == 0 {
		return nil, errors.New("not a TrueType font with glyf outlines; the PDF embeds .ttf fonts only")
	}
	s, err := sfnt.Parse(data)
	if err != nil {
		return nil, err
	}
	return &face{name: name, data: data, sfnt: s, ascent: float64(p.TypoAscender()) / float64(p.UnitsPerEm())}, nil
}

// ValidateFont reports whether the file at path is a font the PDF can embed.
func ValidateFont(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = parseFace("", data)
	return err
}

// loadFonts returns the Go fonts with the roles in paths replaced by the
// TrueType files there.
func loadFonts(paths map[string]string) (fontSet, error) {
	base, err := goFonts()
	if err != nil || len(paths) == 0 {
		return base, err
	}
	for role := range paths {
		if _, ok := base[role]; !ok || role == "emoji" {
			return nil, fmt.Errorf("unknown font role %q", role)
		}
	}
	fs := maps.Clone(base)
	for _, role := range FontRoles {
		path := paths[role]
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s font: %w", role, err)
		}
		f, err := parseFace("", data)
		if err != nil {
			return nil, fmt.Errorf("%s font %s: %w", role, path, err)
		}
		// The PDF names a face by its PostScript name, unless another face
		// has that name already.
		f.name, _ = f.sfnt.Name(nil, sfnt.NameIDPostScript)
		for _, other := range fs {
			if f.name == "" || other.name == f.name && !bytes.Equal(other.data, f.data) {
				f.name = "brand-" + role
				break
			}
		}
		fs[role] = f
	}
	if paths["heading"] == "" {
		fs["heading"] = fs["bold"]
	}
	return fs, nil
}

// register adds every face of fs to p once.
func (fs fontSet) register(p *gopdf.GoPdf) error {
	done := map[string]bool{}
	for _, f := range fs {
		if done[f.name] {
			continue
		}
		done[f.name] = true
		if err := p.AddTTFFontData(f.name, f.data); err != nil {
			return err
		}
	}
	return nil
}

// invisible reports runes that only steer emoji presentation: variation
// selectors, the zero-width joiner, skin tone modifiers and tag characters.
// Without text shaping they cannot combine, so they are dropped and a
// sequence such as a family renders as its single members.
func invisible(r rune) bool {
	return r == 0xFE0E || r == 0xFE0F || r == 0x200D ||
		(r >= 0x1F3FB && r <= 0x1F3FF) || (r >= 0xE0020 && r <= 0xE007F)
}

type segment struct {
	font *face
	text string
}

// segments splits text into runs of one face each: role's face where it
// has a glyph, otherwise the emoji font where that has one.
func (fs fontSet) segments(text, role string) []segment {
	var b sfnt.Buffer
	var out []segment
	for _, r := range text {
		if invisible(r) {
			continue
		}
		f := fs[role]
		if g, _ := f.sfnt.GlyphIndex(&b, r); g == 0 {
			if g, _ = fs["emoji"].sfnt.GlyphIndex(&b, r); g != 0 {
				f = fs["emoji"]
			}
		}
		if n := len(out); n > 0 && out[n-1].font == f {
			out[n-1].text += string(r)
		} else {
			out = append(out, segment{f, string(r)})
		}
	}
	return out
}
