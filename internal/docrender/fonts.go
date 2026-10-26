package docrender

import (
	_ "embed"
	"sync"

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

// fontData holds every font the PDF embeds. Text fonts come first; runes a
// text font lacks fall back to "emoji".
var fontData = map[string][]byte{
	"regular": goregular.TTF, "bold": gobold.TTF, "italic": goitalic.TTF,
	"bolditalic": gobolditalic.TTF, "mono": gomono.TTF, "emoji": notoEmoji,
}

var parsedFonts = sync.OnceValues(func() (map[string]*sfnt.Font, error) {
	out := map[string]*sfnt.Font{}
	for name, data := range fontData {
		f, err := sfnt.Parse(data)
		if err != nil {
			return nil, err
		}
		out[name] = f
	}
	return out, nil
})

// invisible reports runes that only steer emoji presentation: variation
// selectors, the zero-width joiner, skin tone modifiers and tag characters.
// Without text shaping they cannot combine, so they are dropped and a
// sequence such as a family renders as its single members.
func invisible(r rune) bool {
	return r == 0xFE0E || r == 0xFE0F || r == 0x200D ||
		(r >= 0x1F3FB && r <= 0x1F3FF) || (r >= 0xE0020 && r <= 0xE007F)
}

type segment struct{ font, text string }

// segments splits text into runs of one font each: font where it has a
// glyph, otherwise the emoji font where that has one.
func segments(text, font string) ([]segment, error) {
	fonts, err := parsedFonts()
	if err != nil {
		return nil, err
	}
	var b sfnt.Buffer
	var out []segment
	for _, r := range text {
		if invisible(r) {
			continue
		}
		f := font
		if g, _ := fonts[font].GlyphIndex(&b, r); g == 0 {
			if g, _ = fonts["emoji"].GlyphIndex(&b, r); g != 0 {
				f = "emoji"
			}
		}
		if n := len(out); n > 0 && out[n-1].font == f {
			out[n-1].text += string(r)
		} else {
			out = append(out, segment{f, string(r)})
		}
	}
	return out, nil
}
