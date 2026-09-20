package web

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// oneTask is the run a click on a single editing task makes.
var oneTask = []editingTask{{ID: "t1", Title: "Passive voice", Prompt: "Rewrite passive sentences actively."}}

func TestDecodeSuggestionsReadsWhatModelsActuallySend(t *testing.T) {
	cases := map[string]string{
		"plain JSON":    `{"suggestions":[{"original":"a cat","suggestion":"the cat","comment":"Definite."}]}`,
		"fenced JSON":   "Here you go:\n```json\n{\"suggestions\":[{\"original\":\"a cat\",\"suggestion\":\"the cat\",\"comment\":\"Definite.\"}]}\n```\n",
		"JSON in prose": "I found one thing.\n{\"suggestions\":[{\"original\":\"a cat\",\"replacement\":\"the cat\",\"reason\":\"Definite.\"}]}",
	}
	for name, answer := range cases {
		got, err := decodeSuggestions(answer, oneTask)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 1 {
			t.Fatalf("%s: %d suggestions, want 1", name, len(got))
		}
		if got[0].Original != "a cat" || got[0].Replacement != "the cat" || got[0].Comment != "Definite." {
			t.Errorf("%s: suggestion = %+v", name, got[0])
		}
	}
}

func TestDecodeSuggestionsDropsWhatCannotBePointedAt(t *testing.T) {
	got, err := decodeSuggestions(`{"suggestions":[{"original":"","suggestion":"x"},{"original":"here","suggestion":"there"}]}`, oneTask)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Original != "here" {
		t.Errorf("suggestions = %+v, want only the one naming a passage", got)
	}
}

func TestDecodeSuggestionsRefusesProse(t *testing.T) {
	if _, err := decodeSuggestions("The page reads well; I would change nothing.", oneTask); err == nil {
		t.Fatal("prose was accepted as an answer")
	}
}

func TestLocateAllFindsPassagesAndSaysWhenItCannot(t *testing.T) {
	text := "# Title\n\nThe door was opened by Anna.\nShe was seen by nobody.\n"
	got := locateAll(text, []suggestion{
		{Original: "was opened by Anna"},
		{Original: "was seen by nobody"},
		{Original: "was eaten by the dog"},
	})
	for i, want := range []string{"was opened by Anna", "was seen by nobody"} {
		if !got[i].Located {
			t.Fatalf("%q was not located", want)
		}
		if at := text[byteOffset(text, got[i].Start):]; !strings.HasPrefix(at, want) {
			t.Errorf("%q located at %d, which is %.20q", want, got[i].Start, at)
		}
	}
	if got[2].Located {
		t.Errorf("a passage the page does not contain was located at %d", got[2].Start)
	}
}

// byteOffset converts an offset in UTF-16 code units back to a byte index,
// which is what the test needs to look at the text the offset names.
func byteOffset(text string, want int) int {
	n := 0
	for i, r := range text {
		if n == want {
			return i
		}
		n += utf16Len(string(r))
	}
	return len(text)
}

func TestLocateToleratesAReWrappedQuote(t *testing.T) {
	text := "The report was written\nby the committee last spring.\n"
	got := locateAll(text, []suggestion{{Original: "was written by the committee"}})
	if !got[0].Located {
		t.Fatal("a quote spanning a line break was not located")
	}
	from, to := byteOffset(text, got[0].Start), byteOffset(text, got[0].End)
	if text[from:to] != "was written\nby the committee" {
		t.Errorf("located %q, want the passage across the break", text[from:to])
	}
}

func TestLocateAllGivesRepeatedPassagesTheirOwnPlace(t *testing.T) {
	text := "It was done. Then it was done again.\n"
	got := locateAll(text, []suggestion{{Original: "was done"}, {Original: "was done"}})
	if !got[0].Located || !got[1].Located {
		t.Fatalf("both occurrences should be located: %+v", got)
	}
	if got[0].Start == got[1].Start {
		t.Errorf("two suggestions about the same wording mark the same place (%d)", got[0].Start)
	}
}

func TestLocateCountsPositionsTheWayABrowserDoes(t *testing.T) {
	// An emoji is one rune but two UTF-16 code units, which is what a
	// browser counts string positions in — and what the editor marks by.
	text := "🚒 the fire brigade\n"
	got := locateAll(text, []suggestion{{Original: "the fire brigade"}})
	if !got[0].Located {
		t.Fatal("not located")
	}
	if got[0].Start != 3 || got[0].End != 19 {
		t.Errorf("start, end = %d, %d; want 3, 19 (the emoji counting as two)", got[0].Start, got[0].End)
	}
}

func TestRequestForAddressesEachAPIItsOwnWay(t *testing.T) {
	anthropic := apiConnection{Kind: kindAnthropic, BaseURL: "https://api.anthropic.com/v1/", Model: "claude"}
	url, body, err := requestFor(anthropic, "system", "the page", "the tasks", 1)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://api.anthropic.com/v1/messages" {
		t.Errorf("url = %q", url)
	}
	var payload map[string]any
	json.Unmarshal(body, &payload)
	if payload["system"] != "system" {
		t.Errorf("the system prompt is not a field of its own: %v", payload)
	}

	openai := apiConnection{Kind: kindOpenAI, BaseURL: "http://localhost:11434/v1", Model: "llama"}
	url, body, err = requestFor(openai, "system", "the page", "the tasks", 1)
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://localhost:11434/v1/chat/completions" {
		t.Errorf("url = %q", url)
	}
	if !strings.Contains(string(body), `"role":"system"`) {
		t.Errorf("the system prompt is not a message: %s", body)
	}

	if _, _, err := requestFor(apiConnection{Kind: kindOpenAI, Model: "m"}, "s", "p", "t", 1); err == nil {
		t.Error("a connection without a base URL was accepted")
	}
}

func TestTheWholeEndpointIsAcceptedWhereTheBaseURLIsAskedFor(t *testing.T) {
	// A provider's documentation prints the endpoint, not the base it is
	// composed from, so that is what gets pasted into the field; the path
	// must not be appended to it a second time.
	cases := []struct {
		kind, base, want string
	}{
		{kindOpenAI, "https://openrouter.ai/api/v1", "https://openrouter.ai/api/v1/chat/completions"},
		{kindOpenAI, "https://openrouter.ai/api/v1/chat/completions", "https://openrouter.ai/api/v1/chat/completions"},
		{kindOpenAI, "https://openrouter.ai/api/v1/chat/completions/", "https://openrouter.ai/api/v1/chat/completions"},
		{kindAnthropic, "https://api.anthropic.com/v1", "https://api.anthropic.com/v1/messages"},
		{kindAnthropic, "https://api.anthropic.com/v1/messages", "https://api.anthropic.com/v1/messages"},
	}
	for _, c := range cases {
		got, _, err := requestFor(apiConnection{Kind: c.kind, BaseURL: c.base, Model: "m"}, "s", "p", "t", 1)
		if err != nil {
			t.Fatalf("%s: %v", c.base, err)
		}
		if got != c.want {
			t.Errorf("base %q -> %q, want %q", c.base, got, c.want)
		}
	}
}

func TestAnswerTextDigsOutBothShapes(t *testing.T) {
	got, err := answerText(kindAnthropic, []byte(`{"content":[{"type":"text","text":"one"},{"type":"text","text":" two"}]}`))
	if err != nil || got != "one two" {
		t.Errorf("anthropic answer = %q, %v", got, err)
	}
	got, err = answerText(kindOpenAI, []byte(`{"choices":[{"message":{"content":"one"}}]}`))
	if err != nil || got != "one" {
		t.Errorf("openai answer = %q, %v", got, err)
	}
	if _, err := answerText(kindOpenAI, []byte(`{"choices":[]}`)); err == nil {
		t.Error("an answer with no text was accepted")
	}
}

func TestRunCopyeditRefusesAnEmptyPage(t *testing.T) {
	conn := apiConnection{Kind: kindAnthropic, BaseURL: "https://example.invalid/v1", Model: "m"}
	if _, err := runCopyedit(conn, oneTask, "index.qmd", "   \n"); err == nil {
		t.Fatal("an empty page was sent to the model")
	}
}

func TestThePageComesBeforeTheTasksAndCarriesTheCacheBreakpoint(t *testing.T) {
	// The page is the same for every task run against it and the tasks
	// are not, so the page goes first: a prompt cache matches on a
	// request's prefix, and a page sitting behind the part that changes
	// every run could never be cached.
	page, tasks := "The page (index.qmd):\n\n# Title", "Editing tasks:\n\n[t1] Passive voice"

	_, body, err := requestFor(apiConnection{Kind: kindAnthropic, BaseURL: "https://x/v1", Model: "m"}, "system", page, tasks, 1)
	if err != nil {
		t.Fatal(err)
	}
	var anth struct {
		Messages []struct {
			Content []struct {
				Text         string         `json:"text"`
				CacheControl map[string]any `json:"cache_control"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &anth); err != nil {
		t.Fatal(err)
	}
	blocks := anth.Messages[0].Content
	if len(blocks) != 2 {
		t.Fatalf("%d content blocks, want the page and the tasks apart: %s", len(blocks), body)
	}
	if blocks[0].Text != page || blocks[1].Text != tasks {
		t.Errorf("blocks are not page-then-tasks: %s", body)
	}
	if blocks[0].CacheControl == nil {
		t.Errorf("the page is not marked as the cacheable prefix: %s", body)
	}
	if blocks[1].CacheControl != nil {
		t.Errorf("the tasks are inside the cached prefix, which defeats it: %s", body)
	}

	_, body, err = requestFor(apiConnection{Kind: kindOpenAI, BaseURL: "https://x/v1", Model: "m"}, "system", page, tasks, 1)
	if err != nil {
		t.Fatal(err)
	}
	var oai struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &oai); err != nil {
		t.Fatal(err)
	}
	user := oai.Messages[1].Content
	if i, j := strings.Index(user, page), strings.Index(user, tasks); i < 0 || j < 0 || i > j {
		t.Errorf("the user message is not page-then-tasks: %q", user)
	}
}

func TestTheAnswerBudgetGrowsWithTheNumberOfTasks(t *testing.T) {
	// A run of five tasks has five tasks' findings to report, and an
	// answer cut off mid-JSON loses the whole run, not one task of it.
	one, five := answerBudget(1), answerBudget(5)
	if one != maxTokens {
		t.Errorf("one task budgets %d, want the base %d", one, maxTokens)
	}
	if five <= one {
		t.Errorf("five tasks budget %d, no more than one task's %d", five, one)
	}
	if answerBudget(100) > 65536 {
		t.Errorf("the budget is unbounded: %d", answerBudget(100))
	}
	_, body, err := requestFor(apiConnection{Kind: kindOpenAI, BaseURL: "https://x/v1", Model: "m"}, "s", "p", "t", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"max_tokens":`+itoa(five)) {
		t.Errorf("the request does not carry the budget for five tasks: %s", body)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestSuggestionsAreAttributedToTheTaskTheyName(t *testing.T) {
	tasks := []editingTask{
		{ID: "t1", Title: "Passive voice", Prompt: "..."},
		{ID: "t2", Title: "Long sentences", Prompt: "..."},
	}
	got, err := decodeSuggestions(`{"suggestions":[
		{"task":"t1","original":"a","suggestion":"b"},
		{"task":"[t2]","original":"c","suggestion":"d"},
		{"task":"Long sentences","original":"e","suggestion":"f"},
		{"task":"t9","original":"g","suggestion":"h"},
		{"original":"i","suggestion":"j"}]}`, tasks)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Passive voice", "Long sentences", "Long sentences", "", ""}
	for i, w := range want {
		if got[i].Task != w {
			t.Errorf("suggestion %d attributed to %q, want %q", i, got[i].Task, w)
		}
	}
}

func TestASingleTaskNeedsNoTagToBeAttributed(t *testing.T) {
	// With one task there is only one task a suggestion can be from, so a
	// model that left the tag off is understood anyway.
	got, err := decodeSuggestions(`{"suggestions":[{"original":"a","suggestion":"b"}]}`, oneTask)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Task != "Passive voice" {
		t.Errorf("task = %q, want the only one that was run", got[0].Task)
	}
}

func TestTaskBlockNumbersTheTasksItSends(t *testing.T) {
	block := taskBlock(tasksFor([]copyeditPrompt{
		{ID: "p3", Title: "Passive voice", Prompt: "Rewrite them actively."},
		{ID: "p7", Title: "Long sentences", Prompt: "Find the long ones."},
	}))
	for _, want := range []string{"[t1] Passive voice", "Rewrite them actively.", "[t2] Long sentences", "Find the long ones."} {
		if !strings.Contains(block, want) {
			t.Errorf("task block missing %q:\n%s", want, block)
		}
	}
	// The config's own ids are the app's business, not the model's.
	if strings.Contains(block, "p3") || strings.Contains(block, "p7") {
		t.Errorf("the config's prompt ids were sent to the model:\n%s", block)
	}
}

func TestRunCopyeditRefusesARunWithNoTask(t *testing.T) {
	conn := apiConnection{Kind: kindAnthropic, BaseURL: "https://example.invalid/v1", Model: "m"}
	if _, err := runCopyedit(conn, nil, "index.qmd", "Some text.\n"); err == nil {
		t.Fatal("a run with no task was sent to the model")
	}
}

// What comes back when there is no text differs by provider, and "the
// model answered with no text" told the user none of it. Each of these
// is a shape a run has actually met.
func TestAnAnswerWithoutTextSaysWhatCameBack(t *testing.T) {
	cases := []struct {
		name, kind, body string
		want             []string
	}{{
		name: "a reasoning model that spent the budget thinking",
		kind: kindOpenAI,
		body: `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":""}}],
			"usage":{"completion_tokens":4096}}`,
		want: []string{"whole answer budget", "reasoning model"},
	}, {
		name: "an error the gateway put in a 200 body",
		kind: kindOpenAI,
		body: `{"error":{"message":"No endpoints found for z-ai/glm-5.3.","code":404}}`,
		want: []string{"the API refused the call", "No endpoints found"},
	}, {
		name: "no choices at all",
		kind: kindOpenAI,
		body: `{"id":"gen-1","choices":[]}`,
		want: []string{"no text", "gen-1"},
	}, {
		name: "a stop reason of its own",
		kind: kindOpenAI,
		body: `{"choices":[{"finish_reason":"content_filter","message":{"content":""}}]}`,
		want: []string{"content filter"},
	}, {
		name: "an anthropic answer with no text block",
		kind: kindAnthropic,
		body: `{"content":[],"stop_reason":"max_tokens"}`,
		want: []string{"whole answer budget"},
	}, {
		name: "an anthropic error in a 200 body",
		kind: kindAnthropic,
		body: `{"error":{"message":"overloaded"}}`,
		want: []string{"the API refused the call", "overloaded"},
	}}
	for _, c := range cases {
		_, err := answerText(c.kind, []byte(c.body))
		if err == nil {
			t.Errorf("%s: no error", c.name)
			continue
		}
		for _, want := range c.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error does not mention %q:\n%v", c.name, want, err)
			}
		}
	}
}

// A model that thought its way to the answer and never wrote a final
// message still answered: the JSON is in the reasoning, and reading it
// beats refusing.
func TestReasoningStandsInForAnEmptyAnswer(t *testing.T) {
	for _, field := range []string{"reasoning", "reasoning_content"} {
		body := `{"choices":[{"finish_reason":"stop","message":{"content":"",` +
			`"` + field + `":"{\"suggestions\":[{\"original\":\"a\",\"suggestion\":\"b\"}]}"}}]}`
		got, err := answerText(kindOpenAI, []byte(body))
		if err != nil {
			t.Fatalf("%s: %v", field, err)
		}
		sugs, err := decodeSuggestions(got, oneTask)
		if err != nil || len(sugs) != 1 || sugs[0].Original != "a" {
			t.Errorf("%s: suggestions = %+v, %v", field, sugs, err)
		}
	}
	// Anthropic's own spelling of the same thing.
	got, err := answerText(kindAnthropic, []byte(`{"content":[{"type":"thinking","thinking":"{\"suggestions\":[]}"}]}`))
	if err != nil || got != `{"suggestions":[]}` {
		t.Errorf("thinking block not used: %q, %v", got, err)
	}
}

// Providers send a message's content either as a string or as the list of
// parts the newer shape uses.
func TestContentComesAsAStringOrAsParts(t *testing.T) {
	for _, body := range []string{
		`{"choices":[{"message":{"content":"plain"}}]}`,
		`{"choices":[{"message":{"content":[{"type":"text","text":"pl"},{"type":"text","text":"ain"}]}}]}`,
	} {
		got, err := answerText(kindOpenAI, []byte(body))
		if err != nil || got != "plain" {
			t.Errorf("answer = %q, %v; want \"plain\" from %s", got, err, body)
		}
	}
}

func TestTheAnswerBudgetLeavesRoomToThink(t *testing.T) {
	// A reasoning model counts what it thinks against this ceiling, so a
	// budget sized for the suggestion list alone is what produced an
	// empty answer.
	if answerBudget(1) < 16384 {
		t.Errorf("one task budgets %d, too little for a model that thinks first", answerBudget(1))
	}
}
