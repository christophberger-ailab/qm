package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configTestServer(t *testing.T) (*server, string) {
	t.Helper()
	root := fixture(t)
	prefs := filepath.Join(t.TempDir(), "render.json")
	srv, err := newServer(prefs)
	if err != nil {
		t.Fatal(err)
	}
	rec := post(t, srv, "/open", url.Values{"path": {root}})
	if rec.Code != http.StatusOK {
		t.Fatalf("open: status %d: %s", rec.Code, rec.Body)
	}
	return srv, root
}

func TestAppLinksConfigAndPreviewStylesheet(t *testing.T) {
	srv, _ := configTestServer(t)
	body := get(t, srv, "/").Body.String()
	if !strings.Contains(body, `<a class="button topbar-config" href="/config">Config</a>`) {
		t.Errorf("page missing top-bar Config button:\n%s", body)
	}
	app := strings.Index(body, `href="/static/app.css"`)
	custom := strings.Index(body, `href="/config/preview.css?file=`)
	switch {
	case app < 0:
		t.Fatalf("page does not link app.css:\n%s", body)
	case custom < 0:
		t.Fatalf("page does not link custom preview stylesheet:\n%s", body)
	case custom < app:
		t.Fatalf("custom preview stylesheet is linked before app.css:\n%s", body)
	}
}

func TestConfigPageListsPreviewCSSEntry(t *testing.T) {
	srv, _ := configTestServer(t)
	rec := get(t, srv, "/config")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Preview: Custom CSS",
		`href="/config/preview-css"`,
		`href="/"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("config page missing %q:\n%s", want, body)
		}
	}
}

func TestPreviewCSSEditorServesCodeMirrorCSSMode(t *testing.T) {
	srv, _ := configTestServer(t)
	rec := get(t, srv, "/config/preview-css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<textarea name="css" class="preview-css-content"`,
		`src="/static/codemirror/codemirror.js"`,
		`src="/static/codemirror/css.js"`,
		`src="/static/config.js"`,
		"loaded document-wide after the app's own stylesheet",
		".markdown-preview h1 { color: rebeccapurple; }",
		`href="/config"`,
		`href="/"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editor page missing %q:\n%s", want, body)
		}
	}
	before := func(first, second string) {
		t.Helper()
		i, j := strings.Index(body, first), strings.Index(body, second)
		switch {
		case i < 0:
			t.Fatalf("editor page does not load %s", first)
		case j < 0:
			t.Fatalf("editor page does not load %s", second)
		case i > j:
			t.Fatalf("%s is loaded after %s:\n%s", first, second, body)
		}
	}
	before("/static/codemirror/codemirror.js", "/static/codemirror/css.js")
	before("/static/codemirror/css.js", "/static/config.js")
	if rec := get(t, srv, "/static/codemirror/css.js"); rec.Code != http.StatusOK {
		t.Errorf("GET css mode: status %d, want 200", rec.Code)
	}
}

func TestPreviewCSSSaveAndServe(t *testing.T) {
	srv, _ := configTestServer(t)
	css := ".markdown-preview h1 { color: red; }\n"
	rec := post(t, srv, "/config/preview-css", url.Values{"file": {defaultCSSName}, "css": {css}})
	if rec.Code != http.StatusOK {
		t.Fatalf("save: status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Saved.") {
		t.Errorf("save response missing status:\n%s", rec.Body)
	}
	if got, err := os.ReadFile(filepath.Join(srv.cssDir, defaultCSSName)); err != nil || string(got) != css {
		t.Fatalf("stored CSS = %q, %v; want %q", got, err, css)
	}

	rec = get(t, srv, "/config/preview.css?file="+defaultCSSName)
	if rec.Code != http.StatusOK {
		t.Fatalf("stylesheet: status %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/css" {
		t.Errorf("Content-Type %q, want text/css", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", cc)
	}
	if rec.Body.String() != css {
		t.Errorf("stylesheet body = %q, want %q", rec.Body.String(), css)
	}
}

func TestPreviewCSSMalformedFormKeepsSavedCSS(t *testing.T) {
	srv, _ := configTestServer(t)
	css := ".markdown-preview p { color: blue; }\n"
	if rec := post(t, srv, "/config/preview-css", url.Values{"file": {defaultCSSName}, "css": {css}}); rec.Code != http.StatusOK {
		t.Fatalf("initial save: status %d: %s", rec.Code, rec.Body)
	}

	req := httptest.NewRequest("POST", "/config/preview-css", strings.NewReader("file="+defaultCSSName+"&css=a;b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "invalid semicolon") {
		t.Errorf("malformed response missing parse error:\n%s", body)
	}
	if strings.Contains(body, "Saved.") {
		t.Errorf("malformed response claims success:\n%s", body)
	}
	if !strings.Contains(body, css) {
		t.Errorf("malformed response does not show saved CSS:\n%s", body)
	}

	rec = get(t, srv, "/config/preview.css?file="+defaultCSSName)
	if rec.Body.String() != css {
		t.Errorf("stylesheet body = %q, want %q", rec.Body.String(), css)
	}
}

func TestPreviewCSSEmptyFieldClearsStylesheet(t *testing.T) {
	srv, _ := configTestServer(t)
	css := ".markdown-preview p { color: blue; }\n"
	if rec := post(t, srv, "/config/preview-css", url.Values{"file": {defaultCSSName}, "css": {css}}); rec.Code != http.StatusOK {
		t.Fatalf("initial save: status %d: %s", rec.Code, rec.Body)
	}

	rec := post(t, srv, "/config/preview-css", url.Values{"file": {defaultCSSName}, "css": {""}})
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Saved.") {
		t.Errorf("clear response missing status:\n%s", rec.Body)
	}
	rec = get(t, srv, "/config/preview.css?file="+defaultCSSName)
	if rec.Body.Len() != 0 {
		t.Errorf("stylesheet body = %q, want empty", rec.Body.String())
	}
}

// TestDefaultCSSIsBakedIn checks that a fresh config directory gets the
// app's built-in default stylesheet materialized as custom.css, so the
// preview looks the same as it always has without any setup.
func TestDefaultCSSIsBakedIn(t *testing.T) {
	srv, _ := configTestServer(t)
	rec := get(t, srv, "/config/preview.css?file="+defaultCSSName)
	if rec.Code != http.StatusOK {
		t.Fatalf("stylesheet: status %d: %s", rec.Code, rec.Body)
	}
	if rec.Body.String() != defaultCustomCSS {
		t.Errorf("stylesheet body = %q, want the baked-in default", rec.Body.String())
	}
	if got, err := os.ReadFile(filepath.Join(srv.cssDir, defaultCSSName)); err != nil || string(got) != defaultCustomCSS {
		t.Fatalf("custom.css on disk = %q, %v; want the baked-in default", got, err)
	}
	if rec := get(t, srv, "/"); rec.Code != http.StatusOK {
		t.Errorf("app page after baked-in default: status %d", rec.Code)
	}
}

// TestDefaultCSSNotRewrittenOnRestart checks that ensureDefaultCSS never
// touches an existing css directory: a user who cleared or replaced
// custom.css keeps that choice across restarts.
func TestDefaultCSSNotRewrittenOnRestart(t *testing.T) {
	root := fixture(t)
	prefs := filepath.Join(t.TempDir(), "render.json")
	srv1, err := newServer(prefs)
	if err != nil {
		t.Fatal(err)
	}
	if rec := post(t, srv1, "/config/preview-css", url.Values{"file": {defaultCSSName}, "css": {"/* mine */\n"}}); rec.Code != http.StatusOK {
		t.Fatalf("save: status %d: %s", rec.Code, rec.Body)
	}

	srv2, err := newServer(prefs)
	if err != nil {
		t.Fatal(err)
	}
	post(t, srv2, "/open", url.Values{"path": {root}})
	rec := get(t, srv2, "/config/preview.css?file="+defaultCSSName)
	if rec.Body.String() != "/* mine */\n" {
		t.Errorf("stylesheet body = %q, want the user's saved CSS to survive a restart", rec.Body.String())
	}
}

func TestPreviewCSSRouteWithoutConfigDir(t *testing.T) {
	// A server with persistence disabled (empty prefs file, as tests that
	// do not exercise config use) still serves the baked-in default from
	// memory instead of an error or a blank sheet.
	srv, root := testServer(t)
	_ = root
	rec := get(t, srv, "/config/preview.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("stylesheet: status %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/css" {
		t.Errorf("Content-Type %q, want text/css", ct)
	}
	if rec.Body.String() != defaultCustomCSS {
		t.Errorf("stylesheet body = %q, want the baked-in default", rec.Body.String())
	}
}

// TestAddAlternateCSSFile checks that adding a second stylesheet works and
// that its dropdown only shows up once there is more than one to choose
// from.
func TestAddAlternateCSSFile(t *testing.T) {
	srv, _ := configTestServer(t)

	// With only the default stylesheet, no switcher is offered anywhere.
	if body := get(t, srv, "/config/preview-css").Body.String(); strings.Contains(body, `id="css-file-select"`) {
		t.Errorf("single-file editor page unexpectedly offers a file switcher:\n%s", body)
	}
	if body := get(t, srv, "/content?path=index.qmd").Body.String(); strings.Contains(body, `id="preview-css-select"`) {
		t.Errorf("single-file content pane unexpectedly offers a stylesheet dropdown:\n%s", body)
	}

	rec := post(t, srv, "/config/preview-css/new", url.Values{"name": {"dark"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("new stylesheet: status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Created.") {
		t.Errorf("new stylesheet response missing status:\n%s", rec.Body)
	}
	if _, err := os.Stat(filepath.Join(srv.cssDir, "dark.css")); err != nil {
		t.Fatalf("dark.css was not created: %v", err)
	}

	body := get(t, srv, "/config/preview-css").Body.String()
	for _, want := range []string{`id="css-file-select"`, `value="custom.css"`, `value="dark.css"`} {
		if !strings.Contains(body, want) {
			t.Errorf("editor page missing %q after adding a stylesheet:\n%s", want, body)
		}
	}

	body = get(t, srv, "/content?path=index.qmd").Body.String()
	if !strings.Contains(body, `id="preview-css-select"`) {
		t.Errorf("content pane missing stylesheet dropdown once two files exist:\n%s", body)
	}
	for _, want := range []string{`value="custom.css"`, `value="dark.css"`} {
		if !strings.Contains(body, want) {
			t.Errorf("stylesheet dropdown missing option %q:\n%s", want, body)
		}
	}
}

// TestSwitchActiveCSS checks that the dropdown's persisted choice changes
// which stylesheet the main app's <link> and /config/preview.css serve.
func TestSwitchActiveCSS(t *testing.T) {
	srv, _ := configTestServer(t)
	post(t, srv, "/config/preview-css/new", url.Values{"name": {"dark.css"}})
	post(t, srv, "/config/preview-css", url.Values{"file": {"dark.css"}, "css": {"/* dark */\n"}})

	rec := post(t, srv, "/config/active-css", url.Values{"file": {"dark.css"}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("select active: status %d: %s", rec.Code, rec.Body)
	}

	body := get(t, srv, "/").Body.String()
	if !strings.Contains(body, `href="/config/preview.css?file=dark.css"`) {
		t.Errorf("app page does not link the newly active stylesheet:\n%s", body)
	}
	rec = get(t, srv, "/config/preview.css")
	if rec.Body.String() != "/* dark */\n" {
		t.Errorf("stylesheet body = %q, want the active file's content", rec.Body.String())
	}

	if rec := post(t, srv, "/config/active-css", url.Values{"file": {"no-such.css"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("select unknown file: status %d, want 400", rec.Code)
	}
}

func TestPreviewCSSRouteWithoutSavedCSS(t *testing.T) {
	srv, _ := configTestServer(t)
	rec := get(t, srv, "/config/preview.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("stylesheet: status %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/css" {
		t.Errorf("Content-Type %q, want text/css", ct)
	}
	if rec.Body.Len() == 0 {
		t.Errorf("stylesheet route returned empty body, want the baked-in default")
	}
	if rec := get(t, srv, "/"); rec.Code != http.StatusOK {
		t.Errorf("app page after default stylesheet: status %d", rec.Code)
	}
}
