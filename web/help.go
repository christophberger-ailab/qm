package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The online help is a set of Markdown pages kept in web/help and embedded
// in the binary like the rest of the UI, one folder per language holding
// the same file names. The server hands the pages out as they are written;
// the help pane renders them in the browser with the same renderer the
// preview uses, so a help page reads like a previewed one and there is no
// second Markdown implementation to keep in step.

//go:embed help/*/*.md
var helpFS embed.FS

// helpIndex is the page the help pane opens on, and the one whose links
// decide the order the topics are listed in.
const helpIndex = "index.md"

// helpFallback is the language the help is complete in: a language that is
// not there, and a page a translation lacks, are answered from it.
const helpFallback = "en"

// helpLanguageNames names each language of the help in itself, which is
// how the pane's language menu offers it. A folder not listed here is
// offered by its code.
var helpLanguageNames = map[string]string{
	"de": "Deutsch",
	"en": "English",
}

// helpTopic is one entry of the help pane's topic list.
type helpTopic struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// helpLanguage is one entry of the help pane's language menu.
type helpLanguage struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// helpContents is what GET /help answers with: the language the topics are
// in, the languages there are, and the topics.
type helpContents struct {
	Lang      string         `json:"lang"`
	Languages []helpLanguage `json:"languages"`
	Topics    []helpTopic    `json:"topics"`
}

// helpLink matches a Markdown link to another help page, `[text](name.md)`.
var helpLink = regexp.MustCompile(`\]\(([A-Za-z0-9_-]+\.md)\)`)

// helpCode matches a language folder's name, so that a request can only
// ever name one.
var helpCode = regexp.MustCompile(`^[a-z]{2,3}$`)

// helpLanguages lists the languages the help is written in, by code.
func helpLanguages() []helpLanguage {
	entries, _ := helpFS.ReadDir("help")
	var langs []helpLanguage
	for _, e := range entries {
		if e.IsDir() && helpCode.MatchString(e.Name()) {
			name := helpLanguageNames[e.Name()]
			if name == "" {
				name = e.Name()
			}
			langs = append(langs, helpLanguage{Code: e.Name(), Name: name})
		}
	}
	return langs
}

// helpLang picks the language to show the help in from wanted, a
// comma-separated list of language tags in order of preference, the way a
// browser lists its languages: the first the help is written in, matched
// by its primary subtag, so "de-AT" asks for "de". With none of them
// there, it is the fallback.
func helpLang(wanted string) string {
	for tag := range strings.SplitSeq(wanted, ",") {
		lang, _, _ := strings.Cut(strings.TrimSpace(tag), "-")
		lang = strings.ToLower(lang)
		if !helpCode.MatchString(lang) {
			continue
		}
		if _, err := fs.Stat(helpFS, "help/"+lang); err == nil {
			return lang
		}
	}
	return helpFallback
}

// helpPage reads one help page in lang, or in the fallback language when
// the translation lacks it.
func helpPage(lang, name string) ([]byte, error) {
	text, err := helpFS.ReadFile("help/" + lang + "/" + name)
	if err != nil && lang != helpFallback {
		return helpFS.ReadFile("help/" + helpFallback + "/" + name)
	}
	return text, err
}

// helpNames lists the page names of one language, sorted.
func helpNames(lang string) []string {
	entries, _ := helpFS.ReadDir("help/" + lang)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	return names
}

// helpTopics lists the help pages in lang: the index first, then the pages
// in the order the index links them, then any page the index does not
// link, by name. A page's title is its first heading, else its name. A
// page only the fallback language has is listed too, in that language.
func helpTopics(lang string) ([]helpTopic, error) {
	names := helpNames(lang)
	for _, name := range helpNames(helpFallback) {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var order []string
	if index, err := helpPage(lang, helpIndex); err == nil {
		order = append(order, helpIndex)
		for _, m := range helpLink.FindAllStringSubmatch(string(index), -1) {
			if slices.Contains(names, m[1]) && !slices.Contains(order, m[1]) {
				order = append(order, m[1])
			}
		}
	}
	for _, name := range names {
		if !slices.Contains(order, name) {
			order = append(order, name)
		}
	}
	topics := make([]helpTopic, 0, len(order))
	for _, name := range order {
		text, err := helpPage(lang, name)
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

// helpTopicsHandler answers GET /help?lang=<code> with the topic list as
// JSON, in that language when the help is written in it.
func (s *server) helpTopicsHandler(w http.ResponseWriter, r *http.Request) {
	lang := helpLang(r.URL.Query().Get("lang"))
	topics, err := helpTopics(lang)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(helpContents{Lang: lang, Languages: helpLanguages(), Topics: topics})
}

// helpPageHandler answers GET /help/{lang}/{name} with the Markdown source
// of one help page. Only a plain page name is looked up: the pages are a
// flat list, and the embedded FS holds nothing else worth reaching.
func (s *server) helpPageHandler(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != path.Base(name) || !strings.HasSuffix(name, ".md") {
		http.NotFound(w, r)
		return
	}
	text, err := helpPage(helpLang(r.PathValue("lang")), name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write(text)
}
