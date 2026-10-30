package web

// chat.go — talking to the model about the documentation.
//
// The copyedit pane's editing tasks are questions the user wrote down
// once; the chat is the question asked now. The user types, the model
// answers, and to answer it may look the project up: search the pages,
// list them, read one. It reads and nothing else. There is no tool that
// writes, so a chat cannot change a page; what it can do is suggest a
// change to the page open in the editor, in the shape a copyedit run
// suggests one -- a passage, what to put there, and why -- and the pane
// shows those suggestions the way it shows a run's, marked in the text and
// offered for Apply and Done. The writing stays the user's.
//
// The tools are qm's own, implemented here against the open project, and
// offered to every kind of connection in the form that kind speaks:
// Anthropic's tool use, OpenAI's function calling, and the Copilot SDK's
// custom tools. The conversation itself is kept by the browser, with the
// rest of the pane, and sent along with each new message; the server
// remembers nothing between two of them.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	copilot "github.com/github/copilot-sdk/go"

	"github.com/christophberger-ailab/qm/internal/project"
)

// chatSystem is the chat's instruction. Like suggestionFormat it is ours
// rather than the user's: what the model may do, and how a suggested edit
// reaches the pane.
const chatSystem = `You are an assistant for the authors of a Quarto Markdown documentation
project. The user is working on one page of it in an editor and asks you
questions about the documentation: what it says, where something is
covered, whether pages agree, how a passage could be improved.

You can look the project up with tools:
  - search_pages finds the pages containing words or a quoted phrase;
  - list_pages lists every page with its title, in the book's order;
  - read_page reads one page.
Use them whenever the answer depends on what the documentation says, and
say which pages your answer rests on. Do not guess at what a page says.

You cannot change any page. When a change to the page open in the editor
would help, propose it with suggest_edit: quote the passage exactly as it
stands in that page, give the text to put in its place, and say why in one
sentence. The user reviews every suggestion and decides whether to apply
it. suggest_edit works on the open page only; describe changes to other
pages in your answer instead.

Answer in the language the user writes in. Keep the answer short and to
the point; plain text, with Markdown only where a list or a code sample
needs it.`

// maxToolRounds bounds the back and forth of one answer. A model that is
// still searching after this many rounds is asked to answer with what it
// has, rather than left to run up a bill.
const maxToolRounds = 12

// chatTurn is one message of the conversation as the browser keeps it.
type chatTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// chatTool is one tool the model is offered: what it is called, what it
// is for, and the JSON schema of its arguments.
type chatTool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// docTools is what the tools work on: the open project, read from disk,
// and the page open in the editor, read from the editor's text so that a
// question about it is answered on what the user sees. It also collects
// the edits the model suggested, which are the chat's other output.
type docTools struct {
	root     string
	index    *searchIndex
	openPath string
	openText string

	suggestions []suggestion
	// trail is what the tools were used for, in words, so that the pane
	// can say what an answer was based on.
	trail []string
}

var chatTools = []chatTool{
	{
		Name: "search_pages",
		Description: "Search the text of every page of the project. A page matches when it contains every word " +
			"of the query (words match by prefix, case-insensitively); words in double quotes must stand " +
			"together. Returns the matching pages with their titles, hit counts, and matching lines.",
		Parameters: objectSchema(map[string]any{
			"query": map[string]any{"type": "string", "description": "The words or quoted phrase to search for."},
		}, "query"),
	},
	{
		Name:        "list_pages",
		Description: "List every page of the project with its path and title, nested by section, in the book's order.",
		Parameters:  objectSchema(map[string]any{}),
	},
	{
		Name: "read_page",
		Description: "Read one page of the project by its path, as search_pages or list_pages name it. " +
			"The page open in the editor is read as the editor holds it, unsaved edits included.",
		Parameters: objectSchema(map[string]any{
			"path": map[string]any{"type": "string", "description": "The page's path relative to the project root, e.g. chapter1/intro.qmd."},
		}, "path"),
	},
	{
		Name: "suggest_edit",
		Description: "Propose a change to the page open in the editor. Nothing is written: the user sees the " +
			"suggestion beside the page and decides whether to apply it.",
		Parameters: objectSchema(map[string]any{
			"original":    map[string]any{"type": "string", "description": "The passage to change, copied from the open page character for character. Quote as little as possible."},
			"replacement": map[string]any{"type": "string", "description": "The text to put in its place."},
			"comment":     map[string]any{"type": "string", "description": "One short sentence saying why."},
		}, "original", "replacement", "comment"),
	},
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// call runs one tool and returns what the model is told. A tool that
// fails says why in its result rather than ending the chat: the model can
// read the reason and try again, which is what the user would want it to
// do.
func (d *docTools) call(name string, args json.RawMessage) string {
	var in struct {
		Query       string `json:"query"`
		Path        string `json:"path"`
		Original    string `json:"original"`
		Replacement string `json:"replacement"`
		Comment     string `json:"comment"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return "Error: the arguments are not valid JSON: " + err.Error()
		}
	}
	switch name {
	case "search_pages":
		d.trail = append(d.trail, fmt.Sprintf("searched for %q", in.Query))
		return d.search(in.Query)
	case "list_pages":
		d.trail = append(d.trail, "listed the pages")
		return d.listPages()
	case "read_page":
		d.trail = append(d.trail, "read "+in.Path)
		return d.readPage(in.Path)
	case "suggest_edit":
		return d.suggest(in.Original, in.Replacement, in.Comment)
	}
	return "Error: there is no tool named " + name
}

// search answers search_pages from the search field's index, so the model
// finds what the user would find. An index that is not built yet, or not
// up to date, is not waited for: the pages are read for this one query
// instead.
func (d *docTools) search(q string) string {
	if len(parseQuery(q)) == 0 {
		return "Error: the query has no word to search for."
	}
	// The index is told what the project looks like now, which it may not
	// know yet: the watch that keeps it up to date polls. One that turns
	// out to be behind is not trusted for this answer.
	d.index.rebuild(d.root, fingerprint(d.root))
	hits, current := d.index.search(d.root, q, false)
	if !current {
		hits = matchPages(indexPages(d.root), q, false)
	}
	if len(hits) == 0 {
		return "No page matches " + q + "."
	}
	titles := pageTitles(d.root)
	terms := parseQuery(q)
	var b strings.Builder
	fmt.Fprintf(&b, "%s match %s:\n", plural(len(hits), "page"), q)
	const shown = 15
	for i, h := range hits {
		if i == shown {
			fmt.Fprintf(&b, "\n... and %d more; narrow the query to see them.\n", len(hits)-shown)
			break
		}
		fmt.Fprintf(&b, "\n%s — %s (%s)\n", h.Path, titles[h.Path], plural(h.Count, "hit"))
		for _, line := range matchingLines(d.pageText(h.Path), terms, 3) {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	return b.String()
}

// matchingLines are the first few lines of a page that contain one of the
// query's words, which is what tells the model whether a hit is worth
// reading the page for.
func matchingLines(text string, terms []queryTerm, limit int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		low := strings.ToLower(line)
		for _, t := range terms {
			if len(t.words) > 0 && strings.Contains(low, strings.ToLower(t.words[0])) {
				out = append(out, snippet(strings.TrimSpace(line)))
				break
			}
		}
		if len(out) == limit {
			break
		}
	}
	return out
}

// listPages spells the page tree out, one page a line, indented by depth.
func (d *docTools) listPages() string {
	tree, err := project.Load(d.root)
	if tree == nil {
		return "Error: the project could not be read: " + fmt.Sprint(err)
	}
	var b strings.Builder
	var walk func(pages []*project.Page, depth int)
	walk = func(pages []*project.Page, depth int) {
		for _, p := range pages {
			fmt.Fprintf(&b, "%s%s — %s", strings.Repeat("  ", depth), p.Path, p.Title)
			if p.Draft {
				b.WriteString(" (draft)")
			}
			b.WriteString("\n")
			walk(p.Children, depth+1)
		}
	}
	walk(tree.Pages, 0)
	if b.Len() == 0 {
		return "The project has no pages."
	}
	return b.String()
}

// maxPageRead bounds what one read_page hands the model. A page is rarely
// near it; one that is gets its head, which says what it is about, rather
// than a request the provider refuses for its size.
const maxPageRead = 120000

func (d *docTools) readPage(rel string) string {
	if rel == d.openPath && d.openPath != "" {
		return "The page " + rel + " (open in the editor):\n\n" + clip(d.openText)
	}
	if !isProjectPage(rel) {
		return "Error: " + rel + " is not a page of the project; use a path that search_pages or list_pages names."
	}
	text := d.pageText(rel)
	if text == "" {
		return "Error: there is no page " + rel + "."
	}
	return "The page " + rel + ":\n\n" + clip(text)
}

func clip(text string) string {
	if len(text) <= maxPageRead {
		return text
	}
	return text[:maxPageRead] + "\n\n[... the rest of the page is cut off]"
}

// pageText reads a page of the project, or the editor's text for the page
// open in it; "" when there is no such page.
func (d *docTools) pageText(rel string) string {
	if rel == d.openPath && d.openPath != "" {
		return d.openText
	}
	if !isProjectPage(rel) || d.root == "" {
		return ""
	}
	abs := filepath.Join(d.root, filepath.FromSlash(rel))
	// A link inside the project that points out of it is not a page of
	// the project, however it is named.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		if rootReal, err := filepath.EvalSymlinks(d.root); err == nil {
			if r, err := filepath.Rel(rootReal, real); err != nil || strings.HasPrefix(r, "..") {
				return ""
			}
		}
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	return string(src)
}

// isProjectPage says whether a path the model named is one of the pages
// the tree shows -- a .qmd file outside the "_" and "." folders and not
// itself prefixed with "_" -- which is all the chat may read. The rest of
// the project, configuration and all, is not the documentation.
func isProjectPage(rel string) bool {
	if !safeRel(rel) || path.Ext(rel) != ".qmd" {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, "_") || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

// pageTitles maps every page's path to its title, as the tree shows it.
func pageTitles(root string) map[string]string {
	titles := map[string]string{}
	tree, _ := project.Load(root)
	if tree == nil {
		return titles
	}
	var walk func([]*project.Page)
	walk = func(pages []*project.Page) {
		for _, p := range pages {
			titles[p.Path] = p.Title
			walk(p.Children)
		}
	}
	walk(tree.Pages)
	return titles
}

// suggest records a suggested edit to the open page. Only a passage that
// is in the page is recorded: one that is not could neither be marked nor
// applied, and the model, told so, can quote it again properly -- which
// beats a list entry that says the quote was wrong.
func (d *docTools) suggest(original, replacement, comment string) string {
	if d.openPath == "" {
		return "Error: no page is open in the editor, so there is nothing to suggest an edit to."
	}
	if strings.TrimSpace(original) == "" {
		return "Error: the passage to change is empty."
	}
	if _, _, ok := locate(d.openText, original, 0); !ok {
		return "Error: that passage is not in the open page (" + d.openPath + "). " +
			"Quote it exactly as it stands there, character for character; read_page shows the page."
	}
	d.suggestions = append(d.suggestions, suggestion{
		Original: original, Replacement: replacement, Comment: comment,
	})
	return "Recorded. The user sees the suggestion beside the page and decides whether to apply it."
}

// located places the suggestions in the open page, the way a copyedit
// run's are placed.
func (d *docTools) located() []suggestion {
	return locateAll(d.openText, d.suggestions)
}

// chatOpening is the first user message's context: the page open in the
// editor. It comes first and stays the same while the user asks about the
// page, which is what a provider's prompt cache can hold on to.
func chatOpening(path, text string) string {
	if path == "" || strings.TrimSpace(text) == "" {
		return "No page is open in the editor."
	}
	return "The page open in the editor" + " (" + path + "):\n\n" + text
}

// cleanHistory makes the conversation the browser sent into one an API
// takes: known roles only, no empty turns, and the user and the model
// taking turns, starting with the user. Two turns of the same role in a
// row -- a message whose answer failed -- are joined.
func cleanHistory(history []chatTurn, message string) []chatTurn {
	var out []chatTurn
	add := func(role, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		if len(out) == 0 && role != "user" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Text += "\n\n" + text
			return
		}
		out = append(out, chatTurn{Role: role, Text: text})
	}
	for _, t := range history {
		if t.Role == "user" || t.Role == "assistant" {
			add(t.Role, t.Text)
		}
	}
	add("user", message)
	return out
}

// runChat answers the last message of the conversation, using the tools
// as the model sees fit, and returns the answer.
func runChat(conn apiConnection, tools *docTools, turns []chatTurn) (string, error) {
	if conn.Model == "" {
		return "", errors.New("the connection names no model")
	}
	if len(turns) == 0 || turns[len(turns)-1].Role != "user" {
		return "", errors.New("there is no question to answer")
	}
	opening := chatOpening(tools.openPath, tools.openText)
	switch conn.Kind {
	case kindCopilot:
		return chatCopilot(conn, tools, opening, turns)
	case kindAnthropic:
		return chatAnthropic(conn, tools, opening, turns)
	}
	return chatOpenAI(conn, tools, opening, turns)
}

// chatAnthropic runs the conversation on the Messages API: the model asks
// for tools in tool_use blocks, the results go back in tool_result blocks,
// and the round repeats until it answers in text.
func chatAnthropic(conn apiConnection, tools *docTools, opening string, turns []chatTurn) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(conn.BaseURL), "/")
	if base == "" {
		return "", errors.New("the connection names no base URL")
	}
	url := endpointURL(base, "/messages")
	var messages []map[string]any
	for i, t := range turns {
		if i == 0 {
			messages = append(messages, map[string]any{"role": "user", "content": []map[string]any{
				{"type": "text", "text": opening, "cache_control": map[string]string{"type": "ephemeral"}},
				{"type": "text", "text": t.Text},
			}})
			continue
		}
		messages = append(messages, map[string]any{"role": t.Role, "content": t.Text})
	}
	var defs []map[string]any
	for _, t := range chatTools {
		defs = append(defs, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
	}
	for round := 0; ; round++ {
		payload := map[string]any{
			"model":      conn.Model,
			"max_tokens": maxTokens,
			"system":     chatSystem,
			"messages":   messages,
			"tools":      defs,
		}
		if round >= maxToolRounds {
			payload["tool_choice"] = map[string]string{"type": "none"}
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		raw, err := postAPI(conn, url, body)
		if err != nil {
			return "", err
		}
		var reply struct {
			Content    []json.RawMessage `json:"content"`
			StopReason string            `json:"stop_reason"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			return "", fmt.Errorf("unreadable answer: %w", err)
		}
		var results []map[string]any
		for _, block := range reply.Content {
			var use struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			json.Unmarshal(block, &use)
			if use.Type == "tool_use" {
				results = append(results, map[string]any{
					"type": "tool_result", "tool_use_id": use.ID, "content": tools.call(use.Name, use.Input),
				})
			}
		}
		if len(results) == 0 {
			return answerText(kindAnthropic, raw)
		}
		// The model's turn goes back as it came, thinking blocks and all:
		// the API checks that the tool results answer the calls it made.
		messages = append(messages,
			map[string]any{"role": "assistant", "content": reply.Content},
			map[string]any{"role": "user", "content": results})
	}
}

// chatOpenAI runs the conversation on the chat completions API: the model
// asks for tools in tool_calls, each result goes back as a message of the
// "tool" role, and the round repeats until it answers in text.
func chatOpenAI(conn apiConnection, tools *docTools, opening string, turns []chatTurn) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(conn.BaseURL), "/")
	if base == "" {
		return "", errors.New("the connection names no base URL")
	}
	url := endpointURL(base, "/chat/completions")
	messages := []any{map[string]string{"role": "system", "content": chatSystem}}
	for i, t := range turns {
		text := t.Text
		if i == 0 {
			text = opening + "\n\n" + text
		}
		messages = append(messages, map[string]string{"role": t.Role, "content": text})
	}
	var defs []map[string]any
	for _, t := range chatTools {
		defs = append(defs, map[string]any{"type": "function", "function": map[string]any{
			"name": t.Name, "description": t.Description, "parameters": t.Parameters,
		}})
	}
	for round := 0; ; round++ {
		payload := map[string]any{
			"model":      conn.Model,
			"max_tokens": maxTokens,
			"messages":   messages,
			"tools":      defs,
		}
		if round >= maxToolRounds {
			payload["tool_choice"] = "none"
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		raw, err := postAPI(conn, url, body)
		if err != nil {
			return "", err
		}
		var reply struct {
			Choices []struct {
				Message json.RawMessage `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			return "", fmt.Errorf("unreadable answer: %w", err)
		}
		if len(reply.Choices) == 0 {
			return answerText(kindOpenAI, raw)
		}
		var msg struct {
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		}
		json.Unmarshal(reply.Choices[0].Message, &msg)
		if len(msg.ToolCalls) == 0 {
			return answerText(kindOpenAI, raw)
		}
		messages = append(messages, reply.Choices[0].Message)
		for _, c := range msg.ToolCalls {
			messages = append(messages, map[string]string{
				"role": "tool", "tool_call_id": c.ID,
				"content": tools.call(c.Function.Name, json.RawMessage(c.Function.Arguments)),
			})
		}
	}
}

// chatCopilot runs the conversation through the Copilot CLI. The session
// is created for this one answer and given the conversation so far as one
// prompt; the CLI runs the tool rounds itself and calls back into qm for
// each tool, and qm's tools are the only ones the session has.
func chatCopilot(conn apiConnection, tools *docTools, opening string, turns []chatTurn) (string, error) {
	var b strings.Builder
	b.WriteString(opening)
	if len(turns) > 1 {
		b.WriteString("\n\nThe conversation so far:\n")
		for _, t := range turns[:len(turns)-1] {
			who := "User"
			if t.Role == "assistant" {
				who = "You"
			}
			fmt.Fprintf(&b, "\n%s: %s\n", who, t.Text)
		}
	}
	b.WriteString("\n\nThe user asks:\n" + turns[len(turns)-1].Text)

	var defs []copilot.Tool
	for _, t := range chatTools {
		name := t.Name
		defs = append(defs, copilot.Tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
			// The tools read the project and nothing else, and
			// suggest_edit writes nothing either, so there is nothing
			// for the user to be asked about.
			SkipPermission: true,
			Handler: func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
				args, _ := json.Marshal(inv.Arguments)
				return copilot.ToolResult{TextResultForLLM: tools.call(name, args), ResultType: "success"}, nil
			},
		})
	}
	return askCopilotWith(conn, chatSystem, b.String(), defs)
}

// chatReplyView is one answer of the chat as the pane shows it.
type chatReplyView struct {
	Reply       string
	Suggestions []suggestion
	// Trail says what the model looked up to answer.
	Trail []string
	// History is the answer as it is sent back with the next message:
	// the text, and the edits it suggested, which the model would
	// otherwise not know it had made.
	History string
	Model   string
	Error   string
}

// historyText is what the model is reminded of its own answer by.
func historyText(reply string, sugs []suggestion) string {
	if len(sugs) == 0 {
		return reply
	}
	var b strings.Builder
	b.WriteString(reply)
	b.WriteString("\n\n[Edits suggested to the open page:")
	for _, s := range sugs {
		fmt.Fprintf(&b, "\n- %q → %q", s.Original, s.Replacement)
	}
	b.WriteString("]")
	return b.String()
}

// copyeditChatHandler answers one chat message. The form carries the
// message, the conversation so far as JSON, and the page the editor holds
// -- its path and its text, unsaved edits included.
//
// Like a copyedit run, the model call runs without the server's lock; the
// tools read the disk without it too, the project being one that only the
// user writes to.
func (s *server) copyeditChatHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	s.mu.Lock()
	conn, ok := s.activeConnection()
	root := s.root
	s.mu.Unlock()

	view := chatReplyView{Model: conn.Name}
	message := strings.TrimSpace(r.PostFormValue("message"))
	var history []chatTurn
	if h := r.PostFormValue("history"); h != "" {
		if err := json.Unmarshal([]byte(h), &history); err != nil {
			view.Error = "The conversation could not be read: " + err.Error()
		}
	}
	switch {
	case view.Error != "":
	case message == "":
		view.Error = "There is no question to answer."
	case !ok:
		view.Error = "No API connection is configured. Add one under Config → Copyedit: API connections."
	case root == "":
		view.Error = "No project is open."
	}
	if view.Error != "" {
		s.render(w, "copyedit-chat-reply", view)
		return
	}
	tools := &docTools{
		root:     root,
		index:    &s.index,
		openPath: r.PostFormValue("path"),
		openText: r.PostFormValue("body"),
	}
	reply, err := runChat(conn, tools, cleanHistory(history, message))
	view.Trail = tools.trail
	if err != nil {
		view.Error = err.Error()
	}
	view.Reply = strings.TrimSpace(reply)
	view.Suggestions = tools.located()
	view.History = historyText(view.Reply, view.Suggestions)
	s.render(w, "copyedit-chat-reply", view)
}
