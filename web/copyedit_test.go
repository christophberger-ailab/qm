package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// addPrompt and addConnection are what a user does on the two config
// pages, done from a test.
func addPrompt(t *testing.T, srv *server, title, prompt string) {
	t.Helper()
	rec := post(t, srv, "/config/copyedit", url.Values{"title": {title}, "prompt": {prompt}})
	if rec.Code != http.StatusOK {
		t.Fatalf("add prompt: status %d: %s", rec.Code, rec.Body)
	}
}

func addConnection(t *testing.T, srv *server, name, kind, base, model, key string) {
	t.Helper()
	rec := post(t, srv, "/config/connections", url.Values{
		"name": {name}, "kind": {kind}, "base_url": {base}, "model": {model}, "key": {key},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add connection: status %d: %s", rec.Code, rec.Body)
	}
}

func TestConfigPageListsCopyeditEntries(t *testing.T) {
	srv, _ := configTestServer(t)
	body := get(t, srv, "/config").Body.String()
	for _, want := range []string{
		"Copyedit: Editing tasks",
		`href="/config/copyedit"`,
		"Copyedit: API connections",
		`href="/config/connections"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("config page missing %q:\n%s", want, body)
		}
	}
}

func TestPromptsAreAddedEditedAndDeleted(t *testing.T) {
	srv, _ := configTestServer(t)
	addPrompt(t, srv, "Passive voice", "Rewrite passive sentences actively.")

	body := get(t, srv, "/config/copyedit").Body.String()
	for _, want := range []string{"Passive voice", "Rewrite passive sentences actively.", `value="p1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("editing tasks page missing %q:\n%s", want, body)
		}
	}

	// The setup outlives the run: a second server reading the same config
	// directory finds the task.
	again, err := newServer(srv.prefsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.copyedit.Prompts; len(got) != 1 || got[0].Title != "Passive voice" {
		t.Fatalf("reloaded prompts = %+v, want the one just added", got)
	}

	rec := post(t, srv, "/config/copyedit", url.Values{
		"id": {"p1"}, "title": {"Active voice"}, "prompt": {"Rewrite them actively."},
	})
	if body := rec.Body.String(); !strings.Contains(body, `value="Active voice"`) {
		t.Errorf("the edited title is not on the page:\n%s", body)
	}
	if got := srv.copyedit.Prompts; len(got) != 1 || got[0].Title != "Active voice" || got[0].Prompt != "Rewrite them actively." {
		t.Fatalf("prompts after the edit = %+v, want the one task, edited", got)
	}

	rec = post(t, srv, "/config/copyedit/delete", url.Values{"id": {"p1"}})
	if len(srv.copyedit.Prompts) != 0 {
		t.Fatalf("task not deleted: %+v", srv.copyedit.Prompts)
	}
	if !strings.Contains(rec.Body.String(), "Deleted.") {
		t.Errorf("delete not reported:\n%s", rec.Body)
	}
}

func TestPromptNeedsTitleAndText(t *testing.T) {
	srv, _ := configTestServer(t)
	for _, form := range []url.Values{
		{"title": {""}, "prompt": {"something"}},
		{"title": {"Nameless"}, "prompt": {"  "}},
	} {
		rec := post(t, srv, "/config/copyedit", form)
		if !strings.Contains(rec.Body.String(), "needs a") {
			t.Errorf("%v was accepted:\n%s", form, rec.Body)
		}
	}
	if len(srv.copyedit.Prompts) != 0 {
		t.Fatalf("an incomplete task was stored: %+v", srv.copyedit.Prompts)
	}
}

func TestConnectionsAreSavedWithoutLeakingTheKey(t *testing.T) {
	srv, _ := configTestServer(t)
	addConnection(t, srv, "Claude", "anthropic", "https://api.anthropic.com/v1", "claude-sonnet-4-5", "sk-secret")

	body := get(t, srv, "/config/connections").Body.String()
	for _, want := range []string{"Claude", "claude-sonnet-4-5", "https://api.anthropic.com/v1", "stored — leave empty to keep it"} {
		if !strings.Contains(body, want) {
			t.Errorf("connections page missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "sk-secret") {
		t.Fatalf("the API key was sent back to the browser:\n%s", body)
	}

	// The key file is the user's own.
	info, err := os.Stat(srv.copyeditFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("copyedit.json mode = %o, want 600", perm)
	}

	// An edit that leaves the key field empty keeps the stored key.
	post(t, srv, "/config/connections", url.Values{
		"id": {"c1"}, "name": {"Claude"}, "kind": {"anthropic"},
		"base_url": {"https://api.anthropic.com/v1"}, "model": {"claude-opus-4-1"}, "key": {""},
	})
	if got := srv.copyedit.Connections[0]; got.Key != "sk-secret" || got.Model != "claude-opus-4-1" {
		t.Fatalf("connection after edit = %+v, want the new model and the stored key", got)
	}

	post(t, srv, "/config/connections/delete", url.Values{"id": {"c1"}})
	if len(srv.copyedit.Connections) != 0 {
		t.Fatalf("connection not deleted: %+v", srv.copyedit.Connections)
	}
}

func TestActiveConnectionIsRememberedAndFallsBack(t *testing.T) {
	srv, _ := configTestServer(t)
	addConnection(t, srv, "First", "openai", "http://localhost:11434/v1", "llama", "")
	addConnection(t, srv, "Second", "anthropic", "https://api.anthropic.com/v1", "claude-sonnet-4-5", "k")

	// Nothing selected yet: the first connection answers.
	if conn, _ := srv.activeConnection(); conn.Name != "First" {
		t.Errorf("active connection = %q, want the first one", conn.Name)
	}

	rec := post(t, srv, "/copyedit/active", url.Values{"connection": {"c2"}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("select: status %d: %s", rec.Code, rec.Body)
	}
	if conn, _ := srv.activeConnection(); conn.Name != "Second" {
		t.Errorf("active connection = %q, want the selected one", conn.Name)
	}
	if rec := post(t, srv, "/copyedit/active", url.Values{"connection": {"nope"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("selecting an unknown connection: status %d, want 400", rec.Code)
	}

	// Deleting the selected one falls back rather than leaving a dangling
	// selection behind.
	post(t, srv, "/config/connections/delete", url.Values{"id": {"c2"}})
	if conn, ok := srv.activeConnection(); !ok || conn.Name != "First" {
		t.Errorf("active connection after delete = %+v, want the remaining one", conn)
	}
}

func TestEditorPaneCarriesTabsAndCopyeditTab(t *testing.T) {
	srv, _ := configTestServer(t)
	addPrompt(t, srv, "Passive voice", "Rewrite passive sentences actively.")
	addConnection(t, srv, "Claude", "anthropic", "https://api.anthropic.com/v1", "claude-sonnet-4-5", "sk-secret")

	body := get(t, srv, "/content?path=index.qmd").Body.String()
	for _, want := range []string{
		`data-tab="preview"`,
		`data-tab="copyedit"`,
		`<article class="markdown-preview pane-panel" id="preview"`,
		`id="copyedit"`,
		`id="copyedit-connection"`,
		"Claude (claude-sonnet-4-5)",
		`class="copyedit-task" data-id="p1"`,
		"Passive voice",
		`hx-post="/copyedit/run"`,
		`hx-include="#content-path, #content textarea.file-content"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editor pane missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "sk-secret") {
		t.Fatalf("the API key reached the editor pane:\n%s", body)
	}

	// The app page restores the same pane, so it carries the tab too.
	if page := get(t, srv, "/").Body.String(); !strings.Contains(page, `data-tab="copyedit"`) {
		t.Errorf("app page's restored editor has no copyedit tab:\n%s", page)
	}
}

func TestCopyeditPaneWithoutSetupSaysWhereToConfigureIt(t *testing.T) {
	srv, _ := configTestServer(t)
	body := get(t, srv, "/content?path=index.qmd").Body.String()
	for _, want := range []string{
		`href="/config/connections"`,
		`href="/config/copyedit"`,
		"No editing tasks yet",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("empty copyedit tab missing %q:\n%s", want, body)
		}
	}
}

// stubModel is an API standing in for the real one: it records the request
// it was given and answers with the suggestions the test names.
type stubModel struct {
	*httptest.Server
	path   string
	body   map[string]any
	header http.Header
}

func newStubModel(t *testing.T, answer string) *stubModel {
	t.Helper()
	stub := &stubModel{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.path = r.URL.Path
		stub.header = r.Header.Clone()
		json.NewDecoder(r.Body).Decode(&stub.body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": answer}},
		})
	}))
	t.Cleanup(stub.Close)
	return stub
}

func TestCopyeditRunListsSuggestionsWithTheirPlaceInTheText(t *testing.T) {
	srv, _ := configTestServer(t)
	addPrompt(t, srv, "Passive voice", "Rewrite passive sentences actively.")
	stub := newStubModel(t, `{"suggestions":[
		{"original":"was written by the editor","suggestion":"the editor wrote","comment":"Passive."},
		{"original":"nowhere in the page","suggestion":"—","comment":"Not there."}]}`)
	addConnection(t, srv, "Stub", "anthropic", stub.URL+"/v1", "stub-model", "sk-test")

	text := "# Title\n\nThe page was written by the editor.\n"
	rec := post(t, srv, "/copyedit/run", url.Values{
		"prompt": {"p1"}, "path": {"index.qmd"}, "body": {text},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("run: status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()

	// What the model was asked.
	if stub.path != "/v1/messages" {
		t.Errorf("posted to %q, want the Anthropic messages endpoint", stub.path)
	}
	if got := stub.header.Get("x-api-key"); got != "sk-test" {
		t.Errorf("x-api-key = %q, want the configured key", got)
	}
	if got, _ := stub.body["model"].(string); got != "stub-model" {
		t.Errorf("model = %q, want the configured one", got)
	}
	sent, _ := json.Marshal(stub.body)
	for _, want := range []string{"Rewrite passive sentences actively.", "index.qmd", "The page was written by the editor."} {
		if !strings.Contains(string(sent), want) {
			t.Errorf("the model was not given %q:\n%s", want, sent)
		}
	}

	// What came back, and where in the text it belongs. "was written by
	// the editor" starts at offset 18 of the text above.
	start := strings.Index(text, "was written by the editor")
	for _, want := range []string{
		`hx-get="/copyedit/prompts"`, // the way back to the task list
		"Passive voice",
		"the editor wrote",
		"Passive.",
		`data-start="18" data-end="43"`,
		`class="copyedit-suggestion unlocated"`,
		"Not found in the page as quoted",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("suggestion list missing %q:\n%s", want, body)
		}
	}
	if start != 18 {
		t.Fatalf("the fixture text moved: %q starts at %d", "was written by the editor", start)
	}
}

func TestCopyeditRunReportsWhatWentWrong(t *testing.T) {
	srv, _ := configTestServer(t)
	addPrompt(t, srv, "Passive voice", "Rewrite passive sentences actively.")

	// No connection configured yet.
	rec := post(t, srv, "/copyedit/run", url.Values{"prompt": {"p1"}, "body": {"# Page\n"}})
	if !strings.Contains(rec.Body.String(), "No API connection is configured") {
		t.Errorf("a run without a connection did not say so:\n%s", rec.Body)
	}

	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
	}))
	defer refusing.Close()
	addConnection(t, srv, "Stub", "anthropic", refusing.URL+"/v1", "stub-model", "bad")

	rec = post(t, srv, "/copyedit/run", url.Values{"prompt": {"p1"}, "body": {"# Page\n"}})
	if body := rec.Body.String(); !strings.Contains(body, "invalid api key") {
		t.Errorf("the API's refusal was not shown:\n%s", body)
	}

	rec = post(t, srv, "/copyedit/run", url.Values{"prompt": {"nope"}, "body": {"# Page\n"}})
	if !strings.Contains(rec.Body.String(), "no such editing task") {
		t.Errorf("an unknown task was not reported:\n%s", rec.Body)
	}
}

func TestCopyeditPromptsFragmentIsTheWayBack(t *testing.T) {
	srv, _ := configTestServer(t)
	addPrompt(t, srv, "Passive voice", "Rewrite passive sentences actively.")
	rec := get(t, srv, "/copyedit/prompts")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="copyedit-task" data-id="p1"`) {
		t.Errorf("the task list is not what the Back button gets:\n%s", body)
	}
	if strings.Contains(body, "copyedit-suggestion") {
		t.Errorf("the task list still carries suggestions:\n%s", body)
	}
}

func TestOpenAIConnectionPostsChatCompletions(t *testing.T) {
	srv, _ := configTestServer(t)
	addPrompt(t, srv, "Spelling", "Find misspellings.")
	var path string
	var auth string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{
			{"message": map[string]string{"content": `{"suggestions":[{"original":"teh","suggestion":"the"}]}`}},
		}})
	}))
	defer stub.Close()
	addConnection(t, srv, "Local", "openai", stub.URL+"/v1", "llama3", "sk-local")

	rec := post(t, srv, "/copyedit/run", url.Values{"prompt": {"p1"}, "body": {"teh page\n"}})
	if path != "/v1/chat/completions" {
		t.Errorf("posted to %q, want the chat completions endpoint", path)
	}
	if auth != "Bearer sk-local" {
		t.Errorf("Authorization = %q, want the bearer key", auth)
	}
	if body := rec.Body.String(); !strings.Contains(body, `data-start="0" data-end="3"`) {
		t.Errorf("suggestion not located in the text:\n%s", body)
	}
}

func TestOnlyASuggestionThatCanBeWrittenInOffersToBe(t *testing.T) {
	srv, _ := configTestServer(t)
	addPrompt(t, srv, "Passive voice", "Rewrite passive sentences actively.")
	stub := newStubModel(t, `{"suggestions":[
		{"original":"was written by the editor","suggestion":"the editor wrote","comment":"Passive."},
		{"original":"The page","comment":"Vague, but I have no better word."},
		{"original":"nowhere in the page","suggestion":"—","comment":"Not there."}]}`)
	addConnection(t, srv, "Stub", "anthropic", stub.URL+"/v1", "stub-model", "sk-test")

	body := post(t, srv, "/copyedit/run", url.Values{
		"prompt": {"p1"}, "path": {"index.qmd"},
		"body": {"# Title\n\nThe page was written by the editor.\n"},
	}).Body.String()

	// The one that was found and says what to put there instead carries
	// the text to write and the button to write it.
	if !strings.Contains(body, `data-replacement="the editor wrote"`) {
		t.Errorf("the replacement text is not on the entry:\n%s", body)
	}
	if n := strings.Count(body, `class="copyedit-apply"`); n != 1 {
		t.Errorf("%d Apply buttons, want 1 — a comment-only and an unlocated suggestion have nothing to apply:\n%s", n, body)
	}
}

func TestReplacementTextSurvivesAsAnAttribute(t *testing.T) {
	srv, _ := configTestServer(t)
	addPrompt(t, srv, "Quotes", "Fix the quoting.")
	// A replacement carrying the characters an attribute is delimited and
	// escaped with, and a line break: what the button writes into the page
	// must be what the model said, not markup.
	stub := newStubModel(t, `{"suggestions":[{"original":"said hi","suggestion":"said \"hi\" & <waved>\nwarmly"}]}`)
	addConnection(t, srv, "Stub", "anthropic", stub.URL+"/v1", "stub-model", "k")

	body := post(t, srv, "/copyedit/run", url.Values{
		"prompt": {"p1"}, "body": {"She said hi.\n"},
	}).Body.String()
	want := "data-replacement=\"said &#34;hi&#34; &amp; &lt;waved&gt;\nwarmly\""
	if !strings.Contains(body, want) {
		t.Errorf("the replacement did not reach the attribute intact:\n%s", body)
	}
}
