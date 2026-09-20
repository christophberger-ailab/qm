package web

// copyedit.go — the copyediting tool: the editing tasks the user writes,
// the API connections they run on, and the pane that shows what a run came
// back with.
//
// An editing task is a prompt with a title ("Passive voice", "Shorten
// sentences"). The copyedit tab lists the titles; clicking one sends the
// page the editor holds to the selected model and turns the list into the
// suggestions it made. Both the tasks and the connections are the user's
// own, so they live beside the render prefs rather than in the project:
// they are how this user edits, not what this project contains.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// copyeditPrompt is one editing task: the title the pane lists and the
// prompt text the model is given.
type copyeditPrompt struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

// apiConnection is one configured way to reach a model: what to call it,
// which API it speaks, where it lives, which model to ask, and the key it
// takes. Several may be configured; the copyedit pane picks between them.
type apiConnection struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Key     string `json:"key,omitempty"`
}

// copyeditConfig is the whole copyediting setup, as it sits on disk.
// Active names the connection the pane's dropdown last selected.
type copyeditConfig struct {
	Prompts     []copyeditPrompt `json:"prompts"`
	Connections []apiConnection  `json:"connections"`
	Active      string           `json:"active,omitempty"`
}

// copyeditFileForPrefs returns the file that holds the copyediting setup,
// beside the render prefs, or "" when persistence is disabled.
func copyeditFileForPrefs(prefsFile string) string {
	if prefsFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(prefsFile), "copyedit.json")
}

// loadCopyedit reads the copyediting setup. A missing or unreadable file
// just means nothing has been configured yet.
func (s *server) loadCopyedit() {
	s.copyedit = copyeditConfig{}
	if s.copyeditFile == "" {
		return
	}
	if b, err := os.ReadFile(s.copyeditFile); err == nil {
		json.Unmarshal(b, &s.copyedit)
	}
}

// saveCopyedit writes the copyediting setup. The file holds API keys, so
// it is written for its owner alone. The caller must hold s.mu.
func (s *server) saveCopyedit() error {
	if s.copyeditFile == "" {
		return errors.New("no config directory available")
	}
	b, err := json.MarshalIndent(s.copyedit, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.copyeditFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.copyeditFile, b, 0o600)
}

// nextID is the first free id of a series ("p1", "p2", ...). The ids are
// the config's own handles, never shown, so counting is enough: it keeps
// the file readable and the tests predictable.
func nextID(prefix string, taken []string) string {
	for i := 1; ; i++ {
		id := fmt.Sprintf("%s%d", prefix, i)
		if !slices.Contains(taken, id) {
			return id
		}
	}
}

// savePrompt adds an editing task or replaces the one id names. The title
// is what the pane lists and the prompt is what the model is given, so
// neither may be empty. The caller must hold s.mu.
func (s *server) savePrompt(id, title, prompt string) error {
	title, prompt = strings.TrimSpace(title), strings.TrimSpace(prompt)
	if title == "" {
		return errors.New("the editing task needs a title")
	}
	if prompt == "" {
		return errors.New("the editing task needs a prompt")
	}
	if i := s.promptIndex(id); i >= 0 {
		s.copyedit.Prompts[i].Title = title
		s.copyedit.Prompts[i].Prompt = prompt
		return s.saveCopyedit()
	}
	ids := make([]string, 0, len(s.copyedit.Prompts))
	for _, p := range s.copyedit.Prompts {
		ids = append(ids, p.ID)
	}
	s.copyedit.Prompts = append(s.copyedit.Prompts, copyeditPrompt{
		ID: nextID("p", ids), Title: title, Prompt: prompt,
	})
	return s.saveCopyedit()
}

// deletePrompt drops an editing task. The caller must hold s.mu.
func (s *server) deletePrompt(id string) error {
	i := s.promptIndex(id)
	if i < 0 {
		return errors.New("no such editing task")
	}
	s.copyedit.Prompts = slices.Delete(s.copyedit.Prompts, i, i+1)
	return s.saveCopyedit()
}

func (s *server) promptIndex(id string) int {
	return slices.IndexFunc(s.copyedit.Prompts, func(p copyeditPrompt) bool {
		return id != "" && p.ID == id
	})
}

func (s *server) connIndex(id string) int {
	return slices.IndexFunc(s.copyedit.Connections, func(c apiConnection) bool {
		return id != "" && c.ID == id
	})
}

// saveConnection adds an API connection or updates the one id names. An
// empty key on an update keeps the stored one: the form shows a key it
// never sends back, so an empty field means "unchanged", not "cleared".
// The caller must hold s.mu.
func (s *server) saveConnection(c apiConnection) error {
	c.Name = strings.TrimSpace(c.Name)
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.Model = strings.TrimSpace(c.Model)
	c.Key = strings.TrimSpace(c.Key)
	if c.Kind != kindAnthropic {
		c.Kind = kindOpenAI
	}
	if c.Name == "" {
		return errors.New("the connection needs a name")
	}
	if c.BaseURL == "" {
		return errors.New("the connection needs a base URL")
	}
	if c.Model == "" {
		return errors.New("the connection needs a model")
	}
	if i := s.connIndex(c.ID); i >= 0 {
		if c.Key == "" {
			c.Key = s.copyedit.Connections[i].Key
		}
		s.copyedit.Connections[i] = c
		return s.saveCopyedit()
	}
	ids := make([]string, 0, len(s.copyedit.Connections))
	for _, e := range s.copyedit.Connections {
		ids = append(ids, e.ID)
	}
	c.ID = nextID("c", ids)
	s.copyedit.Connections = append(s.copyedit.Connections, c)
	return s.saveCopyedit()
}

// deleteConnection drops an API connection, and the active selection with
// it when it was the one selected. The caller must hold s.mu.
func (s *server) deleteConnection(id string) error {
	i := s.connIndex(id)
	if i < 0 {
		return errors.New("no such connection")
	}
	s.copyedit.Connections = slices.Delete(s.copyedit.Connections, i, i+1)
	if s.copyedit.Active == id {
		s.copyedit.Active = ""
	}
	return s.saveCopyedit()
}

// activeConnection is the connection a run goes to: the one the pane's
// dropdown last selected, or the first configured one when that selection
// is gone or was never made. The caller must hold s.mu.
func (s *server) activeConnection() (apiConnection, bool) {
	if i := s.connIndex(s.copyedit.Active); i >= 0 {
		return s.copyedit.Connections[i], true
	}
	if len(s.copyedit.Connections) > 0 {
		return s.copyedit.Connections[0], true
	}
	return apiConnection{}, false
}

// setActiveConnection remembers which connection the pane selected. The
// caller must hold s.mu.
func (s *server) setActiveConnection(id string) error {
	if s.connIndex(id) < 0 {
		return errors.New("no such connection")
	}
	s.copyedit.Active = id
	return s.saveCopyedit()
}

// connectionView is a connection as the pages show it. The key is not part
// of it: it travels to the API and nowhere else, so it never goes back to
// the browser. HasKey is all the form needs to say that one is stored.
type connectionView struct {
	ID      string
	Name    string
	Kind    string
	BaseURL string
	Model   string
	HasKey  bool
	Active  bool
}

// copyeditPane is what the copyedit tab is built from: the editing tasks
// it lists, the connections its dropdown offers, and the one selected.
type copyeditPane struct {
	Prompts     []copyeditPrompt
	Connections []connectionView
	Active      string
}

// copyeditPaneView assembles the pane. The caller must hold s.mu.
func (s *server) copyeditPaneView() copyeditPane {
	active, _ := s.activeConnection()
	v := copyeditPane{Prompts: s.copyedit.Prompts, Active: active.ID}
	for _, c := range s.copyedit.Connections {
		v.Connections = append(v.Connections, connectionView{
			ID: c.ID, Name: c.Name, Kind: c.Kind, BaseURL: c.BaseURL,
			Model: c.Model, HasKey: c.Key != "", Active: c.ID == active.ID,
		})
	}
	return v
}

// copyeditConfigView is the page data for the editing-tasks config page.
type copyeditConfigView struct {
	Prompts []copyeditPrompt
	Message string
	Error   string
}

// connectionsView is the page data for the API connections config page.
type connectionsView struct {
	Connections []connectionView
	Message     string
	Error       string
}

// suggestionsView is what one copyedit run produced: the tasks that were
// run, their suggestions grouped by task, and — when the run did not get
// that far — why not.
type suggestionsView struct {
	// Tasks are the titles that were run, in the order the config lists
	// them; the pane names them and, for a run of several, offers one
	// filter per task.
	Tasks  []string
	Model  string
	Groups []suggestionGroup
	// Count is how many suggestions came back in all, which is what the
	// head reports and what tells an empty run from a failed one.
	Count int
	Error string
}

// suggestionGroup is one task's findings. A task that found nothing keeps
// its group: a run says what every task it was given came back with, and
// "nothing to change here" is an answer.
type suggestionGroup struct {
	// Task is the title, or "" for the suggestions whose tag named no
	// task that was asked for.
	Task        string
	Suggestions []suggestion
}

// groupSuggestions sorts a run's suggestions into the tasks they came
// from, in the order the tasks were given. Suggestions the model did not
// attribute come last, in a group of their own.
func groupSuggestions(tasks []editingTask, sugs []suggestion) []suggestionGroup {
	groups := make([]suggestionGroup, 0, len(tasks)+1)
	for _, t := range tasks {
		g := suggestionGroup{Task: t.Title}
		for _, s := range sugs {
			if s.Task == t.Title {
				g.Suggestions = append(g.Suggestions, s)
			}
		}
		groups = append(groups, g)
	}
	var loose []suggestion
	for _, s := range sugs {
		if s.Task == "" {
			loose = append(loose, s)
		}
	}
	if len(loose) > 0 {
		groups = append(groups, suggestionGroup{Suggestions: loose})
	}
	return groups
}

// Handlers

func (s *server) copyeditConfigPage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.render(w, "copyedit-page", copyeditConfigView{Prompts: s.copyedit.Prompts})
}

func (s *server) savePromptHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	view := copyeditConfigView{Message: "Saved."}
	if err := s.savePrompt(r.PostFormValue("id"), r.PostFormValue("title"), r.PostFormValue("prompt")); err != nil {
		view.Message, view.Error = "", err.Error()
	}
	view.Prompts = s.copyedit.Prompts
	s.render(w, "copyedit-page", view)
}

func (s *server) deletePromptHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	view := copyeditConfigView{Message: "Deleted."}
	if err := s.deletePrompt(r.PostFormValue("id")); err != nil {
		view.Message, view.Error = "", err.Error()
	}
	view.Prompts = s.copyedit.Prompts
	s.render(w, "copyedit-page", view)
}

func (s *server) connectionsPage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.render(w, "connections-page", connectionsView{Connections: s.copyeditPaneView().Connections})
}

func (s *server) saveConnectionHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	view := connectionsView{Message: "Saved."}
	err := s.saveConnection(apiConnection{
		ID:      r.PostFormValue("id"),
		Name:    r.PostFormValue("name"),
		Kind:    r.PostFormValue("kind"),
		BaseURL: r.PostFormValue("base_url"),
		Model:   r.PostFormValue("model"),
		Key:     r.PostFormValue("key"),
	})
	if err != nil {
		view.Message, view.Error = "", err.Error()
	}
	view.Connections = s.copyeditPaneView().Connections
	s.render(w, "connections-page", view)
}

func (s *server) deleteConnectionHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	view := connectionsView{Message: "Deleted."}
	if err := s.deleteConnection(r.PostFormValue("id")); err != nil {
		view.Message, view.Error = "", err.Error()
	}
	view.Connections = s.copyeditPaneView().Connections
	s.render(w, "connections-page", view)
}

// activeConnectionHandler remembers the model the pane's dropdown picked.
func (s *server) activeConnectionHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	if err := s.setActiveConnection(r.PostFormValue("connection")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// copyeditPromptsHandler serves the list of editing tasks. It is what the
// Back button of the suggestion list asks for, and what the pane is
// refreshed with after the config pages were visited.
func (s *server) copyeditPromptsHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.render(w, "copyedit-prompts", s.copyeditPaneView())
}

// copyeditRunHandler runs one editing task against the text the editor
// holds — the text, not the file, so unsaved edits are what gets read —
// and answers with the suggestion list.
//
// The model call takes as long as it takes, so it runs without the lock:
// the tree, the editor, and the autosave must stay answerable while the
// pane waits.
func (s *server) copyeditRunHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	// The form names the tasks; they are collected in the order the config
	// lists them rather than the order the checkboxes were ticked, so the
	// groups read the same way the task list does.
	picked := r.PostForm["prompt"]
	s.mu.Lock()
	var prompts []copyeditPrompt
	for _, p := range s.copyedit.Prompts {
		if slices.Contains(picked, p.ID) {
			prompts = append(prompts, p)
		}
	}
	conn, ok := s.activeConnection()
	s.mu.Unlock()

	tasks := tasksFor(prompts)
	view := suggestionsView{Model: conn.Name}
	for _, t := range tasks {
		view.Tasks = append(view.Tasks, t.Title)
	}
	switch {
	case len(tasks) == 0:
		view.Error = "No editing task was selected."
	case !ok:
		view.Error = "No API connection is configured. Add one under Config → Copyedit: API connections."
	}
	if view.Error != "" {
		s.render(w, "copyedit-suggestions", view)
		return
	}
	sugs, err := runCopyedit(conn, tasks, r.PostFormValue("path"), r.PostFormValue("body"))
	if err != nil {
		view.Error = err.Error()
		s.render(w, "copyedit-suggestions", view)
		return
	}
	view.Groups, view.Count = groupSuggestions(tasks, sugs), len(sugs)
	s.render(w, "copyedit-suggestions", view)
}
