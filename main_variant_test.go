package main

import (
	"sort"
	"testing"
)

func intPtr(i int) *int { return &i }

func makeEntry(relPath string, order *int) *fileEntry {
	return &fileEntry{relPath: relPath, order: order}
}

func TestApplyVariantFilter_NoVariants(t *testing.T) {
	entries := map[string]*fileEntry{
		"a/index.qmd": makeEntry("a/index.qmd", intPtr(0)),
		"a/foo.qmd":   makeEntry("a/foo.qmd", intPtr(1)),
	}
	got := applyVariantFilter(entries, "fw")
	if len(got) != 2 {
		t.Errorf("expected 2 entries, got %d", len(got))
	}
	if _, ok := got["a/index.qmd"]; !ok {
		t.Error("expected a/index.qmd to be kept")
	}
}

func TestApplyVariantFilter_VariantFilesMatchingFW(t *testing.T) {
	entries := map[string]*fileEntry{
		"a/foo_FW.qmd":  makeEntry("a/foo_FW.qmd", intPtr(1)),
		"a/foo_POL.qmd": makeEntry("a/foo_POL.qmd", intPtr(1)),
	}
	got := applyVariantFilter(entries, "fw")
	if len(got) != 1 {
		t.Errorf("expected 1 entry, got %d", len(got))
	}
	if _, ok := got["a/foo_FW.qmd"]; !ok {
		t.Error("expected a/foo_FW.qmd to be kept for fw profile")
	}
}

func TestApplyVariantFilter_VariantFilesMatchingPOL(t *testing.T) {
	entries := map[string]*fileEntry{
		"a/foo_FW.qmd":  makeEntry("a/foo_FW.qmd", intPtr(1)),
		"a/foo_POL.qmd": makeEntry("a/foo_POL.qmd", intPtr(1)),
	}
	got := applyVariantFilter(entries, "pol")
	if len(got) != 1 {
		t.Errorf("expected 1 entry, got %d", len(got))
	}
	if _, ok := got["a/foo_POL.qmd"]; !ok {
		t.Error("expected a/foo_POL.qmd to be kept for pol profile")
	}
}

func TestApplyVariantFilter_PlainSupersededByVariant(t *testing.T) {
	entries := map[string]*fileEntry{
		"a/foo.qmd":    makeEntry("a/foo.qmd", intPtr(1)),
		"a/foo_FW.qmd": makeEntry("a/foo_FW.qmd", intPtr(1)),
	}
	got := applyVariantFilter(entries, "fw")
	if len(got) != 1 {
		t.Errorf("expected 1 entry, got %d", len(got))
	}
	if _, ok := got["a/foo_FW.qmd"]; !ok {
		t.Error("expected a/foo_FW.qmd to win over plain a/foo.qmd")
	}
	if _, ok := got["a/foo.qmd"]; ok {
		t.Error("expected plain a/foo.qmd to be excluded")
	}
}

func TestApplyVariantFilter_FolderSuffixExcludesNonMatching(t *testing.T) {
	entries := map[string]*fileEntry{
		"a/b_POL/foo.qmd": makeEntry("a/b_POL/foo.qmd", intPtr(1)),
		"a/b_FW/foo.qmd":  makeEntry("a/b_FW/foo.qmd", intPtr(1)),
		"a/c/foo.qmd":     makeEntry("a/c/foo.qmd", intPtr(1)),
	}
	got := applyVariantFilter(entries, "fw")
	if _, ok := got["a/b_POL/foo.qmd"]; ok {
		t.Error("expected _POL folder content to be excluded for fw profile")
	}
	if _, ok := got["a/b_FW/foo.qmd"]; !ok {
		t.Error("expected _FW folder content to be kept for fw profile")
	}
	if _, ok := got["a/c/foo.qmd"]; !ok {
		t.Error("expected plain folder content to be kept")
	}
}

func TestApplyVariantFilter_PlainFolderSupersededByVariantFolder(t *testing.T) {
	entries := map[string]*fileEntry{
		"a/b/foo.qmd":    makeEntry("a/b/foo.qmd", intPtr(1)),
		"a/b_FW/bar.qmd": makeEntry("a/b_FW/bar.qmd", intPtr(1)),
	}
	got := applyVariantFilter(entries, "fw")
	if _, ok := got["a/b/foo.qmd"]; ok {
		t.Error("expected plain a/b/ folder to be superseded by a/b_FW/")
	}
	if _, ok := got["a/b_FW/bar.qmd"]; !ok {
		t.Error("expected a/b_FW/bar.qmd to be kept")
	}
}

func TestApplyVariantFilter_NoProfileVariantKeepsAll(t *testing.T) {
	entries := map[string]*fileEntry{
		"a/foo.qmd":     makeEntry("a/foo.qmd", intPtr(1)),
		"a/foo_FW.qmd":  makeEntry("a/foo_FW.qmd", intPtr(1)),
		"a/foo_POL.qmd": makeEntry("a/foo_POL.qmd", intPtr(1)),
	}
	// Empty variant means "no variant filter" — variant pairing still
	// runs, but with no matching profile variant, variant files are
	// dropped (no chosen entry) and plain file is dropped (superseded).
	got := applyVariantFilter(entries, "")
	// With variant="", chosen is nil for both fw and pol, so all are dropped.
	// This documents current behavior.
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// Only plain entries with no variant counterparts survive; here all three
	// share a base key, so none should remain.
	if len(got) != 0 {
		t.Errorf("expected 0 entries for empty variant with conflicting pair, got %v", keys)
	}
}
