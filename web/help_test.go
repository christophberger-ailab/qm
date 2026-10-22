package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// UI.13-3: the topic list starts with the table of contents, follows the
// order it links the pages in, and names each page by its first heading.
func TestHelpTopicsFollowTheIndex(t *testing.T) {
	srv, _ := testServer(t)
	for _, lang := range helpLanguages() {
		rec := get(t, srv, "/help?lang="+lang.Code)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /help: status %d", rec.Code)
		}
		var contents helpContents
		if err := json.Unmarshal(rec.Body.Bytes(), &contents); err != nil {
			t.Fatal(err)
		}
		if contents.Lang != lang.Code {
			t.Errorf("asked for %s, got %s", lang.Code, contents.Lang)
		}
		topics := contents.Topics
		if len(topics) < 2 || topics[0].Name != helpIndex {
			t.Fatalf("%s: topics = %+v, want the index first", lang.Code, topics)
		}
		index, err := helpPage(lang.Code, helpIndex)
		if err != nil {
			t.Fatal(err)
		}
		var linked []string
		for _, m := range helpLink.FindAllStringSubmatch(string(index), -1) {
			linked = append(linked, m[1])
		}
		for i, name := range linked {
			if i+1 >= len(topics) || topics[i+1].Name != name {
				t.Fatalf("%s: topic %d is not %s as the index links it: %+v", lang.Code, i+1, name, topics)
			}
		}
		for _, topic := range topics {
			if topic.Title == "" || strings.HasSuffix(topic.Title, ".md") {
				t.Errorf("%s: topic %s has no title of its own: %q", lang.Code, topic.Name, topic.Title)
			}
		}
	}
}

// UI.13-9: the help is in English and German, and a translation has every
// page the English help has, so no topic falls back mid-reading.
func TestHelpTranslationsAreComplete(t *testing.T) {
	var codes []string
	for _, lang := range helpLanguages() {
		codes = append(codes, lang.Code)
	}
	if !slices.Equal(codes, []string{"de", "en"}) {
		t.Fatalf("languages = %v, want de and en", codes)
	}
	en := helpNames(helpFallback)
	for _, code := range codes {
		if got := helpNames(code); !slices.Equal(got, en) {
			t.Errorf("%s has pages %v, want %v", code, got, en)
		}
	}
}

// UI.13-9: the language is picked from the browser's list by primary
// subtag, the first the help has winning; anything else gets English.
func TestHelpLangPicksFromTheBrowsersList(t *testing.T) {
	for wanted, want := range map[string]string{
		"de-DE,de;q=0.9":  "de",
		"fr-FR,de-AT,en":  "de",
		"en-US,de":        "en",
		"fr,it":           "en",
		"":                "en",
		"../en,DE":        "de",
		"de/../../server": "en",
	} {
		if got := helpLang(wanted); got != want {
			t.Errorf("helpLang(%q) = %q, want %q", wanted, got, want)
		}
	}
}

// UI.13-1, UI.13-7: a page is served as embedded, and only by plain name.
func TestHelpPageIsServedByName(t *testing.T) {
	srv, _ := testServer(t)
	for _, lang := range []string{"en", "de"} {
		rec := get(t, srv, "/help/"+lang+"/index.md")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /help/%s/index.md: status %d", lang, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
			t.Errorf("Content-Type = %q, want text/markdown", ct)
		}
		want, _ := helpFS.ReadFile("help/" + lang + "/index.md")
		if rec.Body.String() != string(want) {
			t.Errorf("GET /help/%s/index.md does not serve that page", lang)
		}
	}
	// A language the help is not written in gets the English page.
	en, _ := helpFS.ReadFile("help/en/index.md")
	if rec := get(t, srv, "/help/fr/index.md"); rec.Body.String() != string(en) {
		t.Errorf("GET /help/fr/index.md does not fall back to English")
	}
	for _, target := range []string{"/help/en/missing.md", "/help/en/index", "/help/en/..%2Fen.md", "/help/en/%2E%2E%2F%2E%2E%2Fhelp.go"} {
		if rec := get(t, srv, target); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", target, rec.Code)
		}
	}
}

// Every link between help pages leads to a page that exists, so the pane
// never follows a link into a 404.
func TestHelpLinksResolve(t *testing.T) {
	link := regexp.MustCompile(`\]\(([^)#:]+\.md)[^)]*\)`)
	for _, lang := range helpLanguages() {
		for _, name := range helpNames(lang.Code) {
			text, err := helpPage(lang.Code, name)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range link.FindAllStringSubmatch(string(text), -1) {
				if _, err := helpFS.ReadFile("help/" + lang.Code + "/" + m[1]); err != nil {
					t.Errorf("%s/%s links %s, which is not a help page", lang.Code, name, m[1])
				}
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
	for _, want := range []string{`id="help-toggle" data-pane="help"`, `id="help-pane" data-pane="help"`, `id="help-divider"`, `id="help"`, `id="help-lang"`} {
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
