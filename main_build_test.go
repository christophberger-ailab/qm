package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildFolderChapters_SimpleOrder(t *testing.T) {
	docRoot := t.TempDir()
	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/foo.qmd", "---\norder: 1\n---\n")
	writeTempFile(t, docRoot, "a/bar.qmd", "---\norder: 2\n---\n")

	entries := map[string]*fileEntry{
		"a/index.qmd": {relPath: "a/index.qmd", order: intPtr(0)},
		"a/foo.qmd":   {relPath: "a/foo.qmd", order: intPtr(1)},
		"a/bar.qmd":   {relPath: "a/bar.qmd", order: intPtr(2)},
	}

	got := buildFolderChapters(docRoot, filepath.Join(docRoot, "a"), "a", entries)
	want := []string{"a/index.qmd", "a/foo.qmd", "a/bar.qmd"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildFolderChapters_NestedFolders(t *testing.T) {
	docRoot := t.TempDir()
	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/foo.qmd", "---\norder: 2\n---\n")
	writeTempFile(t, docRoot, "a/b/index.qmd", "---\norder: 1\n---\n")
	writeTempFile(t, docRoot, "a/b/baz.qmd", "---\norder: 1\n---\n")

	entries := map[string]*fileEntry{
		"a/index.qmd":   {relPath: "a/index.qmd", order: intPtr(0)},
		"a/foo.qmd":     {relPath: "a/foo.qmd", order: intPtr(2)},
		"a/b/index.qmd": {relPath: "a/b/index.qmd", order: intPtr(1)},
		"a/b/baz.qmd":   {relPath: "a/b/baz.qmd", order: intPtr(1)},
	}

	got := buildFolderChapters(docRoot, filepath.Join(docRoot, "a"), "a", entries)
	want := []string{
		"a/index.qmd",
		"a/b/index.qmd",
		"a/b/baz.qmd",
		"a/foo.qmd",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildFolderChapters_UnorderedAppendedLast(t *testing.T) {
	docRoot := t.TempDir()
	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/ordered.qmd", "---\norder: 1\n---\n")
	writeTempFile(t, docRoot, "a/noorder.qmd", "---\ntitle: x\n---\n")

	entries := map[string]*fileEntry{
		"a/index.qmd":   {relPath: "a/index.qmd", order: intPtr(0)},
		"a/ordered.qmd": {relPath: "a/ordered.qmd", order: intPtr(1)},
		"a/noorder.qmd": {relPath: "a/noorder.qmd", order: nil},
	}

	got := buildFolderChapters(docRoot, filepath.Join(docRoot, "a"), "a", entries)
	// noorder.qmd should come last; index.qmd and ordered.qmd first.
	if len(got) != 3 {
		t.Fatalf("expected 3 chapters, got %d: %v", len(got), got)
	}
	if got[0] != "a/index.qmd" {
		t.Errorf("expected a/index.qmd first, got %s", got[0])
	}
	if got[1] != "a/ordered.qmd" {
		t.Errorf("expected a/ordered.qmd second, got %s", got[1])
	}
	if got[2] != "a/noorder.qmd" {
		t.Errorf("expected a/noorder.qmd last, got %s", got[2])
	}
}

func TestBuildFolderChapters_IndexMd(t *testing.T) {
	docRoot := t.TempDir()
	writeTempFile(t, docRoot, "a/index.md", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/foo.qmd", "---\norder: 1\n---\n")

	entries := map[string]*fileEntry{
		"a/index.md": {relPath: "a/index.md", order: intPtr(0)},
		"a/foo.qmd":  {relPath: "a/foo.qmd", order: intPtr(1)},
	}

	got := buildFolderChapters(docRoot, filepath.Join(docRoot, "a"), "a", entries)
	want := []string{"a/index.md", "a/foo.qmd"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildFolderChapters_SkipsSubfolderWithoutIndex(t *testing.T) {
	docRoot := t.TempDir()
	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/b/foo.qmd", "---\norder: 1\n---\n")
	// No a/b/index.qmd -> subfolder is skipped.

	entries := map[string]*fileEntry{
		"a/index.qmd": {relPath: "a/index.qmd", order: intPtr(0)},
		"a/b/foo.qmd": {relPath: "a/b/foo.qmd", order: intPtr(1)},
	}

	got := buildFolderChapters(docRoot, filepath.Join(docRoot, "a"), "a", entries)
	want := []string{"a/index.qmd"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
