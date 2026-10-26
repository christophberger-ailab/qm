package bookrender

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

const brandYML = `color:
  primary: "#003366"
typography:
  fonts:
    - family: Corporate Sans
      source: file
      files:
        - path: fonts/Sans-Regular.ttf
          weight: 400
        - path: fonts/Sans-Medium.ttf
          weight: medium
        - path: fonts/Sans-Bold.ttf
          weight: bold
        - path: fonts/Sans-Italic.ttf
          style: italic
        - fonts/Sans.woff2
    - family: Corporate Mono
      source: file
      files:
        - fonts/Mono.ttf
    - family: Fancy Display
      source: google
  base: Corporate Sans
  headings:
    family: Corporate Sans
    weight: 500
  monospace: Corporate Mono
`

func brandProject(t *testing.T, yml string) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "fonts"), 0o755)
	for name, data := range map[string][]byte{
		"Sans-Regular.ttf": goregular.TTF, "Sans-Medium.ttf": gomedium.TTF, "Sans-Bold.ttf": gobold.TTF,
		"Sans-Italic.ttf": goitalic.TTF, "Sans.woff2": []byte("wOF2"), "Mono.ttf": gomono.TTF,
	} {
		os.WriteFile(filepath.Join(root, "fonts", name), data, 0o644)
	}
	os.WriteFile(filepath.Join(root, BrandFile), []byte(yml), 0o644)
	return root
}

func TestBrandFonts(t *testing.T) {
	root := brandProject(t, brandYML)
	paths, notes, err := brandFonts(root)
	if err != nil {
		t.Fatal(err)
	}
	font := func(name string) string { return filepath.Join(root, "fonts", name) }
	want := map[string]string{
		"regular": font("Sans-Regular.ttf"), "bold": font("Sans-Bold.ttf"),
		"italic": font("Sans-Italic.ttf"), "bolditalic": font("Sans-Italic.ttf"),
		"heading": font("Sans-Medium.ttf"), "mono": font("Mono.ttf"),
	}
	for role, p := range want {
		if paths[role] != p {
			t.Errorf("%s: %q, want %q", role, paths[role], p)
		}
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "Sans.woff2") {
		t.Errorf("notes %q", notes)
	}

	// Families the PDF cannot embed keep the Go fonts, with a note.
	yml := strings.Replace(brandYML, "  base: Corporate Sans\n", "  base: Fancy Display\n", 1)
	yml = strings.Replace(yml, "    family: Corporate Sans\n", "    family: Unknown\n", 1)
	paths, notes, err = brandFonts(brandProject(t, yml))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths["mono"] == "" {
		t.Errorf("paths %v, want mono only", paths)
	}
	if len(notes) != 3 || !strings.Contains(notes[1], "Fancy Display comes from google") ||
		!strings.Contains(notes[2], "Unknown has no TrueType files") {
		t.Errorf("notes %q", notes)
	}

	if paths, notes, err = brandFonts(t.TempDir()); paths != nil || notes != nil || err != nil {
		t.Errorf("no brand file: %v, %v, %v", paths, notes, err)
	}
	for name, yml := range map[string]string{
		"missing file": strings.Replace(brandYML, "Mono.ttf", "Missing.ttf", 1),
		"outside":      strings.Replace(brandYML, "fonts/Mono.ttf", "../Mono.ttf", 1),
		"weight":       strings.Replace(brandYML, "weight: 500", "weight: heavyish", 1),
		"yaml":         "typography: [",
	} {
		if _, _, err := brandFonts(brandProject(t, yml)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLoadNativeReadsBrandFonts(t *testing.T) {
	root := project(t)
	os.WriteFile(filepath.Join(root, "_quarto-format-handout.yml"), []byte(nativeFormat), 0o644)
	os.MkdirAll(filepath.Join(root, "fonts"), 0o755)
	os.WriteFile(filepath.Join(root, "fonts", "Mono.ttf"), gomono.TTF, 0o644)
	os.WriteFile(filepath.Join(root, BrandFile), []byte("typography:\n  fonts:\n    - family: M\n"+
		"      source: file\n      files: [fonts/Mono.ttf]\n  base: M\n"), 0o644)
	n, err := loadNative(t, root)
	if err != nil {
		t.Fatal(err)
	}
	if n.Fonts["regular"] != filepath.Join(root, "fonts", "Mono.ttf") {
		t.Errorf("fonts %v", n.Fonts)
	}
}
