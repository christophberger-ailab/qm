package web

// llm.go — the copyediting model call.
//
// A copyedit run is one request to a chat API: the editing task the user
// picked, the page the editor holds, and a fixed instruction that asks for
// the answer as JSON. What comes back is a list of suggestions, each one
// quoting the piece of the page it applies to — that quote is what the
// pane highlights in the text, so the suggestion is read where it belongs
// rather than in a list beside the page.
//
// Two request shapes cover what a connection can point at: Anthropic's
// Messages API and the OpenAI-style chat completions every other provider
// (OpenAI, Ollama, LM Studio, OpenRouter, ...) speaks. Both are plain JSON
// over net/http; no client library is vendored for either.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// llmTimeout bounds one copyedit run. A model working through a long page
// takes its time, and the pane waits for it; what it must not do is wait
// forever on a connection that will never answer.
var llmTimeout = 3 * time.Minute

// maxTokens is the answer's budget. It is the whole suggestion list, not
// the page, so it can be far smaller than the input.
const maxTokens = 4096

// The kinds of API a connection can address. The kind decides the request
// shape, the auth header, and where the answer's text sits in the reply.
const (
	kindAnthropic = "anthropic"
	kindOpenAI    = "openai"
)

// suggestionFormat is the part of the instruction that is ours rather than
// the user's: the editing task comes from the prompt the user wrote, the
// shape of the answer comes from here. The pane can only highlight what it
// can find again in the page, so the "original" field is asked for
// verbatim and kept short.
const suggestionFormat = `You are a copy editor working on one page of a Quarto Markdown document.
Apply the editing task below to the page and report what you would change.

Answer with JSON and nothing else, in exactly this shape:

{"suggestions":[{"original":"...","suggestion":"...","comment":"..."}]}

  - "original" is the passage the suggestion applies to, copied from the
    page character for character, so that it can be found in the page
    again. Quote as little as possible: the sentence or the phrase, never a
    whole paragraph or section.
  - "suggestion" is the text you would put in its place.
  - "comment" is one short sentence saying why.

Report only what the editing task asks for. Leave Markdown syntax, YAML
frontmatter, code blocks, and Quarto shortcodes alone unless the task is
about them. If the page needs no change, answer {"suggestions":[]}.`

// suggestion is one piece of advice: the passage it is about, what to put
// there instead, and why. Start and End locate the passage in the page as
// offsets in UTF-16 code units — what a browser counts string positions in
// — so the editor can mark it; Located says whether the passage was found
// at all.
type suggestion struct {
	Original    string
	Replacement string
	Comment     string
	Start       int
	End         int
	Located     bool
}

// llmReply is the JSON the model is asked for. The fields are read
// leniently: models spell the replacement "suggestion", "replacement", or
// "revised", and a run is not worth losing over the word one picked.
type llmReply struct {
	Suggestions []struct {
		Original    string `json:"original"`
		Suggestion  string `json:"suggestion"`
		Replacement string `json:"replacement"`
		Revised     string `json:"revised"`
		Comment     string `json:"comment"`
		Reason      string `json:"reason"`
	} `json:"suggestions"`
}

// runCopyedit asks the connection's model to apply task to the text of the
// page at path and returns the suggestions it made, each located in the
// text. The path travels with the page because it is part of what the page
// is: a Quarto page's name carries its audience (`_FW`, `_POL`) and its
// place in the book, and an editing task may well be about either.
func runCopyedit(conn apiConnection, task, path, text string) ([]suggestion, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("the page is empty, so there is nothing to edit")
	}
	page := "The page"
	if path != "" {
		page += " (" + path + ")"
	}
	answer, err := askModel(conn, suggestionFormat, "Editing task:\n\n"+task+"\n\n"+page+":\n\n"+text)
	if err != nil {
		return nil, err
	}
	sugs, err := decodeSuggestions(answer)
	if err != nil {
		return nil, err
	}
	return locateAll(text, sugs), nil
}

// askModel sends one system/user pair to the connection and returns the
// model's text.
func askModel(conn apiConnection, system, user string) (string, error) {
	if conn.Model == "" {
		return "", errors.New("the connection names no model")
	}
	url, body, err := requestFor(conn, system, user)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	setAuth(req, conn)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling %s: %w", url, err)
	}
	defer resp.Body.Close()
	// The answer is read whatever the status: an API says why it refused
	// in the body, and that sentence is the only useful thing to show.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s: %s", conn.Name, resp.Status, snippet(string(raw)))
	}
	return answerText(conn.Kind, raw)
}

// requestFor composes the endpoint and the request body for the
// connection's API kind.
func requestFor(conn apiConnection, system, user string) (string, []byte, error) {
	base := strings.TrimRight(strings.TrimSpace(conn.BaseURL), "/")
	if base == "" {
		return "", nil, errors.New("the connection names no base URL")
	}
	var payload any
	var url string
	switch conn.Kind {
	case kindAnthropic:
		url = base + "/messages"
		payload = map[string]any{
			"model":      conn.Model,
			"max_tokens": maxTokens,
			"system":     system,
			"messages": []map[string]string{
				{"role": "user", "content": user},
			},
		}
	default: // OpenAI-style, which is what every other provider speaks
		url = base + "/chat/completions"
		payload = map[string]any{
			"model":       conn.Model,
			"max_tokens":  maxTokens,
			"temperature": 0,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
		}
	}
	body, err := json.Marshal(payload)
	return url, body, err
}

// setAuth carries the key the way the API kind expects it. A connection to
// a local model may have no key at all, and then nothing is sent.
func setAuth(req *http.Request, conn apiConnection) {
	key := strings.TrimSpace(conn.Key)
	switch conn.Kind {
	case kindAnthropic:
		req.Header.Set("anthropic-version", "2023-06-01")
		if key != "" {
			req.Header.Set("x-api-key", key)
		}
	default:
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
}

// answerText digs the model's text out of the reply. The two shapes differ
// only in where the text sits.
func answerText(kind string, raw []byte) (string, error) {
	if kind == kindAnthropic {
		var reply struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			return "", fmt.Errorf("unreadable answer: %w", err)
		}
		var text strings.Builder
		for _, part := range reply.Content {
			if part.Type == "text" || part.Type == "" {
				text.WriteString(part.Text)
			}
		}
		if text.Len() == 0 {
			return "", errors.New("the model answered with no text")
		}
		return text.String(), nil
	}
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return "", fmt.Errorf("unreadable answer: %w", err)
	}
	if len(reply.Choices) == 0 || reply.Choices[0].Message.Content == "" {
		return "", errors.New("the model answered with no text")
	}
	return reply.Choices[0].Message.Content, nil
}

// decodeSuggestions reads the model's text as the JSON it was asked for. A
// model that wrapped the JSON in a code fence, or wrote a line above it, is
// still understood: the object between the outermost braces is the answer.
func decodeSuggestions(answer string) ([]suggestion, error) {
	text := strings.TrimSpace(answer)
	if fence := strings.Index(text, "```"); fence >= 0 {
		rest := text[fence+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			if end := strings.Index(rest[nl+1:], "```"); end >= 0 {
				text = strings.TrimSpace(rest[nl+1 : nl+1+end])
			}
		}
	}
	first, last := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}')
	if first < 0 || last < first {
		return nil, fmt.Errorf("the model did not answer with JSON: %s", snippet(answer))
	}
	var reply llmReply
	if err := json.Unmarshal([]byte(text[first:last+1]), &reply); err != nil {
		return nil, fmt.Errorf("unreadable suggestions: %w: %s", err, snippet(answer))
	}
	var out []suggestion
	for _, s := range reply.Suggestions {
		if strings.TrimSpace(s.Original) == "" {
			continue // nothing to point at, so nothing to show
		}
		out = append(out, suggestion{
			Original:    s.Original,
			Replacement: firstNonEmpty(s.Suggestion, s.Replacement, s.Revised),
			Comment:     firstNonEmpty(s.Comment, s.Reason),
		})
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// snippet shortens a message meant for the UI: an API's refusal, or a
// model's prose where JSON was asked for, says what it has to say in its
// first lines.
func snippet(s string) string {
	s = strings.TrimSpace(s)
	// Cut on a rune boundary: the text goes into the page, and half a
	// character is not text.
	if r := []rune(s); len(r) > 300 {
		s = string(r[:300]) + "…"
	}
	return s
}

// locateAll finds each suggestion's passage in the page. A passage found
// twice is taken at its first unclaimed position, so two suggestions about
// the same wording mark two places rather than the same one twice.
func locateAll(text string, sugs []suggestion) []suggestion {
	claimed := 0 // byte offset past the last located passage
	out := make([]suggestion, 0, len(sugs))
	for _, s := range sugs {
		start, end, ok := locate(text, s.Original, claimed)
		if !ok && claimed > 0 {
			// Not ahead of the last one: the model may report out of
			// order, so the page is searched from the top again.
			start, end, ok = locate(text, s.Original, 0)
		}
		if ok {
			s.Start, s.End, s.Located = utf16Len(text[:start]), utf16Len(text[:end]), true
			if end > claimed {
				claimed = end
			}
		}
		out = append(out, s)
	}
	return out
}

// locate finds passage in text at or after from, first as it stands and
// then allowing any run of whitespace where the passage has one: a model
// quoting across a line break rarely reproduces the break. Returns byte
// offsets.
func locate(text, passage string, from int) (int, int, bool) {
	if from > len(text) {
		return 0, 0, false
	}
	if i := strings.Index(text[from:], passage); i >= 0 {
		return from + i, from + i + len(passage), true
	}
	re, err := looseRE(passage)
	if err != nil {
		return 0, 0, false
	}
	if m := re.FindStringIndex(text[from:]); m != nil {
		return from + m[0], from + m[1], true
	}
	return 0, 0, false
}

// looseRE spells a quoted passage out as a pattern that accepts any run of
// whitespace between its words, which is what tells a passage the model
// re-wrapped from one that is not in the page at all.
func looseRE(passage string) (*regexp.Regexp, error) {
	fields := strings.Fields(passage)
	if len(fields) == 0 {
		return nil, errors.New("empty passage")
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = regexp.QuoteMeta(f)
	}
	return regexp.Compile(strings.Join(parts, `\s+`))
}

// utf16Len counts a string the way a browser counts one: in UTF-16 code
// units. The editor is handed offsets, and an emoji or a CJK character
// ahead of a passage would otherwise shift the mark off it.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r == utf8.RuneError {
			n++
			continue
		}
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}
