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

// maxTokens is the answer's budget. The suggestion list itself is short
// next to the page, but the budget is not only spent on it: a reasoning
// model counts what it thinks against the same ceiling, and one that
// reaches it while still thinking answers with nothing at all. The
// ceiling is a limit rather than a bill -- only what is generated is
// paid for -- so it is set well above what the list needs.
const maxTokens = 16384

// The kinds of API a connection can address. The kind decides the request
// shape, the auth header, and where the answer's text sits in the reply.
const (
	kindAnthropic = "anthropic"
	kindOpenAI    = "openai"
)

// suggestionFormat is the part of the instruction that is ours rather than
// the user's: the editing tasks come from the prompts the user wrote, the
// shape of the answer comes from here. The pane can only highlight what it
// can find again in the page, so the "original" field is asked for
// verbatim and kept short.
//
// It is the system prompt, and the page follows it unchanged from one run
// to the next, which is what lets the two of them be cached together (see
// requestFor). Everything that varies -- which tasks are being run -- is
// asked for after the page.
const suggestionFormat = `You are a copy editor working on one page of a Quarto Markdown document.
You are given the page, then one or more editing tasks to apply to it.
Report what you would change.

Answer with JSON and nothing else, in exactly this shape:

{"suggestions":[{"task":"t1","original":"...","suggestion":"...","comment":"..."}]}

  - "task" is the id in brackets of the editing task the suggestion comes
    from, copied exactly. Every suggestion names the task it answers.
  - "original" is the passage the suggestion applies to, copied from the
    page character for character, so that it can be found in the page
    again. Quote as little as possible: the sentence or the phrase, never a
    whole paragraph or section.
  - "suggestion" is the text you would put in its place.
  - "comment" is one short sentence saying why.

Work through every task you are given and report each one's findings, even
when one task yields many and another none. Report only what the tasks ask
for. Leave Markdown syntax, YAML frontmatter, code blocks, and Quarto
shortcodes alone unless a task is about them. If the page needs no change,
answer {"suggestions":[]}.`

// suggestion is one piece of advice: the passage it is about, what to put
// there instead, and why. Start and End locate the passage in the page as
// offsets in UTF-16 code units — what a browser counts string positions in
// — so the editor can mark it; Located says whether the passage was found
// at all.
type suggestion struct {
	// Task is the title of the editing task this came from; a run of
	// several tasks is grouped by it.
	Task        string
	Original    string
	Replacement string
	Comment     string
	Start       int
	End         int
	Located     bool
}

// editingTask is one task as a run gives it to the model: the id the model
// tags its suggestions with, and the title and prompt the user wrote. The
// id is the task's place in this run ("t1", "t2"), not the id it has in
// the config, which is nothing the model needs to know.
type editingTask struct {
	ID     string
	Title  string
	Prompt string
}

// Applicable says whether this suggestion can be written into the page at
// the click of a button: it must name a passage that was found there, and
// say what to put in its place. A suggestion that only comments on the
// text, or quotes something the page does not contain, is read and carried
// out by hand.
func (s suggestion) Applicable() bool {
	return s.Located && s.Replacement != ""
}

// llmReply is the JSON the model is asked for. The fields are read
// leniently: models spell the replacement "suggestion", "replacement", or
// "revised", and a run is not worth losing over the word one picked.
type llmReply struct {
	Suggestions []struct {
		Task        string `json:"task"`
		Original    string `json:"original"`
		Suggestion  string `json:"suggestion"`
		Replacement string `json:"replacement"`
		Revised     string `json:"revised"`
		Comment     string `json:"comment"`
		Reason      string `json:"reason"`
	} `json:"suggestions"`
}

// runCopyedit asks the connection's model to apply the tasks to the text of
// the page at path and returns the suggestions they produced, each located
// in the text and attributed to the task it came from. The path travels
// with the page because it is part of what the page is: a Quarto page's
// name carries its audience (`_FW`, `_POL`) and its place in the book, and
// an editing task may well be about either.
//
// Several tasks go in one call rather than one call each. The page is the
// bulk of what a run sends, and sending it once for five tasks costs a
// fraction of sending it five times; the tasks are what the user picked,
// so nothing is asked for that was not.
func runCopyedit(conn apiConnection, tasks []editingTask, path, text string) ([]suggestion, error) {
	if len(tasks) == 0 {
		return nil, errors.New("no editing task was selected")
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("the page is empty, so there is nothing to edit")
	}
	answer, err := askModel(conn, suggestionFormat, pageBlock(path, text), taskBlock(tasks), len(tasks))
	if err != nil {
		return nil, err
	}
	sugs, err := decodeSuggestions(answer, tasks)
	if err != nil {
		return nil, err
	}
	return locateAll(text, sugs), nil
}

// runCopyeditPerTask asks each task on its own, one call after another,
// and returns everything they came back with. It is the other way of
// carrying out a run (see the mode constants in copyedit.go): the model
// sees one task at a time and attends to it fully, at the price of a
// request per task.
//
// The calls are made one after another rather than at once, on purpose.
// The page is the cached part of the request, and a cache is written by
// the call that misses it: firing every task in parallel would have them
// all miss, where in sequence the first writes the page into the
// provider's cache and the rest read it. Sequence is also what keeps a
// long run from arriving at the provider as a burst that its rate limit
// answers with 429s.
//
// A task that fails does not take the run with it: its failure is
// reported, and what the other tasks found is still shown. Only a run in
// which nothing succeeded is an error.
func runCopyeditPerTask(conn apiConnection, tasks []editingTask, path, text string) ([]suggestion, []string, error) {
	if len(tasks) == 0 {
		return nil, nil, errors.New("no editing task was selected")
	}
	var all []suggestion
	var failed []string
	for _, task := range tasks {
		sugs, err := runCopyedit(conn, []editingTask{task}, path, text)
		if err != nil {
			failed = append(failed, task.Title+": "+err.Error())
			continue
		}
		all = append(all, sugs...)
	}
	if len(failed) == len(tasks) {
		return nil, failed, errors.New(strings.Join(failed, "; "))
	}
	return all, failed, nil
}

// pageBlock is the part of the request that stays the same while the user
// works through the tasks on one page: it is what a provider's prompt
// cache can hold on to, so it is kept whole and put first.
func pageBlock(path, text string) string {
	head := "The page"
	if path != "" {
		head += " (" + path + ")"
	}
	return head + ":\n\n" + text
}

// taskBlock spells the selected tasks out, each under the id its
// suggestions are to be tagged with. It comes after the page, being the
// part that differs from run to run.
func taskBlock(tasks []editingTask) string {
	var b strings.Builder
	b.WriteString("Editing task")
	if len(tasks) > 1 {
		b.WriteString("s")
	}
	b.WriteString(":\n")
	for _, t := range tasks {
		fmt.Fprintf(&b, "\n[%s] %s\n%s\n", t.ID, t.Title, t.Prompt)
	}
	return b.String()
}

// tasksFor numbers the prompts for one run: "t1", "t2", ... The model sees
// these ids and tags each suggestion with one, which is how a run of
// several tasks is sorted back out into the task it came from.
func tasksFor(prompts []copyeditPrompt) []editingTask {
	tasks := make([]editingTask, 0, len(prompts))
	for i, p := range prompts {
		tasks = append(tasks, editingTask{
			ID: fmt.Sprintf("t%d", i+1), Title: p.Title, Prompt: p.Prompt,
		})
	}
	return tasks
}

// answerBudget is the answer's token ceiling. It grows with the number of
// tasks, because a run of five has five tasks' findings to report and an
// answer cut off in the middle is not JSON at all -- the whole run is lost
// with it. The ceiling keeps a runaway answer from being paid for twice
// over.
func answerBudget(tasks int) int {
	if tasks < 1 {
		tasks = 1
	}
	budget := maxTokens + (tasks-1)*4096
	if budget > 65536 {
		return 65536
	}
	return budget
}

// askModel sends one request to the connection and returns the model's
// text. The page and the tasks are passed apart rather than as one string
// because the request keeps them apart: the page is the cacheable part.
func askModel(conn apiConnection, system, page, tasks string, count int) (string, error) {
	if conn.Model == "" {
		return "", errors.New("the connection names no model")
	}
	url, body, err := requestFor(conn, system, page, tasks, count)
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
		// The URL is part of the report: a refusal is about the key, but a
		// 404 is about the address, and the address is composed here from
		// what the connection carries.
		return "", fmt.Errorf("%s answered %s for %s: %s", conn.Name, resp.Status, url, snippet(string(raw)))
	}
	return answerText(conn.Kind, raw)
}

// endpointURL puts the API's own path onto a connection's base URL. A base
// URL that already ends in that path is left as it is: what a provider's
// documentation prints is the whole endpoint
// (`https://openrouter.ai/api/v1/chat/completions`), so that is what gets
// pasted into the field at least as often as the base it asks for, and
// appending the path a second time only produces a 404.
func endpointURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(strings.ToLower(base), path) {
		return base
	}
	return base + path
}

// requestFor composes the endpoint and the request body for the
// connection's API kind.
//
// The order is what makes a run cheap: the instruction and the page come
// first and are the same for every task run against that page, the tasks
// come last. A prompt cache matches on the prefix of a request, so
// everything up to the tasks can be served from the cache of the run
// before it; the other way round -- the task first -- the page would sit
// behind something that changes every run and could never be cached at
// all. On Anthropic the prefix must also be marked, which is what the
// cache_control breakpoint on the page does; the OpenAI-style providers
// that cache do it by themselves, and those that do not are simply served
// a request in a sensible order.
func requestFor(conn apiConnection, system, page, tasks string, count int) (string, []byte, error) {
	base := strings.TrimRight(strings.TrimSpace(conn.BaseURL), "/")
	if base == "" {
		return "", nil, errors.New("the connection names no base URL")
	}
	var payload any
	var url string
	switch conn.Kind {
	case kindAnthropic:
		url = endpointURL(base, "/messages")
		payload = map[string]any{
			"model":      conn.Model,
			"max_tokens": answerBudget(count),
			"system":     system,
			"messages": []map[string]any{{
				"role": "user",
				"content": []map[string]any{
					{
						"type":          "text",
						"text":          page,
						"cache_control": map[string]string{"type": "ephemeral"},
					},
					{"type": "text", "text": tasks},
				},
			}},
		}
	default: // OpenAI-style, which is what every other provider speaks
		url = endpointURL(base, "/chat/completions")
		payload = map[string]any{
			"model":       conn.Model,
			"max_tokens":  answerBudget(count),
			"temperature": 0,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": page + "\n\n" + tasks},
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

// messageText is a chat message's content. Providers send it either as a
// string or as the list of parts the newer shape uses, and a connection
// may be pointed at either, so both are read.
type messageText string

func (m *messageText) UnmarshalJSON(b []byte) error {
	var text string
	if err := json.Unmarshal(b, &text); err == nil {
		*m = messageText(text)
		return nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &parts); err != nil {
		*m = "" // null, a number, something else: no text, which the caller reports
		return nil
	}
	var out strings.Builder
	for _, p := range parts {
		if p.Type == "text" || p.Type == "" {
			out.WriteString(p.Text)
		}
	}
	*m = messageText(out.String())
	return nil
}

// apiError is the error object an API may put in a body it answers 200
// with. A gateway that could not reach the model behind it reports the
// reason this way rather than in the status, and that reason is the only
// thing worth showing.
type apiError struct {
	Message string `json:"message"`
	Code    any    `json:"code"`
}

// answerText digs the model's text out of the reply.
//
// Where the text sits differs by API, and what comes back when there is
// no text differs by provider, so this is more forgiving than the shapes
// suggest: an error in a 200 body is reported as the error it is, a
// reasoning model that put everything in its reasoning and never wrote a
// final answer is taken at its reasoning, and anything else says what
// actually came back (see noTextError) rather than "no text".
func answerText(kind string, raw []byte) (string, error) {
	if kind == kindAnthropic {
		var reply struct {
			Error   *apiError `json:"error"`
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking string `json:"thinking"`
			} `json:"content"`
			StopReason string `json:"stop_reason"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			return "", fmt.Errorf("unreadable answer: %w", err)
		}
		if reply.Error != nil && reply.Error.Message != "" {
			return "", fmt.Errorf("the API refused the call: %s", reply.Error.Message)
		}
		var text, thinking strings.Builder
		for _, part := range reply.Content {
			switch part.Type {
			case "text", "":
				text.WriteString(part.Text)
			case "thinking":
				thinking.WriteString(part.Thinking)
			}
		}
		if text.Len() > 0 {
			return text.String(), nil
		}
		if thinking.Len() > 0 {
			return thinking.String(), nil
		}
		return "", noTextError(reply.StopReason, raw)
	}

	var reply struct {
		Error   *apiError `json:"error"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content messageText `json:"content"`
				// What a reasoning model thought before answering.
				// Providers spell it both ways.
				Reasoning        string `json:"reasoning"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return "", fmt.Errorf("unreadable answer: %w", err)
	}
	if reply.Error != nil && reply.Error.Message != "" {
		return "", fmt.Errorf("the API refused the call: %s", reply.Error.Message)
	}
	if len(reply.Choices) == 0 {
		return "", noTextError("", raw)
	}
	choice := reply.Choices[0]
	if text := string(choice.Message.Content); text != "" {
		return text, nil
	}
	// A reasoning model that spent its budget thinking answers with empty
	// content and its reasoning beside it. The JSON we asked for is often
	// in there, and reading it beats refusing an answer the model did
	// give.
	if thought := firstNonEmpty(choice.Message.Reasoning, choice.Message.ReasoningContent); thought != "" {
		return thought, nil
	}
	return "", noTextError(choice.FinishReason, raw)
}

// noTextError says what came back when none of it was text. The reason
// the model stopped is the useful half -- "length" means the answer hit
// the token ceiling, which on a reasoning model it can reach while still
// thinking -- and the body is the other, since no amount of guessing
// beats showing what the provider actually sent.
func noTextError(stopReason string, raw []byte) error {
	switch stopReason {
	case "length", "max_tokens":
		return fmt.Errorf("the model used its whole answer budget (%d tokens) before writing an answer, "+
			"which a reasoning model can do while still thinking; the answer was: %s",
			maxTokens, snippet(string(raw)))
	case "content_filter":
		return fmt.Errorf("the provider's content filter stopped the answer: %s", snippet(string(raw)))
	case "":
		return fmt.Errorf("the model answered with no text: %s", snippet(string(raw)))
	}
	return fmt.Errorf("the model answered with no text (it stopped on %q): %s", stopReason, snippet(string(raw)))
}

// decodeSuggestions reads the model's text as the JSON it was asked for. A
// model that wrapped the JSON in a code fence, or wrote a line above it, is
// still understood: the object between the outermost braces is the answer.
//
// Each suggestion is attributed to the task it names (see taskOf). tasks
// is what was asked for, so a tag that names none of them can be told from
// one that names a task properly.
func decodeSuggestions(answer string, tasks []editingTask) ([]suggestion, error) {
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
			Task:        taskOf(tasks, s.Task),
			Original:    s.Original,
			Replacement: firstNonEmpty(s.Suggestion, s.Replacement, s.Revised),
			Comment:     firstNonEmpty(s.Comment, s.Reason),
		})
	}
	return out, nil
}

// taskOf turns the tag a suggestion carries into the title of the task it
// belongs to. A run of one task needs no tag to be understood -- there is
// only one task it can be from -- and a model that tagged by title rather
// than by id is taken at its word. A tag naming nothing that was asked for
// leaves the suggestion unattributed, which the pane shows as its own
// group: the advice is still the model's answer about the page, and
// dropping it because a label came out wrong would lose real work.
func taskOf(tasks []editingTask, tag string) string {
	if len(tasks) == 1 {
		return tasks[0].Title
	}
	tag = strings.TrimSpace(tag)
	for _, t := range tasks {
		if strings.EqualFold(tag, t.ID) || strings.EqualFold(tag, t.Title) {
			return t.Title
		}
	}
	// A model that answered "[t2]" or "t2: Long sentences" rather than the
	// bare id is still saying which task it means.
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(tag), strings.ToLower(t.ID)) ||
			strings.Contains(strings.ToLower(tag), strings.ToLower(t.Title)) {
			return t.Title
		}
	}
	return ""
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
