package qmcore

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadBase(t *testing.T) {
	root := t.TempDir()
	b, err := LoadBase(root)
	if err != nil || b == nil || b.Path != "" || b.Lang != "" {
		t.Fatalf("a project without _quarto.yml: got %+v, %v", b, err)
	}
	writeFiles(t, root, map[string]string{
		"_quarto.yml": "lang: de\nmake:\n  audiences: [pol, fw]\nfilters:\n  - ext/gate\n  - own.lua\nproject:\n  type: book\n",
	})
	if b, err = LoadBase(root); err != nil {
		t.Fatal(err)
	}
	if b.Lang != "de" || len(b.Filters) != 2 || b.Make["audiences"] == nil || b.Path != filepath.Join(root, "_quarto.yml") {
		t.Errorf("base = %+v", b)
	}
	writeFiles(t, root, map[string]string{"_quarto.yml": "lang: [unterminated"})
	if _, err := LoadBase(root); err == nil {
		t.Error("a broken _quarto.yml was accepted")
	}
}

func TestMergedSelectionConfig(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"_quarto-topic-t.yml":    "lang: fr\nmake:\n  shared: topic\n  only_topic: true\nfilters:\n  - topic.lua\n",
		"_quarto-format-f.yml":   "make:\n  shared: format\n  explanation: true\nfilters:\n  - path: _build/x.lua\n    at: post-finalize\n",
		"_quarto-audience-a.yml": "lang: es\nmake:\n  shared: audience\n  pol: true\nfilters:\n  - audience.lua\n",
	})
	ps, err := LoadSelection(root, Selection{Topic: "t", Format: "f", Audience: "a"})
	if err != nil {
		t.Fatal(err)
	}
	base := &Base{Lang: "de", Make: map[string]any{"shared": "base", "audiences": []any{"pol"}}, Filters: []any{"base.lua"}}

	flags := ps.MakeFlags(base)
	// The first profile listed wins a conflict, the base has the lowest priority.
	want := map[string]any{
		"shared": "topic", "only_topic": true, "explanation": true, "pol": true, "audiences": []any{"pol"},
	}
	if !reflect.DeepEqual(flags, want) {
		t.Errorf("MakeFlags = %v, want %v", flags, want)
	}
	if got := ps.Language(base); got != "fr" {
		t.Errorf("Language = %q, want fr (the first profile that sets one)", got)
	}
	if got := (&Profiles{Topic: &Profile{}, Format: &Profile{}, Audience: &Profile{}}).Language(base); got != "de" {
		t.Errorf("Language = %q, want the base's de", got)
	}
	entries := ps.FilterEntries(base)
	if len(entries) != 4 || entries[0] != "base.lua" || entries[1] != "topic.lua" || entries[3] != "audience.lua" {
		t.Errorf("FilterEntries = %v (base first, then the profiles in axis order)", entries)
	}
	if got := ps.MakeFlags(nil); got["audiences"] != nil || got["shared"] != "topic" {
		t.Errorf("MakeFlags without a base = %v", got)
	}
}

func TestFormatOptionsAreRead(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"_quarto-format-doc.yml":   "format:\n  docx:\n    toc: true\n    reference-doc: ref.docx\n    syntax-highlighting: github\n",
		"_quarto-format-deck.yml":  "format:\n  pptx:\n    slide-level: 2\n    reference-doc: _build/ref.pptx\n    highlight-style: kate\n",
		"_quarto-format-short.yml": "format: html\n",
	})
	doc, err := LoadProfile(root, "format-doc")
	if err != nil {
		t.Fatal(err)
	}
	if o := doc.Format["docx"]; !o.TOC || o.ReferenceDoc != "ref.docx" || o.SyntaxHighlighting != "github" {
		t.Errorf("docx options = %+v", o)
	}
	deck, _ := LoadProfile(root, "format-deck")
	if o := deck.Format["pptx"]; o.SlideLevel != 2 || o.ReferenceDoc != "_build/ref.pptx" || o.HighlightStyle != "kate" {
		t.Errorf("pptx options = %+v", o)
	}
	short, _ := LoadProfile(root, "format-short")
	if _, ok := short.Format["html"]; !ok {
		t.Errorf("the scalar form lost its format: %v", short.Format)
	}
}
