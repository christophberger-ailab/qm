package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// UI.13-3: the topic list starts with the table of contents, follows the
// order it links the pages in, and names each page by its first heading.
func TestHelpTopicsFollowTheIndex(t *testing.T) {
	srv, _ := testServer(t)
	rec := get(t, srv, "/help")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /help: status %d", rec.Code)
	}
	var topics []helpTopic
	if err := json.Unmarshal(rec.Body.Bytes(), &topics); err != nil {
		t.Fatal(err)
	}
	if len(topics) < 2 || topics[0].Name != helpIndex {
		t.Fatalf("topics = %+v, want the index first", topics)
	}
	index, err := helpFS.ReadFile("help/" + helpIndex)
	if err != nil {
		t.Fatal(err)
	}
	var linked []string
	for _, m := range helpLink.FindAllStringSubmatch(string(index), -1) {
		linked = append(linked, m[1])
	}
	for i, name := range linked {
		if i+1 >= len(topics) || topics[i+1].Name != name {
			t.Fatalf("topic %d is %+v, want %s as the index links it", i+1, topics[i+1], name)
		}
	}
	for _, topic := range topics {
		if topic.Title == "" || strings.HasSuffix(topic.Title, ".md") {
			t.Errorf("topic %s has no title of its own: %q", topic.Name, topic.Title)
		}
	}
}

// UI.13-1, UI.13-7: a page is served as embedded, and only by plain name.
func TestHelpPageIsServedByName(t *testing.T) {
	srv, _ := testServer(t)
	rec := get(t, srv, "/help/index.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /help/index.md: status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("Content-Type = %q, want text/markdown", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "# ") {
		t.Errorf("body does not start with the page's heading: %q", rec.Body.String())
	}
	for _, target := range []string{"/help/missing.md", "/help/index", "/help/..%2Fhelp.go", "/help/%2E%2E%2Fserver.go"} {
		if rec := get(t, srv, target); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", target, rec.Code)
		}
	}
}

// Every link between help pages leads to a page that exists, so the pane
// never follows a link into a 404.
func TestHelpLinksResolve(t *testing.T) {
	entries, err := helpFS.ReadDir("help")
	if err != nil {
		t.Fatal(err)
	}
	link := regexp.MustCompile(`\]\(([^)#:]+\.md)[^)]*\)`)
	for _, e := range entries {
		text, err := helpFS.ReadFile("help/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range link.FindAllStringSubmatch(string(text), -1) {
			if _, err := helpFS.ReadFile("help/" + m[1]); err != nil {
				t.Errorf("%s links %s, which is not a help page", e.Name(), m[1])
			}
		}
	}
}

// UI.3-3, UI.13: the content header has a Help button and the editor
// split a help pane beside the other two.
func TestHelpPaneIsOffered(t *testing.T) {
	srv, _ := testServer(t)
	rec := get(t, srv, "/content?path=index.qmd")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /content: status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`id="help-toggle" data-pane="help"`, `id="help-pane" data-pane="help"`, `id="help-divider"`, `id="help"`} {
		if !strings.Contains(body, want) {
			t.Errorf("content does not carry %s", want)
		}
	}
}

// UI.13-8: the help is offered while no page is open, with or without a
// project.
func TestHelpPaneWithoutAPage(t *testing.T) {
	srv, err := newServer("")
	if err != nil {
		t.Fatal(err)
	}
	check := func(when, body string) {
		t.Helper()
		for _, want := range []string{`id="help-toggle" data-pane="help"`, `id="help-pane" data-pane="help"`, `id="help-divider"`, `class="edit-form editor-placeholder"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: page does not carry %s", when, want)
			}
		}
	}
	check("no project", get(t, srv, "/").Body.String())
	srv, _ = testServer(t)
	check("no page", get(t, srv, "/").Body.String())
}

// helpTitle falls back to the page's name when it has no heading.
func TestHelpTitle(t *testing.T) {
	if got := helpTitle("intro\n# The tree \n", "tree.md"); got != "The tree" {
		t.Errorf("helpTitle = %q", got)
	}
	if got := helpTitle("no heading", "tree.md"); got != "tree" {
		t.Errorf("helpTitle = %q", got)
	}
}
