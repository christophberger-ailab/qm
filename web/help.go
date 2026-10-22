package web

import (
	"embed"
	"encoding/json"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The online help is a set of Markdown pages kept in web/help and embedded
// in the binary like the rest of the UI. The server hands them out as they
// are written; the help pane renders them in the browser with the same
// renderer the preview uses, so a help page reads like a previewed one and
// there is no second Markdown implementation to keep in step.

//go:embed help/*.md
var helpFS embed.FS

// helpIndex is the page the help pane opens on, and the one whose links
// decide the order the topics are listed in.
const helpIndex = "index.md"

// helpTopic is one entry of the help pane's topic list.
type helpTopic struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// helpLink matches a Markdown link to another help page, `[text](name.md)`.
var helpLink = regexp.MustCompile(`\]\(([A-Za-z0-9_-]+\.md)\)`)

// helpTopics lists the help pages: the index first, then the pages in the
// order the index links them, then any page the index does not link, by
// name. A page's title is its first heading, else its name.
func helpTopics() ([]helpTopic, error) {
	entries, err := helpFS.ReadDir("help")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	var order []string
	if index, err := helpFS.ReadFile("help/" + helpIndex); err == nil {
		order = append(order, helpIndex)
		for _, m := range helpLink.FindAllStringSubmatch(string(index), -1) {
			if slices.Contains(names, m[1]) && !slices.Contains(order, m[1]) {
				order = append(order, m[1])
			}
		}
	}
	for _, name := range names { // ReadDir sorts by name
		if !slices.Contains(order, name) {
			order = append(order, name)
		}
	}
	topics := make([]helpTopic, 0, len(order))
	for _, name := range order {
		text, err := helpFS.ReadFile("help/" + name)
		if err != nil {
			return nil, err
		}
		topics = append(topics, helpTopic{Name: name, Title: helpTitle(string(text), name)})
	}
	return topics, nil
}

// helpTitle is the text of the page's first `#` heading, or the page's
// name without its extension when it has none.
func helpTitle(text, name string) string {
	for line := range strings.SplitSeq(text, "\n") {
		if title, ok := strings.CutPrefix(line, "# "); ok {
			if title = strings.TrimSpace(title); title != "" {
				return title
			}
		}
	}
	return strings.TrimSuffix(name, ".md")
}

// helpTopicsHandler answers GET /help with the topic list as JSON.
func (s *server) helpTopicsHandler(w http.ResponseWriter, r *http.Request) {
	topics, err := helpTopics()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(topics)
}

// helpPageHandler answers GET /help/{name} with the Markdown source of one
// help page. Only a plain page name is looked up: the pages are a flat
// list, and the embedded FS holds nothing else worth reaching.
func (s *server) helpPageHandler(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != path.Base(name) || !strings.HasSuffix(name, ".md") {
		http.NotFound(w, r)
		return
	}
	text, err := helpFS.ReadFile("help/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write(text)
}
