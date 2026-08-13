package bookmaker

import (
	"path/filepath"
	"strings"
	"testing"
)

const (
	testProject = "../../testdata/project"
	testBook    = testProject + "/book"
)

func loadTestTree(t *testing.T) *Node {
	t.Helper()
	root, err := LoadTree(testBook, testProject)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadTreeOrderAndLevels(t *testing.T) {
	root := loadTestTree(t)

	type entry struct {
		rel   string
		level int
	}
	var got []entry
	root.Walk(func(n *Node) { got = append(got, entry{n.Rel, n.Level}) })

	want := []entry{
		{"book", 1},
		// Siblings sort by `order:`; a directory takes the order of its
		// index.qmd, which is Quarto's "index sorts at the parent level".
		{"book/erster", 1},
		{"book/erster/alpha", 2},
		{"book/erster/alpha/tief", 3},
		{"book/erster/beta", 2},
		{"book/zweiter", 1},
		{"book/dritter_FW", 1},
		{"book/dritter_FW/seite", 2},
	}

	if len(got) != len(want) {
		t.Fatalf("visited %d nodes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("node %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLoadTreeSkipsHiddenAndUnderscore(t *testing.T) {
	root := loadTestTree(t)
	root.Walk(func(n *Node) {
		if strings.Contains(n.Rel, "_entwurf") || strings.Contains(n.Rel, ".hidden") {
			t.Errorf("%s should have been skipped", n.Rel)
		}
	})
}

func TestLoadTreePrunesContentFreeDirectories(t *testing.T) {
	root := loadTestTree(t)
	root.Walk(func(n *Node) {
		if filepath.Base(n.Rel) == "images" {
			t.Errorf("media directory %s should have been pruned", n.Rel)
		}
	})
}

func TestLoadTreeAudience(t *testing.T) {
	root := loadTestTree(t)
	want := map[string]string{
		"book":                  "",
		"book/erster":           "",
		"book/dritter_FW":       "fw",
		"book/dritter_FW/seite": "",
	}
	root.Walk(func(n *Node) {
		if w, ok := want[n.Rel]; ok && n.Audience != w {
			t.Errorf("%s: audience = %q, want %q", n.Rel, n.Audience, w)
		}
	})
}

func TestLoadTreeRejectsFiles(t *testing.T) {
	if _, err := LoadTree(testBook+"/index.qmd", testProject); err == nil {
		t.Fatal("expected an error for a file argument")
	}
}

func TestFindProjectRoot(t *testing.T) {
	abs, err := filepath.Abs(testProject)
	if err != nil {
		t.Fatal(err)
	}
	got := FindProjectRoot(testBook + "/erster/alpha")
	if got != abs {
		t.Errorf("FindProjectRoot = %q, want %q", got, abs)
	}
	if !HasProjectConfig(testProject) {
		t.Error("HasProjectConfig should be true for the project root")
	}
	if HasProjectConfig(testBook) {
		t.Error("HasProjectConfig should be false for a content folder")
	}
}

// TestBookFolders checks the folders --all picks up: content folders yes,
// media folders and nested projects no.
func TestBookFolders(t *testing.T) {
	got, err := BookFolders(testProject)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture project holds `book` (content) and `assets` (media only).
	want := []string{filepath.Join(testProject, "book")}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("BookFolders = %v, want %v", got, want)
	}
}

func TestBookFoldersSkipsHiddenAndNestedProjects(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "buch", "index.qmd"), "# Buch\n")
	writeFile(t, filepath.Join(dir, "_entwurf", "index.qmd"), "# Entwurf\n")
	writeFile(t, filepath.Join(dir, ".hidden", "index.qmd"), "# Versteckt\n")
	writeFile(t, filepath.Join(dir, "eigenes", "_quarto.yml"), "project:\n  type: book\n")
	writeFile(t, filepath.Join(dir, "eigenes", "index.qmd"), "# Eigenes\n")
	writeFile(t, filepath.Join(dir, "bilder", "x.png"), "")

	got, err := BookFolders(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "buch")}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("BookFolders = %v, want %v", got, want)
	}
}

func TestHumanise(t *testing.T) {
	tests := []struct{ in, want string }{
		{"einsatz-starten", "Einsatz starten"},
		{"betriebszustaende_FW", "Betriebszustaende"},
		{"bma-uea", "Bma uea"},
		{"wartung_patches_updates", "Wartung patches updates"},
	}
	for _, tc := range tests {
		if got := humanise(tc.in); got != tc.want {
			t.Errorf("humanise(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
