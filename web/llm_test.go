package web

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeSuggestionsReadsWhatModelsActuallySend(t *testing.T) {
	cases := map[string]string{
		"plain JSON":    `{"suggestions":[{"original":"a cat","suggestion":"the cat","comment":"Definite."}]}`,
		"fenced JSON":   "Here you go:\n```json\n{\"suggestions\":[{\"original\":\"a cat\",\"suggestion\":\"the cat\",\"comment\":\"Definite.\"}]}\n```\n",
		"JSON in prose": "I found one thing.\n{\"suggestions\":[{\"original\":\"a cat\",\"replacement\":\"the cat\",\"reason\":\"Definite.\"}]}",
	}
	for name, answer := range cases {
		got, err := decodeSuggestions(answer)
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
	got, err := decodeSuggestions(`{"suggestions":[{"original":"","suggestion":"x"},{"original":"here","suggestion":"there"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Original != "here" {
		t.Errorf("suggestions = %+v, want only the one naming a passage", got)
	}
}

func TestDecodeSuggestionsRefusesProse(t *testing.T) {
	if _, err := decodeSuggestions("The page reads well; I would change nothing."); err == nil {
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
	url, body, err := requestFor(anthropic, "system", "user")
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
	url, body, err = requestFor(openai, "system", "user")
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://localhost:11434/v1/chat/completions" {
		t.Errorf("url = %q", url)
	}
	if !strings.Contains(string(body), `"role":"system"`) {
		t.Errorf("the system prompt is not a message: %s", body)
	}

	if _, _, err := requestFor(apiConnection{Kind: kindOpenAI, Model: "m"}, "s", "u"); err == nil {
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
		got, _, err := requestFor(apiConnection{Kind: c.kind, BaseURL: c.base, Model: "m"}, "s", "u")
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
	if _, err := runCopyedit(conn, "task", "index.qmd", "   \n"); err == nil {
		t.Fatal("an empty page was sent to the model")
	}
}
