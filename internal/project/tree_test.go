package project

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeFixture creates a Quarto project resembling the reference example:
//
//	index.qmd            (order 1)
//	chapter1/index.qmd   (order 2)
//	chapter2/index.qmd   (order 3)
//	chapter2/second.qmd  (order 1)
//	chapter2/third.qmd   (order 2)
//	chapter2/fourth.qmd  (no order)
//	chapter3.qmd         (order 4)
//	chapter3/another.qmd     (order 1)
//	chapter3/yet-another.qmd (order 2)
//	chapter3/deep/index.qmd  (order 3)
//	chapter3/deep/leaf.qmd   (order 1)
func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pages := map[string]string{
		"index.qmd":                "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"chapter1/index.qmd":       "---\ntitle: Chapter 1\norder: 2\n---\n# One\n",
		"chapter2/index.qmd":       "---\ntitle: Chapter 2\norder: 3\n---\n# Two\n",
		"chapter2/second.qmd":      "---\ntitle: Second\norder: 1\n---\n# Second\n\n## Detail\n",
		"chapter2/third.qmd":       "---\ntitle: Third\norder: 2\n---\n# Third\n",
		"chapter2/fourth.qmd":      "---\ntitle: Fourth\n---\n# Fourth\n",
		"chapter3.qmd":             "---\ntitle: Chapter 3\norder: 4\n---\n# Three\n",
		"chapter3/another.qmd":     "---\ntitle: Another\norder: 1\n---\n# Another\n",
		"chapter3/yet-another.qmd": "---\ntitle: Yet Another\norder: 2\n---\n# Yet Another\n",
		"chapter3/deep/index.qmd":  "---\ntitle: Deep\norder: 3\n---\n# Deep\n",
		"chapter3/deep/leaf.qmd":   "---\ntitle: Leaf\norder: 1\n---\n# Leaf\n",
	}
	for name, content := range pages {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	yml := map[string]string{
		"_quarto.yml":              "project:\n  type: book\nbook:\n  title: Test Book # keep\n  chapters:\n    - index.qmd\n",
		"_quarto-chapter2.yml":     "book:\n  chapters:\n    - index.qmd\n",
		"_quarto-chapter3-pol.yml": "book:\n  chapters:\n    - index.qmd\n",
		"_quarto-web.yml":          "format:\n  html: default\n",
	}
	for name, content := range yml {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func paths(pages []*Page) []string {
	var out []string
	for _, p := range pages {
		out = append(out, p.Path)
	}
	return out
}

func TestLoadTree(t *testing.T) {
	tree, err := Load(writeFixture(t))
	if err != nil {
		t.Fatal(err)
	}

	wantRoot := []string{"index.qmd", "chapter1/index.qmd", "chapter2/index.qmd", "chapter3.qmd"}
	if got := paths(tree.Pages); !reflect.DeepEqual(got, wantRoot) {
		t.Fatalf("root pages = %v, want %v", got, wantRoot)
	}

	ch2 := tree.Pages[2]
	wantCh2 := []string{"chapter2/second.qmd", "chapter2/third.qmd", "chapter2/fourth.qmd"}
	if got := paths(ch2.Children); !reflect.DeepEqual(got, wantCh2) {
		t.Fatalf("chapter2 children = %v, want %v", got, wantCh2)
	}
	if ch2.Children[2].Order != nil {
		t.Errorf("fourth.qmd should be unordered")
	}
	if ch2.Children[0].Order == nil || *ch2.Children[0].Order != 1 {
		t.Errorf("second.qmd order = %v, want 1", ch2.Children[0].Order)
	}

	ch3 := tree.Pages[3]
	wantCh3 := []string{"chapter3/another.qmd", "chapter3/yet-another.qmd", "chapter3/deep/index.qmd"}
	if got := paths(ch3.Children); !reflect.DeepEqual(got, wantCh3) {
		t.Fatalf("chapter3 children = %v, want %v", got, wantCh3)
	}
	deep := ch3.Children[2]
	if got := paths(deep.Children); !reflect.DeepEqual(got, []string{"chapter3/deep/leaf.qmd"}) {
		t.Fatalf("deep children = %v", got)
	}

	if tree.Pages[0].Title != "Home" {
		t.Errorf("title = %q, want Home", tree.Pages[0].Title)
	}
}

func TestLoadTitleFallback(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "untitled.qmd"), []byte("body\n"), 0o644)
	tree, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Pages[0].Title; got != "untitled" {
		t.Errorf("fallback title = %q, want %q", got, "untitled")
	}
}

func TestLoadSkipsUnderscoreAndHiddenDirs(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"_book", ".quarto"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
		os.WriteFile(filepath.Join(root, d, "x.qmd"), []byte("x\n"), 0o644)
	}
	os.WriteFile(filepath.Join(root, "real.qmd"), []byte("x\n"), 0o644)
	tree, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(tree.Pages); !reflect.DeepEqual(got, []string{"real.qmd"}) {
		t.Errorf("pages = %v, want [real.qmd]", got)
	}
}

// Profiles lists only profiles that configure a book; _quarto-web.yml has
// no book key and stays hidden.
func TestProfiles(t *testing.T) {
	root := writeFixture(t)
	got, err := Profiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"chapter2", "chapter3-pol"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Profiles() = %v, want %v", got, want)
	}
}

func TestFind(t *testing.T) {
	tree, err := Load(writeFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	p := tree.Find("chapter3/deep/leaf.qmd")
	if p == nil || p.Title != "Leaf" {
		t.Fatalf("Find returned %+v", p)
	}
	if tree.Find("nope.qmd") != nil {
		t.Error("Find(nope.qmd) should be nil")
	}
	if !strings.HasSuffix(tree.Find("chapter3.qmd").Dir, "chapter3") {
		t.Errorf("chapter3.qmd Dir = %q, want chapter3", tree.Find("chapter3.qmd").Dir)
	}
}
