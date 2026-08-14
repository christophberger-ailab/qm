package web

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cboct/qm/internal/project"
)

// Keep test files out of the real system trash.
func TestMain(m *testing.M) {
	project.TrashCommands = nil
	os.Exit(m.Run())
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.qmd":            "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"chapter2/index.qmd":   "---\ntitle: Chapter 2\norder: 2\n---\n# Two\n",
		"chapter2/second.qmd":  "---\ntitle: Second\norder: 1\n---\n# Second\n",
		"chapter2/third.qmd":   "---\ntitle: Third\norder: 2\n---\n# Third\n",
		"chapter2/loose.qmd":   "---\ntitle: Loose\n---\n# Loose\n",
		"chapter2/broken.qmd":  "---\ntitle: Broken\norder: 3\n---\n::: {.callout-note}\nunclosed\n",
		"_quarto.yml":          "project:\n  type: book\nbook:\n  chapters:\n    - index.qmd\n",
		"_quarto-chapter2.yml": "book:\n  chapters:\n    - index.qmd\n",
		"_quarto-web.yml":      "format:\n  html: default\n",
	}
	for name, content := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testServer(t *testing.T) (*server, string) {
	t.Helper()
	root := fixture(t)
	srv, err := newServer("")
	if err != nil {
		t.Fatal(err)
	}
	rec := post(t, srv, "/open", url.Values{"path": {root}})
	if rec.Code != http.StatusOK {
		t.Fatalf("open: status %d: %s", rec.Code, rec.Body)
	}
	return srv, root
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	return rec
}

func post(t *testing.T, h http.Handler, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestOpenRendersTree(t *testing.T) {
	srv, _ := testServer(t)
	body := get(t, srv, "/").Body.String()
	for _, want := range []string{
		`data-path="chapter2/second.qmd"`,
		`data-parent="chapter2/index.qmd"`,
		"Second",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if !strings.Contains(body, `unordered" data-path="chapter2/loose.qmd"`) {
		t.Errorf("loose.qmd not marked unordered:\n%s", body)
	}
	if !strings.Contains(body, `bad-fences" data-path="chapter2/broken.qmd"`) {
		t.Errorf("broken.qmd not marked bad-fences:\n%s", body)
	}
	if strings.Contains(body, `bad-fences" data-path="chapter2/second.qmd"`) {
		t.Errorf("second.qmd wrongly marked bad-fences:\n%s", body)
	}
}

// A move reorders the tree on disk and leaves every _quarto*.yml config
// alone: chapter lists are no longer maintained by the sorter.
func TestMoveReordersAndLeavesConfigsAlone(t *testing.T) {
	srv, root := testServer(t)
	before := configs(t, root)
	rec := post(t, srv, "/move", url.Values{
		"src": {"chapter2/third.qmd"}, "parent": {"chapter2/index.qmd"}, "pos": {"0"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("move: status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if strings.Index(body, "chapter2/third.qmd") > strings.Index(body, "chapter2/second.qmd") {
		t.Errorf("third not before second:\n%s", body)
	}
	assertConfigsUnchanged(t, root, before)
}

// configs snapshots the contents of the project's _quarto*.yml files.
func configs(t *testing.T, root string) map[string]string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "_quarto*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(m)] = string(b)
	}
	return out
}

func assertConfigsUnchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	for name, want := range configs(t, root) {
		if got := before[name]; got != want {
			t.Errorf("%s was rewritten:\nbefore: %s\nafter:  %s", name, got, want)
		}
	}
}

func TestContent(t *testing.T) {
	srv, _ := testServer(t)
	body := get(t, srv, "/content?path=chapter2/second.qmd").Body.String()
	if !strings.Contains(body, "# Second") {
		t.Errorf("content missing file body:\n%s", body)
	}
	if rec := get(t, srv, "/content?path=../outside.qmd"); rec.Code != http.StatusBadRequest {
		t.Errorf("traversal: status %d, want 400", rec.Code)
	}
	// A plain content fetch must not refresh the tree, a reload must.
	if strings.Contains(body, `hx-swap-oob="innerHTML:#tree"`) {
		t.Errorf("content without reload refreshes tree:\n%s", body)
	}
	reload := get(t, srv, "/content?path=chapter2/second.qmd&reload=1").Body.String()
	if !strings.Contains(reload, `hx-swap-oob="innerHTML:#tree"`) {
		t.Errorf("reload missing tree refresh:\n%s", reload)
	}
}

// The editor is served with the preview pane and its toggle beside it; the
// preview itself is filled in the browser from the textarea (preview.js).
func TestContentServesPreviewPane(t *testing.T) {
	srv, _ := testServer(t)
	body := get(t, srv, "/content?path=chapter2/second.qmd").Body.String()
	for _, want := range []string{
		`class="editor-split"`,
		`id="preview-toggle"`,
		`id="preview-divider"`,
		`id="preview"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editor missing %q:\n%s", want, body)
		}
	}
}

// The preview renders in the browser, so its two scripts must be embedded
// in the binary and loaded by the page.
func TestPreviewAssetsAreServed(t *testing.T) {
	srv, _ := testServer(t)
	page := get(t, srv, "/").Body.String()
	for _, asset := range []string{"/static/marked.umd.js", "/static/preview.js"} {
		if !strings.Contains(page, `src="`+asset+`"`) {
			t.Errorf("page does not load %s", asset)
		}
		if rec := get(t, srv, asset); rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", asset, rec.Code)
		}
	}
}

// onePixelPNG is a 1x1 transparent PNG. The media route copies bytes, so
// the tests only need a file that is genuinely an image.
var onePixelPNG = mustDecode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

func mustDecode(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// writePNG puts an image at rel below dir and returns its content.
func writePNG(t *testing.T, dir, rel string) []byte {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	return onePixelPNG
}

// A page's images are fetched from the project through /media. The project
// writes them website-absolute (`/assets/images/bord.png`), which the
// preview turns into `/media/assets/images/bord.png`.
func TestMediaServesProjectImages(t *testing.T) {
	srv, root := testServer(t)
	want := writePNG(t, root, "assets/images/bord.png")

	rec := get(t, srv, "/media/assets/images/bord.png")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type %q, want image/png", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Errorf("body is not the file on disk")
	}
}

// /media exists so the preview can show images. It must not become a reader
// for the rest of the project.
func TestMediaServesImagesOnly(t *testing.T) {
	srv, _ := testServer(t)
	for _, p := range []string{"/media/index.qmd", "/media/_quarto.yml"} {
		if rec := get(t, srv, p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", p, rec.Code)
		}
	}
}

// However it is spelled, a path leading out of the project must not be
// served.
func TestMediaRejectsTraversal(t *testing.T) {
	srv, root := testServer(t)
	writePNG(t, filepath.Dir(root), "outside.png")

	for _, p := range []string{
		"/media/../outside.png",
		"/media/%2e%2e/outside.png",
		"/media/assets/../../outside.png",
	} {
		if rec := get(t, srv, p); rec.Code == http.StatusOK {
			t.Errorf("GET %s: served the file", p)
		}
	}
}

func TestMediaWithoutProject(t *testing.T) {
	srv, err := newServer("")
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(t, srv, "/media/x.png"); rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rec.Code)
	}
}

func TestMediaMissingFile(t *testing.T) {
	srv, _ := testServer(t)
	if rec := get(t, srv, "/media/assets/nope.png"); rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}

func TestSave(t *testing.T) {
	srv, root := testServer(t)
	newBody := "---\ntitle: Second\norder: 1\n---\n# Second updated\n"
	rec := post(t, srv, "/save", url.Values{"path": {"chapter2/second.qmd"}, "body": {newBody}})
	if rec.Code != http.StatusOK {
		t.Fatalf("save: status %d: %s", rec.Code, rec.Body)
	}
	// Save leaves the editor alone and updates the heading out of band;
	// the title is unchanged here, so the tree must not be refreshed.
	if !strings.Contains(rec.Body.String(), `id="content-title" hx-swap-oob="true"`) {
		t.Errorf("response missing heading update:\n%s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), `hx-swap-oob="innerHTML:#tree"`) {
		t.Errorf("tree refreshed although title unchanged:\n%s", rec.Body)
	}
	got, err := os.ReadFile(filepath.Join(root, "chapter2/second.qmd"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != newBody {
		t.Errorf("file on disk not updated: %s", got)
	}

	// Changing the title must refresh the tree out of band.
	renamed := "---\ntitle: Second renamed\norder: 1\n---\n# Second updated\n"
	rec = post(t, srv, "/save", url.Values{"path": {"chapter2/second.qmd"}, "body": {renamed}})
	if rec.Code != http.StatusOK {
		t.Fatalf("save rename: status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `hx-swap-oob="innerHTML:#tree"`) {
		t.Errorf("tree not refreshed after title change:\n%s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Second renamed") {
		t.Errorf("response missing new title:\n%s", rec.Body)
	}

	if rec := post(t, srv, "/save", url.Values{"path": {"../outside.qmd"}, "body": {"x"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("traversal: status %d, want 400", rec.Code)
	}

	if rec := post(t, srv, "/save", url.Values{"path": {"nope.qmd"}, "body": {"x"}}); rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Errorf("nonexistent file: status %d, want 400 or 404", rec.Code)
	}
}

func TestCreateAndDelete(t *testing.T) {
	srv, root := testServer(t)
	before := configs(t, root)
	rec := post(t, srv, "/create", url.Values{
		"parent": {""}, "name": {"about"}, "title": {"About"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d: %s", rec.Code, rec.Body)
	}
	// A new page is created as name/index.qmd.
	if !strings.Contains(rec.Body.String(), `data-path="about/index.qmd"`) {
		t.Errorf("tree missing created page:\n%s", rec.Body)
	}
	assertConfigsUnchanged(t, root, before)

	rec = post(t, srv, "/delete", url.Values{"path": {"about/index.qmd"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status %d: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), `data-path="about/index.qmd"`) {
		t.Errorf("tree still shows deleted page:\n%s", rec.Body)
	}
	if _, err := os.Stat(filepath.Join(root, "about")); !os.IsNotExist(err) {
		t.Error("about/ still on disk")
	}
	assertConfigsUnchanged(t, root, before)
}

// The top-bar create form sends the selected page as "after"; the new page
// lands right behind it in the same group.
func TestCreateAfter(t *testing.T) {
	srv, _ := testServer(t)
	rec := post(t, srv, "/create", url.Values{
		"parent": {""}, "after": {"chapter2/second.qmd"},
		"name": {"inserted"}, "title": {"Inserted"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-path="chapter2/inserted/index.qmd"`) {
		t.Errorf("tree missing inserted page:\n%s", body)
	}
	// The new page sits after second and before third in the tree.
	iSecond := strings.Index(body, `data-path="chapter2/second.qmd"`)
	iNew := strings.Index(body, `data-path="chapter2/inserted/index.qmd"`)
	iThird := strings.Index(body, `data-path="chapter2/third.qmd"`)
	if !(iSecond < iNew && iNew < iThird) {
		t.Errorf("inserted page not between second and third:\n%s", body)
	}
}

// /watch returns 204 while the project is unchanged and the refreshed tree
// once a page appears on disk outside the sorter.
func TestWatchDetectsExternalChanges(t *testing.T) {
	srv, root := testServer(t)
	if rec := get(t, srv, "/watch"); rec.Code != http.StatusNoContent {
		t.Fatalf("watch unchanged: status %d, want 204", rec.Code)
	}
	// Add a page directly on disk.
	page := "---\ntitle: Outside\norder: 9\n---\n# Outside\n"
	if err := os.WriteFile(filepath.Join(root, "chapter2", "outside.qmd"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := get(t, srv, "/watch")
	if rec.Code != http.StatusOK {
		t.Fatalf("watch changed: status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-path="chapter2/outside.qmd"`) {
		t.Errorf("watch missing externally added page:\n%s", rec.Body)
	}
	// Once served, the state is up to date again.
	if rec := get(t, srv, "/watch"); rec.Code != http.StatusNoContent {
		t.Errorf("watch after refresh: status %d, want 204", rec.Code)
	}
}

func TestOpenBadPath(t *testing.T) {
	srv, err := newServer("")
	if err != nil {
		t.Fatal(err)
	}
	if rec := post(t, srv, "/open", url.Values{"path": {"/no/such/dir"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("open bad path: status %d, want 400", rec.Code)
	}
}
