package flatten

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.qmd":         "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"book/index.qmd":    "---\ntitle: Book\norder: 1\n---\n# Book\n",
		"book/sub/page.qmd": "---\ntitle: Page\norder: 2\n---\n# Page\n\nVersion {{< var version >}}.\n",
		"anderes/index.qmd": "---\ntitle: Other\norder: 1\n---\n# Other\n",
		"qm.yml":            "vars: {version: \"1.2\"}\ntopics:\n  book:\n  other: {folder: anderes}\n",
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

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRunWritesTheFlattenedTopic(t *testing.T) {
	root := fixture(t)
	if err := Run(root, "book"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := read(t, filepath.Join(root, BuildDir, "book.md"))
	for _, want := range []string{"# Book", "## Page", "Version 1.2."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

// The content folder is the topic's `folder:` when it differs from the
// topic name.
func TestRunFollowsTheTopicsFolder(t *testing.T) {
	root := fixture(t)
	if err := Run(root, "other"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, BuildDir, "other.md")); !strings.Contains(got, "# Other") {
		t.Errorf("wrong folder flattened:\n%s", got)
	}
}

func TestRunRejectsUnknownTopic(t *testing.T) {
	root := fixture(t)
	if err := Run(root, "nope"); err == nil {
		t.Error("unknown topic accepted")
	}
	if _, err := os.Stat(filepath.Join(root, BuildDir)); !os.IsNotExist(err) {
		t.Error("something was written for an unknown topic")
	}
}
