package remove

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRun_RemovesFileWithConfirmation(t *testing.T) {
	doc := t.TempDir()
	write(t, doc, "f/index.qmd", "---\norder: 0\n---\n")
	write(t, doc, "f/a.qmd", "---\norder: 1\ntitle: A\n---\n")
	write(t, doc, "f/b.qmd", "---\norder: 2\ntitle: B\n---\n")

	var out bytes.Buffer
	if err := Run(doc, filepath.Join(doc, "f"), 1, strings.NewReader("y\n"), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(doc, "f/a.qmd")); !os.IsNotExist(err) {
		t.Errorf("expected a.qmd removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(doc, "f/b.qmd")); err != nil {
		t.Errorf("b.qmd should still exist: %v", err)
	}
	if !strings.Contains(out.String(), "title: A") {
		t.Errorf("expected title preview in output, got: %s", out.String())
	}
}

func TestRun_AbortedOnNo(t *testing.T) {
	doc := t.TempDir()
	write(t, doc, "f/index.qmd", "---\norder: 0\n---\n")
	write(t, doc, "f/a.qmd", "---\norder: 1\ntitle: A\n---\n")

	var out bytes.Buffer
	if err := Run(doc, filepath.Join(doc, "f"), 1, strings.NewReader("n\n"), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(doc, "f/a.qmd")); err != nil {
		t.Errorf("a.qmd should still exist (aborted): %v", err)
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Errorf("expected Aborted message, got: %s", out.String())
	}
}

func TestRun_RemovesSubfolderForDirectoryChapter(t *testing.T) {
	doc := t.TempDir()
	write(t, doc, "f/index.qmd", "---\norder: 0\n---\n")
	write(t, doc, "f/sub/index.qmd", "---\norder: 1\ntitle: Sub\n---\n")
	write(t, doc, "f/sub/extra.qmd", "---\norder: 1\n---\n")

	var out bytes.Buffer
	if err := Run(doc, filepath.Join(doc, "f"), 1, strings.NewReader("y\n"), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(doc, "f/sub")); !os.IsNotExist(err) {
		t.Errorf("expected sub/ directory removed, stat err = %v", err)
	}
}

func TestRun_MissingChapter(t *testing.T) {
	doc := t.TempDir()
	write(t, doc, "f/index.qmd", "---\norder: 0\n---\n")
	var out bytes.Buffer
	err := Run(doc, filepath.Join(doc, "f"), 9, strings.NewReader("y\n"), &out)
	if err == nil || !strings.Contains(err.Error(), "no chapter at order 9") {
		t.Errorf("expected missing-chapter error, got %v", err)
	}
}
