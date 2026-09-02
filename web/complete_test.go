package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// completeEntries decodes a /complete response into the entries it lists.
func completeEntries(t *testing.T, rec interface {
	Result() *http.Response
}) []completeEntry {
	t.Helper()
	var out struct {
		Entries []completeEntry `json:"entries"`
	}
	resp := rec.Result()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode /complete response: %v", err)
	}
	return out.Entries
}

func completeNames(entries []completeEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

func TestCompleteListsPagesAtRoot(t *testing.T) {
	srv, _ := testServer(t)
	rec := get(t, srv, "/complete?dir=.&kind=page")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	entries := completeEntries(t, rec)
	want := map[string]bool{"index.qmd": false, "chapter2": true}
	if len(entries) != len(want) {
		t.Fatalf("entries = %v, want keys of %v", entries, want)
	}
	for _, e := range entries {
		dir, ok := want[e.Name]
		if !ok {
			t.Errorf("unexpected entry %q", e.Name)
			continue
		}
		if e.Dir != dir {
			t.Errorf("%q: Dir = %v, want %v", e.Name, e.Dir, dir)
		}
	}
}

func TestCompleteSkipsUnderscoreAndDotEntries(t *testing.T) {
	srv, root := testServer(t)
	os.MkdirAll(filepath.Join(root, "_drafts"), 0o755)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, "_notes.qmd"), []byte("# notes"), 0o644)

	entries := completeEntries(t, get(t, srv, "/complete?dir=.&kind=page"))
	for _, e := range entries {
		if e.Name == "_drafts" || e.Name == ".git" || e.Name == "_notes.qmd" {
			t.Errorf("entry %q should have been skipped", e.Name)
		}
	}
}

func TestCompleteFiltersByKind(t *testing.T) {
	srv, root := testServer(t)
	writePNG(t, root, "bord.png")
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644)

	images := completeNames(completeEntries(t, get(t, srv, "/complete?dir=.&kind=image")))
	if !contains(images, "bord.png") {
		t.Errorf("images = %v, want bord.png", images)
	}
	if contains(images, "index.qmd") || contains(images, "notes.txt") {
		t.Errorf("images = %v, want only image files (plus directories)", images)
	}

	pages := completeNames(completeEntries(t, get(t, srv, "/complete?dir=.&kind=page")))
	if !contains(pages, "index.qmd") {
		t.Errorf("pages = %v, want index.qmd", pages)
	}
	if contains(pages, "bord.png") || contains(pages, "notes.txt") {
		t.Errorf("pages = %v, want only .qmd files (plus directories)", pages)
	}
}

func TestCompleteDirectoriesAppearForBothKinds(t *testing.T) {
	srv, _ := testServer(t)
	for _, kind := range []string{"image", "page"} {
		entries := completeEntries(t, get(t, srv, "/complete?dir=.&kind="+kind))
		if !contains(completeNames(entries), "chapter2") {
			t.Errorf("kind=%s: entries = %v, want chapter2", kind, entries)
		}
	}
}

func TestCompleteListsNestedDirectory(t *testing.T) {
	srv, _ := testServer(t)
	entries := completeEntries(t, get(t, srv, "/complete?dir=chapter2&kind=page"))
	names := completeNames(entries)
	for _, want := range []string{"index.qmd", "second.qmd", "third.qmd", "loose.qmd", "broken.qmd"} {
		if !contains(names, want) {
			t.Errorf("chapter2 entries = %v, want %s", names, want)
		}
	}
}

func TestCompleteRejectsPathTraversal(t *testing.T) {
	srv, _ := testServer(t)
	for _, dir := range []string{"..", "../../etc", "/etc", "chapter2/../.."} {
		rec := get(t, srv, "/complete?dir="+dir+"&kind=page")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("dir=%q: status = %d, want 400", dir, rec.Code)
		}
	}
}

func TestCompleteRejectsBadKind(t *testing.T) {
	srv, _ := testServer(t)
	rec := get(t, srv, "/complete?dir=.&kind=bogus")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestCompleteWithoutOpenProject(t *testing.T) {
	srv, err := newServer("")
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, srv, "/complete?dir=.&kind=page")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestCompleteHandlesSpacesInNames(t *testing.T) {
	srv, root := testServer(t)
	os.MkdirAll(filepath.Join(root, "my images"), 0o755)
	writePNG(t, root, "my images/a photo.png")

	dirs := completeEntries(t, get(t, srv, "/complete?dir=.&kind=image"))
	if !contains(completeNames(dirs), "my images") {
		t.Fatalf("root entries = %v, want \"my images\"", dirs)
	}

	nested := completeEntries(t, get(t, srv, "/complete?dir=my+images&kind=image"))
	if !contains(completeNames(nested), "a photo.png") {
		t.Errorf("nested entries = %v, want \"a photo.png\"", nested)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
