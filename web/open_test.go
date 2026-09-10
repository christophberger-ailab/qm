package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Open field shows the last element of the project's path -- the
// folder name -- while the full path travels with it, so a submit still
// opens the project the label stands for.
func TestOpenFieldShowsLastPathElement(t *testing.T) {
	srv, root := testServer(t)
	body := get(t, srv, "/").Body.String()
	if !strings.Contains(body, `value="`+filepath.Base(root)+`"`) {
		t.Errorf("field does not show the folder name %q:\n%s", filepath.Base(root), body)
	}
	if !strings.Contains(body, `data-full="`+root+`"`) {
		t.Errorf("field does not carry the full path %q:\n%s", root, body)
	}
}

// A path with nothing to shorten -- the root directory -- is shown as it
// is, rather than as an empty field.
func TestPathLabel(t *testing.T) {
	tests := []struct{ path, want string }{
		{"/home/u/book", "book"},
		{"/home/u/book/", "book"},
		{"/", "/"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := pathLabel(tc.path); got != tc.want {
			t.Errorf("pathLabel(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// Every project opened lands in the dropdown, most recent first, and none
// of them twice.
func TestRecentPathsAreOffered(t *testing.T) {
	srv, first := testServer(t)
	second := fixture(t)
	if rec := post(t, srv, "/open", url.Values{"path": {second}}); rec.Code != http.StatusOK {
		t.Fatalf("open: status %d: %s", rec.Code, rec.Body)
	}
	// Opening the first one again moves it back to the head instead of
	// listing it twice.
	if rec := post(t, srv, "/open", url.Values{"path": {first}}); rec.Code != http.StatusOK {
		t.Fatalf("reopen: status %d: %s", rec.Code, rec.Body)
	}

	if got := srv.recent; len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("recent = %v, want [%s %s]", got, first, second)
	}
	body := get(t, srv, "/").Body.String()
	for _, want := range []string{
		`data-path="` + first + `"`,
		`data-path="` + second + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dropdown missing %q:\n%s", want, body)
		}
	}
}

// The list is capped, so the dropdown stays a list one can look through.
func TestRecentPathsAreCapped(t *testing.T) {
	srv := bareServer(t)
	for i := 0; i < maxRecent+5; i++ {
		srv.rememberRoot(filepath.Join(t.TempDir(), "p"))
	}
	if len(srv.recent) != maxRecent {
		t.Errorf("recent holds %d paths, want %d", len(srv.recent), maxRecent)
	}
}

// The dropdown survives a restart: it is written beside the render prefs
// and read back by the next server.
func TestRecentPathsPersist(t *testing.T) {
	dir := t.TempDir()
	prefs := filepath.Join(dir, "render.json")
	root := fixture(t)

	srv, err := newServer(prefs)
	if err != nil {
		t.Fatal(err)
	}
	if rec := post(t, srv, "/open", url.Values{"path": {root}}); rec.Code != http.StatusOK {
		t.Fatalf("open: status %d: %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "recent.json")); err != nil {
		t.Fatalf("recent.json not written: %v", err)
	}

	again, err := newServer(prefs)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.recent) != 1 || again.recent[0] != root {
		t.Errorf("restored recent = %v, want [%s]", again.recent, root)
	}
}

// /open replaces the top bar's Open field along with the panes, so the
// label and the dropdown describe the project just opened.
func TestOpenRefreshesTheOpenField(t *testing.T) {
	srv, _ := testServer(t)
	other := fixture(t)
	body := post(t, srv, "/open", url.Values{"path": {other}}).Body.String()
	if !strings.Contains(body, `hx-swap-oob="innerHTML:#open-panel"`) {
		t.Errorf("open does not refresh the Open field:\n%s", body)
	}
	if !strings.Contains(body, `data-full="`+other+`"`) {
		t.Errorf("refreshed field does not carry %q:\n%s", other, body)
	}
}

// bareServer is a server with no project open, which is what the app
// starts as when it is given no path.
func bareServer(t *testing.T) *server {
	t.Helper()
	srv, err := newServer("")
	if err != nil {
		t.Fatal(err)
	}
	return srv
}
