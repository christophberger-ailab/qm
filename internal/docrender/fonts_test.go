package docrender

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
)

func TestBrandFontsReplaceGoFonts(t *testing.T) {
	dir := t.TempDir()
	mono, monoBold := filepath.Join(dir, "mono.ttf"), filepath.Join(dir, "monobold.ttf")
	os.WriteFile(mono, gomono.TTF, 0o644)
	os.WriteFile(monoBold, gomonobold.TTF, 0o644)

	fs, err := loadFonts(map[string]string{"regular": mono, "bold": monoBold})
	if err != nil {
		t.Fatal(err)
	}
	if fs["regular"].name != "GoMono" || fs["bold"].name != "GoMono-Bold" {
		t.Errorf("faces %q, %q", fs["regular"].name, fs["bold"].name)
	}
	if fs["heading"] != fs["bold"] {
		t.Error("headings without a font of their own do not use the bold face")
	}
	if fs["italic"].name != "italic" {
		t.Errorf("italic is %q, want the Go font", fs["italic"].name)
	}
	// Emoji still fall back to the emoji font.
	var got []string
	for _, s := range fs.segments("ok 👍", "regular") {
		got = append(got, s.font.name+":"+s.text)
	}
	if strings.Join(got, "|") != "GoMono:ok |emoji:👍" {
		t.Errorf("segments %q", got)
	}

	if _, err := loadFonts(map[string]string{"emoji": mono}); err == nil {
		t.Error("emoji role accepted")
	}
	if _, err := loadFonts(map[string]string{"regular": filepath.Join(dir, "missing.ttf")}); err == nil {
		t.Error("missing font accepted")
	}
}

func TestValidateFontRejectsOtherFormats(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{"cff.otf": "OTTO\x00\x0a", "font.woff2": "wOF2\x00\x01", "empty.ttf": ""} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, []byte(data), 0o644)
		if err := ValidateFont(path); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	path := filepath.Join(dir, "mono.ttf")
	os.WriteFile(path, gomono.TTF, 0o644)
	if err := ValidateFont(path); err != nil {
		t.Error(err)
	}
}

func TestBrandFontsInPDF(t *testing.T) {
	dir := t.TempDir()
	mono := filepath.Join(dir, "mono.ttf")
	os.WriteFile(mono, gomono.TTF, 0o644)
	doc := &Document{Blocks: []Node{
		{T: "Header", C: []byte(`[1,["h",[],[]],[{"t":"Str","c":"Heading"}]]`)},
		{T: "Para", C: []byte(`[{"t":"Str","c":"Text"},{"t":"Space"},{"t":"Str","c":"👍"}]`)},
	}}
	out := filepath.Join(dir, "out.pdf")
	if err := PDF(out, doc, Options{Fonts: map[string]string{"regular": mono, "heading": mono}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "/BaseFont /GoMono") {
		t.Error("brand font not embedded")
	}
}
