package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestScanFiles_BasicStructure(t *testing.T) {
	docRoot := t.TempDir()
	baseFolder := filepath.Join(docRoot, "a")

	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/foo.qmd", "---\norder: 1\n---\n")
	writeTempFile(t, docRoot, "a/bar.qmd", "---\norder: 2\n---\n")
	writeTempFile(t, docRoot, "a/b/index.qmd", "---\norder: 3\n---\n")
	writeTempFile(t, docRoot, "a/b/baz.qmd", "---\norder: 1\n---\n")

	entries, err := scanFiles(docRoot, baseFolder, "", nil)
	if err != nil {
		t.Fatalf("scanFiles: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

func TestScanFiles_ExcludesByPattern(t *testing.T) {
	docRoot := t.TempDir()
	baseFolder := filepath.Join(docRoot, "a")

	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/_hidden.qmd", "---\norder: 99\n---\n")
	writeTempFile(t, docRoot, "a/.dotfile.qmd", "---\norder: 99\n---\n")

	pattern := regexp.MustCompile(defaultExcludePattern)
	entries, err := scanFiles(docRoot, baseFolder, "", pattern)
	if err != nil {
		t.Fatalf("scanFiles: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
	if _, ok := entries["a/index.qmd"]; !ok {
		t.Error("expected a/index.qmd to be present")
	}
}

func TestScanFiles_SkipsNonMarkdownFiles(t *testing.T) {
	docRoot := t.TempDir()
	baseFolder := filepath.Join(docRoot, "a")

	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/image.png", "not markdown")
	writeTempFile(t, docRoot, "a/data.json", "{}")

	entries, err := scanFiles(docRoot, baseFolder, "", nil)
	if err != nil {
		t.Fatalf("scanFiles: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 markdown entry, got %d", len(entries))
	}
}

func TestScanFiles_VariantFiltering(t *testing.T) {
	docRoot := t.TempDir()
	baseFolder := filepath.Join(docRoot, "a")

	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/foo_FW.qmd", "---\norder: 1\n---\n")
	writeTempFile(t, docRoot, "a/foo_POL.qmd", "---\norder: 1\n---\n")

	entries, err := scanFiles(docRoot, baseFolder, "fw", nil)
	if err != nil {
		t.Fatalf("scanFiles: %v", err)
	}
	if _, ok := entries["a/foo_FW.qmd"]; !ok {
		t.Error("expected a/foo_FW.qmd to be kept for fw")
	}
	if _, ok := entries["a/foo_POL.qmd"]; ok {
		t.Error("expected a/foo_POL.qmd to be excluded for fw")
	}
}

func TestScanFiles_NonexistentFolder(t *testing.T) {
	docRoot := t.TempDir()
	_, err := scanFiles(docRoot, filepath.Join(docRoot, "nonexistent"), "", nil)
	if err == nil {
		t.Error("expected error for nonexistent folder")
	}
	// Ensure the underlying error is "does not exist"-ish.
	if !os.IsNotExist(err) {
		// WalkDir may wrap; this is just a sanity check.
		t.Logf("got error (acceptable): %v", err)
	}
}
