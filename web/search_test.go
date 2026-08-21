package web

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// searchFor runs a query and returns the fragment the search field shows.
// The index is built in the background, so the first queries after a change
// may still answer "indexing"; the browser repeats them, and so does this.
func searchFor(t *testing.T, srv *server, q string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		body := get(t, srv, "/search?q="+url.QueryEscape(q)).Body.String()
		if !strings.Contains(body, "data-indexing") {
			return body
		}
		if time.Now().After(deadline) {
			t.Fatalf("search for %q: the index was never built", q)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// hits reads the pages of a search fragment, mapped to their hit counts.
func hits(t *testing.T, body string) map[string]string {
	t.Helper()
	found := map[string]string{}
	for _, item := range strings.Split(body, `<li data-path="`)[1:] {
		path, rest, ok := strings.Cut(item, `"`)
		if !ok {
			t.Fatalf("malformed hit in:\n%s", body)
		}
		_, rest, ok = strings.Cut(rest, `data-count="`)
		if !ok {
			t.Fatalf("hit for %s has no count in:\n%s", path, body)
		}
		count, _, _ := strings.Cut(rest, `"`)
		found[path] = count
	}
	return found
}

// The search field is what the whole feature hangs off, and it posts to
// /search as the user types.
func TestPageServesSearchField(t *testing.T) {
	page := get(t, mustServer(t), "/").Body.String()
	for _, want := range []string{
		`id="search-input"`,
		`id="search-results"`,
		`hx-get="/search"`,
		`hx-target="#search-results"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q:\n%s", want, page)
		}
	}
}

// A query finds the pages whose text contains it, and reports how often.
// The count is what the tree shows next to a highlighted page.
func TestSearchFindsPages(t *testing.T) {
	srv, _ := testServer(t)
	found := hits(t, searchFor(t, srv, "second"))
	if len(found) != 1 {
		t.Fatalf("search for \"second\" found %v, want only chapter2/second.qmd", found)
	}
	// "Second" is both the title and the heading of that page.
	if found["chapter2/second.qmd"] != "2" {
		t.Errorf("chapter2/second.qmd has %q hits, want 2", found["chapter2/second.qmd"])
	}
}

// The summary next to the field is what tells the user whether the query
// matched at all, since the results themselves are shown in the tree.
func TestSearchSummarizesTheHits(t *testing.T) {
	srv, _ := testServer(t)
	if body := searchFor(t, srv, "second"); !strings.Contains(body, "2 hits in 1 page") {
		t.Errorf("search for \"second\" does not report its hits:\n%s", body)
	}
	if body := searchFor(t, srv, "nowhere"); !strings.Contains(body, "no hits") {
		t.Errorf("search for \"nowhere\" does not report the empty result:\n%s", body)
	}
}

// Typing is not case-accurate, and a search that only matched whole words
// would find nothing until the last letter is typed.
func TestSearchIsCaseInsensitiveAndMatchesWordStarts(t *testing.T) {
	srv, _ := testServer(t)
	found := hits(t, searchFor(t, srv, "SECO"))
	if _, ok := found["chapter2/second.qmd"]; !ok {
		t.Errorf("search for \"SECO\" found %v, want chapter2/second.qmd", found)
	}
	// A word start, not a substring: "econd" is inside "second".
	if found := hits(t, searchFor(t, srv, "econd")); len(found) != 0 {
		t.Errorf("search for \"econd\" found %v, want nothing", found)
	}
}

// Several terms narrow the search down instead of widening it.
func TestSearchRequiresEveryTerm(t *testing.T) {
	srv, _ := testServer(t)
	if found := hits(t, searchFor(t, srv, "second title")); len(found) != 1 {
		t.Errorf("search for \"second title\" found %v, want only chapter2/second.qmd", found)
	}
	// "Second" and "Home" are titles of two different pages.
	if found := hits(t, searchFor(t, srv, "second home")); len(found) != 0 {
		t.Errorf("search for \"second home\" found %v, want nothing", found)
	}
}

// A one-letter term matches nearly every page, which would highlight the
// whole tree while the user is still typing.
func TestSearchIgnoresShortAndEmptyQueries(t *testing.T) {
	srv, _ := testServer(t)
	for _, q := range []string{"", "  ", "s"} {
		if found := hits(t, searchFor(t, srv, q)); len(found) != 0 {
			t.Errorf("search for %q found %v, want nothing", q, found)
		}
	}
}

// The search reaches exactly the pages the tree shows: files and folders
// starting with "_" are drafts and includes, and a hit the user cannot open
// from the tree is a dead end.
func TestSearchSkipsWhatTheTreeSkips(t *testing.T) {
	srv, root := testServer(t)
	files := map[string]string{
		"_include.qmd":        "zebracrossing\n",
		"_drafts/notyet.qmd":  "zebracrossing\n",
		".hidden/secret.qmd":  "zebracrossing\n",
		"chapter2/listed.qmd": "---\ntitle: Listed\n---\nzebracrossing\n",
	}
	for name, content := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	get(t, srv, "/") // the index follows what the tree is rendered from

	found := hits(t, searchFor(t, srv, "zebracrossing"))
	if len(found) != 1 || found["chapter2/listed.qmd"] == "" {
		t.Errorf("search found %v, want only chapter2/listed.qmd", found)
	}
}

// An index that outlives the pages it describes finds text that is no
// longer there and misses text that just arrived.
func TestSearchFollowsTheProject(t *testing.T) {
	srv, root := testServer(t)
	searchFor(t, srv, "second") // build the index of the project as opened

	body := "---\ntitle: Second\n---\n# Second\n\nkumquat\n"
	rec := post(t, srv, "/save", url.Values{"path": {"chapter2/second.qmd"}, "body": {body}})
	if rec.Code != 200 {
		t.Fatalf("save: status %d: %s", rec.Code, rec.Body)
	}
	if found := hits(t, searchFor(t, srv, "kumquat")); found["chapter2/second.qmd"] == "" {
		t.Errorf("search found %v after the save, want chapter2/second.qmd", found)
	}

	// A page written behind the app's back is picked up by the same watch
	// that refreshes the tree.
	page := filepath.Join(root, "chapter2", "third.qmd")
	if err := os.WriteFile(page, []byte("---\ntitle: Third\n---\nrhubarb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	get(t, srv, "/watch")
	if found := hits(t, searchFor(t, srv, "rhubarb")); found["chapter2/third.qmd"] == "" {
		t.Errorf("search found %v after the outside edit, want chapter2/third.qmd", found)
	}
}

// The field is served before a project is open, and typing in it then must
// not fail -- it simply finds nothing.
func TestSearchWithoutProject(t *testing.T) {
	srv, err := newServer("")
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, srv, "/search?q=anything")
	if rec.Code != 200 {
		t.Fatalf("search without a project: status %d: %s", rec.Code, rec.Body)
	}
	if found := hits(t, rec.Body.String()); len(found) != 0 {
		t.Errorf("search without a project found %v", found)
	}
}

// The index is built in the background: a query that arrives before it is
// ready is answered with "indexing" rather than with a wait for the whole
// project to be read, and the client repeats it until the answer is real.
func TestSearchReportsIndexing(t *testing.T) {
	root := fixture(t)
	srv, err := newServer("")
	if err != nil {
		t.Fatal(err)
	}
	// `qm web <path>` opens the project without serving a page first, so
	// nothing has asked for an index yet.
	if err := srv.setRoot(root); err != nil {
		t.Fatal(err)
	}
	body := get(t, srv, "/search?q=second").Body.String()
	if !strings.Contains(body, "data-indexing") {
		t.Errorf("the first query does not report that the index is still being built:\n%s", body)
	}
	if body := searchFor(t, srv, "second"); !strings.Contains(body, "chapter2/second.qmd") {
		t.Errorf("the repeated query does not answer from the finished index:\n%s", body)
	}
}

// An empty field is not a query, and reporting it as "indexing" would put
// the client into a poll it can never leave.
func TestSearchDoesNotReportIndexingWithoutAQuery(t *testing.T) {
	srv, _ := testServer(t)
	if body := get(t, srv, "/search?q=").Body.String(); strings.Contains(body, "data-indexing") {
		t.Errorf("the empty query reports indexing:\n%s", body)
	}
}
