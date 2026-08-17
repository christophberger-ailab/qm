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
	custom := strings.Index(body, `href="/config/preview.css"`)
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
	rec := post(t, srv, "/config/preview-css", url.Values{"css": {css}})
	if rec.Code != http.StatusOK {
		t.Fatalf("save: status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Saved.") {
		t.Errorf("save response missing status:\n%s", rec.Body)
	}
	if got, err := os.ReadFile(srv.previewCSSFile); err != nil || string(got) != css {
		t.Fatalf("stored CSS = %q, %v; want %q", got, err, css)
	}

	rec = get(t, srv, "/config/preview.css")
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
	if rec := post(t, srv, "/config/preview-css", url.Values{"css": {css}}); rec.Code != http.StatusOK {
		t.Fatalf("initial save: status %d: %s", rec.Code, rec.Body)
	}

	req := httptest.NewRequest("POST", "/config/preview-css", strings.NewReader("css=a;b"))
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

	rec = get(t, srv, "/config/preview.css")
	if rec.Body.String() != css {
		t.Errorf("stylesheet body = %q, want %q", rec.Body.String(), css)
	}
}

func TestPreviewCSSEmptyFieldClearsStylesheet(t *testing.T) {
	srv, _ := configTestServer(t)
	css := ".markdown-preview p { color: blue; }\n"
	if rec := post(t, srv, "/config/preview-css", url.Values{"css": {css}}); rec.Code != http.StatusOK {
		t.Fatalf("initial save: status %d: %s", rec.Code, rec.Body)
	}

	rec := post(t, srv, "/config/preview-css", url.Values{"css": {""}})
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Saved.") {
		t.Errorf("clear response missing status:\n%s", rec.Body)
	}
	rec = get(t, srv, "/config/preview.css")
	if rec.Body.Len() != 0 {
		t.Errorf("stylesheet body = %q, want empty", rec.Body.String())
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
	if rec.Body.Len() != 0 {
		t.Errorf("empty stylesheet route returned %q", rec.Body.String())
	}
	if rec := get(t, srv, "/"); rec.Code != http.StatusOK {
		t.Errorf("app page after empty stylesheet: status %d", rec.Code)
	}
}
