package flatten

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christophberger-ailab/qm/internal/bookrender"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.qmd":               "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"book/index.qmd":          "---\ntitle: Book\norder: 1\n---\n# Book\n",
		"book/page.qmd":           "---\ntitle: Page\norder: 2\n---\n# Page\n\n::: slide\n## Deck\n:::\n",
		"anderes/index.qmd":       "---\ntitle: Other\norder: 1\n---\n# Other\n",
		"_quarto.yml":             "project:\n  type: website\n",
		"_quarto-topic-book.yml":  "book:\n  title: Book\n",
		"_quarto-topic-other.yml": "book:\n  title: Other\nqm:\n  folder: anderes\n",
		"_quarto-topic-none.yml":  "{}\n",
		"_quarto-topic-all.yml":   "{}\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRunWritesTheBuildDocumentsAndExits(t *testing.T) {
	root := fixture(t)
	if err := Run(root, "book"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, p := range []string{bookrender.BookFile(root), bookrender.SlidesFile(root)} {
		content, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if len(content) == 0 {
			t.Errorf("%s is empty", p)
		}
	}
	if !strings.Contains(read(t, bookrender.BookFile(root)), "Book") {
		t.Error("the flattened topic is not in the book document")
	}
}

// The content folder is the topic profile's `qm: folder:` when it differs
// from the topic name — which is what lets an output file be called
// `auswertung_ermittlungsunterstuetzung` while the folder is `auswertung`.
func TestRunFollowsTheProfilesFolder(t *testing.T) {
	root := fixture(t)
	if err := Run(root, "other"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(read(t, bookrender.BookFile(root)), "Other") {
		t.Error("qm: folder: was not followed")
	}
}

// topic-all and topic-none are the website's topic: they select no folder,
// and the build documents have to end up contributing nothing. Neither is
// looked up as a content folder — there is no `all/` directory.
func TestRunWithoutTopicEmptiesTheDocuments(t *testing.T) {
	for _, topic := range []string{"all", "none"} {
		t.Run(topic, func(t *testing.T) {
			root := fixture(t)
			if err := Run(root, "book"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if err := Run(root, topic); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if strings.Contains(read(t, bookrender.BookFile(root)), "Book") {
				t.Error("the previous topic is still in the book document")
			}
		})
	}
}

func TestRunRejectsUnknownTopic(t *testing.T) {
	root := fixture(t)
	err := Run(root, "missing")
	if err == nil || !strings.Contains(err.Error(), "no profile file") {
		t.Fatalf("Run error = %v, want a missing-profile error", err)
	}
	if _, err := os.Stat(bookrender.BookFile(root)); !os.IsNotExist(err) {
		t.Error("a build document was written for an unknown topic")
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
