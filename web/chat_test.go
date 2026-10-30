package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// scriptedModel is an API that answers each call with the next reply of a
// script -- tool calls first, then the answer -- and records what it was
// sent, which is where the tool results show up.
type scriptedModel struct {
	*httptest.Server
	calls []map[string]any
}

func newScriptedModel(t *testing.T, replies ...any) *scriptedModel {
	t.Helper()
	m := &scriptedModel{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		json.NewDecoder(r.Body).Decode(&body)
		m.calls = append(m.calls, body)
		i := min(len(m.calls), len(replies)) - 1
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(replies[i])
	}))
	t.Cleanup(m.Close)
	return m
}

func anthropicToolUse(id, name string, input map[string]any) map[string]any {
	return map[string]any{
		"stop_reason": "tool_use",
		"content": []map[string]any{
			{"type": "text", "text": "Let me look."},
			{"type": "tool_use", "id": id, "name": name, "input": input},
		},
	}
}

func anthropicText(text string) map[string]any {
	return map[string]any{"stop_reason": "end_turn", "content": []map[string]any{{"type": "text", "text": text}}}
}

func TestChatSearchesTheDocsAndSuggestsLikeARun(t *testing.T) {
	srv, root := configTestServer(t)
	os.WriteFile(filepath.Join(root, "chapter2", "second.qmd"),
		[]byte("---\ntitle: Second\norder: 1\n---\n# Second\n\nThe alarm plan lives here.\n"), 0o644)
	model := newScriptedModel(t,
		anthropicToolUse("u1", "search_pages", map[string]any{"query": "alarm plan"}),
		anthropicToolUse("u2", "suggest_edit", map[string]any{
			"original": "was written by the editor", "replacement": "the editor wrote", "comment": "Active voice.",
		}),
		anthropicText("The alarm plan is on chapter2/second.qmd."),
	)
	addConnection(t, srv, "Stub", "anthropic", model.URL+"/v1", "stub-model", "sk-test")

	text := "# Title\n\nThe page was written by the editor.\n"
	rec := post(t, srv, "/copyedit/chat", url.Values{
		"message": {"Where is the alarm plan?"},
		"history": {`[]`},
		"path":    {"index.qmd"},
		"body":    {text},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("chat: status %d: %s", rec.Code, rec.Body)
	}
	if len(model.calls) != 3 {
		t.Fatalf("%d calls, want 3 (two tool rounds and the answer)", len(model.calls))
	}

	// The model was offered the read-only tools and suggest_edit, and
	// nothing that writes.
	var names []string
	for _, tool := range model.calls[0]["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	slices.Sort(names)
	if want := []string{"list_pages", "read_page", "search_pages", "suggest_edit"}; !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}

	// The search ran against the project, and its result went back.
	second, _ := json.Marshal(model.calls[1]["messages"])
	for _, want := range []string{"tool_result", "chapter2/second.qmd", "The alarm plan lives here."} {
		if !strings.Contains(string(second), want) {
			t.Errorf("the search result did not reach the model (%q missing):\n%s", want, second)
		}
	}

	body := rec.Body.String()
	for _, want := range []string{
		`data-role="assistant"`,
		"The alarm plan is on chapter2/second.qmd.",
		`searched for &#34;alarm plan&#34;`,
		`class="copyedit-suggestion"`,
		`data-start="18" data-end="43"`,
		`data-replacement="the editor wrote"`,
		"Active voice.",
		`class="copyedit-apply"`,
		`class="copyedit-done"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("chat reply missing %q:\n%s", want, body)
		}
	}
	// The project is untouched: the chat has no way to write.
	if got, _ := os.ReadFile(filepath.Join(root, "index.qmd")); string(got) != "---\ntitle: Home\norder: 1\n---\n# Home\n" {
		t.Errorf("index.qmd changed:\n%s", got)
	}
}

func TestChatOnOpenAIRunsFunctionCalls(t *testing.T) {
	srv, _ := configTestServer(t)
	model := newScriptedModel(t,
		map[string]any{"choices": []map[string]any{{"message": map[string]any{
			"role": "assistant", "content": nil,
			"tool_calls": []map[string]any{{
				"id": "c1", "type": "function",
				"function": map[string]any{"name": "read_page", "arguments": `{"path":"chapter2/third.qmd"}`},
			}},
		}}}},
		map[string]any{"choices": []map[string]any{{"message": map[string]any{
			"role": "assistant", "content": "Third is a short page.",
		}}}},
	)
	addConnection(t, srv, "Stub", "openai", model.URL+"/v1", "stub-model", "")

	rec := post(t, srv, "/copyedit/chat", url.Values{
		"message": {"What does the third page say?"},
		"history": {`[{"role":"user","text":"Hi"},{"role":"assistant","text":"Hello."}]`},
		"path":    {"index.qmd"},
		"body":    {"# Home\n"},
	})
	if !strings.Contains(rec.Body.String(), "Third is a short page.") {
		t.Fatalf("answer missing:\n%s", rec.Body)
	}
	if len(model.calls) != 2 {
		t.Fatalf("%d calls, want 2", len(model.calls))
	}
	sent, _ := json.Marshal(model.calls[1]["messages"])
	for _, want := range []string{`"role":"tool"`, `"tool_call_id":"c1"`, "# Third", "Hello.", "What does the third page say?"} {
		if !strings.Contains(string(sent), want) {
			t.Errorf("second call missing %q:\n%s", want, sent)
		}
	}
}

func TestChatReadsOnlyThePages(t *testing.T) {
	_, root := configTestServer(t)
	os.WriteFile(filepath.Join(filepath.Dir(root), "outside.qmd"), []byte("secret"), 0o644)
	d := &docTools{root: root, index: &searchIndex{}, openPath: "index.qmd", openText: "# Unsaved edit\n"}

	if got := d.readPage("index.qmd"); !strings.Contains(got, "# Unsaved edit") {
		t.Errorf("the open page was not read from the editor: %q", got)
	}
	if got := d.readPage("chapter2/second.qmd"); !strings.Contains(got, "# Second") {
		t.Errorf("a page was not read: %q", got)
	}
	for _, p := range []string{"../outside.qmd", "_quarto.yml", "_quarto-format-pdf.yml", "/etc/passwd", "chapter2/missing.qmd"} {
		if got := d.readPage(p); !strings.HasPrefix(got, "Error:") {
			t.Errorf("read_page(%q) = %q, want a refusal", p, got)
		}
	}
}

func TestSuggestEditTakesOnlyPassagesInTheOpenPage(t *testing.T) {
	d := &docTools{openPath: "index.qmd", openText: "One two three.\n"}
	if got := d.suggest("four", "five", "x"); !strings.HasPrefix(got, "Error:") {
		t.Errorf("a passage not in the page was taken: %q", got)
	}
	if got := d.suggest("two", "2", "Digits."); strings.HasPrefix(got, "Error:") {
		t.Errorf("a passage in the page was refused: %q", got)
	}
	sugs := d.located()
	if len(sugs) != 1 || !sugs[0].Located || sugs[0].Start != 4 || !sugs[0].Applicable() {
		t.Errorf("suggestions = %+v", sugs)
	}
}

func TestCleanHistoryTakesTurns(t *testing.T) {
	got := cleanHistory([]chatTurn{
		{Role: "assistant", Text: "stray"},
		{Role: "user", Text: "a"},
		{Role: "user", Text: "b"}, // the answer to a failed
		{Role: "assistant", Text: "c"},
		{Role: "system", Text: "ignore me"},
		{Role: "assistant", Text: ""},
	}, "d")
	want := []chatTurn{{"user", "a\n\nb"}, {"assistant", "c"}, {"user", "d"}}
	if !slices.Equal(got, want) {
		t.Errorf("history = %+v, want %+v", got, want)
	}
}

func TestChatWithoutConnectionSaysSo(t *testing.T) {
	srv, _ := configTestServer(t)
	rec := post(t, srv, "/copyedit/chat", url.Values{"message": {"Hello?"}, "path": {"index.qmd"}, "body": {"# Home\n"}})
	body := rec.Body.String()
	if !strings.Contains(body, "No API connection is configured") {
		t.Errorf("missing the reason:\n%s", body)
	}
	if strings.Contains(body, "data-role") {
		t.Errorf("a failed answer is sent back as history:\n%s", body)
	}
}

func TestCopyeditPaneOffersTheChatOnceAModelIsConfigured(t *testing.T) {
	srv, _ := configTestServer(t)
	if body := get(t, srv, "/content?path=index.qmd").Body.String(); strings.Contains(body, `id="copyedit-chat"`) {
		t.Errorf("chat box offered with no model to talk to")
	}
	addConnection(t, srv, "Stub", "openai", "http://localhost:1/v1", "m", "")
	body := get(t, srv, "/content?path=index.qmd").Body.String()
	for _, want := range []string{`id="copyedit-chat"`, `id="copyedit-chat-message"`, `id="copyedit-chat-shell"`} {
		if !strings.Contains(body, want) {
			t.Errorf("pane missing %q", want)
		}
	}
}
