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
	"errors"
	"fmt"
	"net/http"
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
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
	Key     string `json:"key"`
}

// copyeditConfig is the whole copyediting setup, as it sits on disk.
// Active names the connection the pane's dropdown last selected.
type copyeditConfig struct {
	Prompts     []copyeditPrompt `json:"prompts"`
	Connections []apiConnection  `json:"connections"`
	Active      string           `json:"active"`
	// Mode is how a run of several tasks is carried out: all of them in
	// one call, or one call each.
	Mode string `json:"mode"`
	// Selected are the ids of the tasks ticked in the pane. It is kept
	// because the pane is re-rendered on every page switch, and a
	// selection that had to be made again for each page would not be
	// worth making: the same handful of tasks is run over page after
	// page.
	Selected []string `json:"selected"`
}

// How a run of several tasks reaches the model.
//
// Batched is one call carrying every selected task, which sends the page
// once. Per-task is one call per task, each sending the page again --
// except that the page is the cached part of the request (see requestFor),
// so from the second call on the provider may serve it from its cache and
// the difference in cost is far smaller than the difference in requests.
// What the two really differ in is the answer: a model given one task at a
// time attends to it fully, where five tasks in one call are answered in
// one list that tends to be shorter than five lists would be. Which reads
// better is a question about a particular model and a particular set of
// tasks, so it is the user's to answer, not ours to assume.
const (
	modeBatched = "batched"
	modePerTask = "per-task"
)

// runMode is the configured mode, defaulting to one call for the lot. The
// caller must hold s.mu.
func (s *server) runMode() string {
	if s.cfg.Copyedit.Mode == modePerTask {
		return modePerTask
	}
	return modeBatched
}

// setRunMode records how a run of several tasks is to be carried out. The
// caller must hold s.mu.
func (s *server) setRunMode(mode string) error {
	if mode != modeBatched && mode != modePerTask {
		return errors.New("no such run mode")
	}
	s.cfg.Copyedit.Mode = mode
	return s.saveConfig()
}

// modeLabel says in the pane which way a run was carried out, so that a
// list of suggestions can be told apart from the same tasks run the other
// way -- which is the whole point of being able to switch.
func modeLabel(mode string) string {
	if mode == modePerTask {
		return "one call per task"
	}
	return "one call for all tasks"
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
		s.cfg.Copyedit.Prompts[i].Title = title
		s.cfg.Copyedit.Prompts[i].Prompt = prompt
		return s.saveConfig()
	}
	ids := make([]string, 0, len(s.cfg.Copyedit.Prompts))
	for _, p := range s.cfg.Copyedit.Prompts {
		ids = append(ids, p.ID)
	}
	s.cfg.Copyedit.Prompts = append(s.cfg.Copyedit.Prompts, copyeditPrompt{
		ID: nextID("p", ids), Title: title, Prompt: prompt,
	})
	return s.saveConfig()
}

// deletePrompt drops an editing task. The caller must hold s.mu.
func (s *server) deletePrompt(id string) error {
	i := s.promptIndex(id)
	if i < 0 {
		return errors.New("no such editing task")
	}
	s.cfg.Copyedit.Prompts = slices.Delete(s.cfg.Copyedit.Prompts, i, i+1)
	s.cfg.Copyedit.Selected = slices.DeleteFunc(s.cfg.Copyedit.Selected, func(sel string) bool {
		return sel == id
	})
	return s.saveConfig()
}

func (s *server) promptIndex(id string) int {
	return slices.IndexFunc(s.cfg.Copyedit.Prompts, func(p copyeditPrompt) bool {
		return id != "" && p.ID == id
	})
}

func (s *server) connIndex(id string) int {
	return slices.IndexFunc(s.cfg.Copyedit.Connections, func(c apiConnection) bool {
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
			c.Key = s.cfg.Copyedit.Connections[i].Key
		}
		s.cfg.Copyedit.Connections[i] = c
		return s.saveConfig()
	}
	ids := make([]string, 0, len(s.cfg.Copyedit.Connections))
	for _, e := range s.cfg.Copyedit.Connections {
		ids = append(ids, e.ID)
	}
	c.ID = nextID("c", ids)
	s.cfg.Copyedit.Connections = append(s.cfg.Copyedit.Connections, c)
	return s.saveConfig()
}

// deleteConnection drops an API connection, and the active selection with
// it when it was the one selected. The caller must hold s.mu.
func (s *server) deleteConnection(id string) error {
	i := s.connIndex(id)
	if i < 0 {
		return errors.New("no such connection")
	}
	s.cfg.Copyedit.Connections = slices.Delete(s.cfg.Copyedit.Connections, i, i+1)
	if s.cfg.Copyedit.Active == id {
		s.cfg.Copyedit.Active = ""
	}
	return s.saveConfig()
}

// selectedTasks are the ticked tasks that still exist, in the order the
// task list has them. A saved id naming a task that has since been
// deleted -- or a hand-written one naming nothing -- drops out here
// rather than being offered or run. The caller must hold s.mu.
func (s *server) selectedTasks() []string {
	var out []string
	for _, p := range s.cfg.Copyedit.Prompts {
		if slices.Contains(s.cfg.Copyedit.Selected, p.ID) {
			out = append(out, p.ID)
		}
	}
	return out
}

// setSelectedTasks records which tasks are ticked, keeping only the ids
// that name a task. The caller must hold s.mu.
func (s *server) setSelectedTasks(ids []string) error {
	var keep []string
	for _, p := range s.cfg.Copyedit.Prompts {
		if slices.Contains(ids, p.ID) {
			keep = append(keep, p.ID)
		}
	}
	s.cfg.Copyedit.Selected = keep
	return s.saveConfig()
}

// activeConnection is the connection a run goes to: the one the pane's
// dropdown last selected, or the first configured one when that selection
// is gone or was never made. The caller must hold s.mu.
func (s *server) activeConnection() (apiConnection, bool) {
	if i := s.connIndex(s.cfg.Copyedit.Active); i >= 0 {
		return s.cfg.Copyedit.Connections[i], true
	}
	if len(s.cfg.Copyedit.Connections) > 0 {
		return s.cfg.Copyedit.Connections[0], true
	}
	return apiConnection{}, false
}

// setActiveConnection remembers which connection the pane selected. The
// caller must hold s.mu.
func (s *server) setActiveConnection(id string) error {
	if s.connIndex(id) < 0 {
		return errors.New("no such connection")
	}
	s.cfg.Copyedit.Active = id
	return s.saveConfig()
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
	Prompts     []paneTask
	Connections []connectionView
	Active      string
	// AllSelected says the "Select all" box starts ticked: every task is,
	// and clicking it clears them. Any says at least one is, which is
	// what the box shows as its in-between state.
	AllSelected bool
	AnySelected bool
}

// paneTask is an editing task as the pane lists it: the task, and whether
// it is ticked for the next run of several.
type paneTask struct {
	copyeditPrompt
	Selected bool
}

// copyeditPaneView assembles the pane. The caller must hold s.mu.
func (s *server) copyeditPaneView() copyeditPane {
	active, _ := s.activeConnection()
	selected := s.selectedTasks()
	v := copyeditPane{
		Active:      active.ID,
		AllSelected: len(s.cfg.Copyedit.Prompts) > 0 && len(selected) == len(s.cfg.Copyedit.Prompts),
		AnySelected: len(selected) > 0,
	}
	for _, p := range s.cfg.Copyedit.Prompts {
		v.Prompts = append(v.Prompts, paneTask{copyeditPrompt: p, Selected: slices.Contains(selected, p.ID)})
	}
	for _, c := range s.cfg.Copyedit.Connections {
		v.Connections = append(v.Connections, connectionView{
			ID: c.ID, Name: c.Name, Kind: c.Kind, BaseURL: c.BaseURL,
			Model: c.Model, HasKey: c.Key != "", Active: c.ID == active.ID,
		})
	}
	return v
}

// copyeditConfigView is the page data for the editing-tasks config page:
// the tasks themselves, and how a run of several of them is carried out.
type copyeditConfigView struct {
	Prompts []copyeditPrompt
	Mode    string
	PerTask bool
	Message string
	Error   string
}

// copyeditConfigPageView assembles that page. The caller must hold s.mu.
func (s *server) copyeditConfigPageView() copyeditConfigView {
	mode := s.runMode()
	return copyeditConfigView{
		Prompts: s.cfg.Copyedit.Prompts,
		Mode:    mode,
		PerTask: mode == modePerTask,
	}
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
	Tasks []string
	Model string
	// Mode says which way the run was carried out, so that a list can be
	// told from the same tasks run the other way.
	Mode   string
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
	s.render(w, "copyedit-page", s.copyeditConfigPageView())
}

func (s *server) savePromptHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	view := s.copyeditConfigPageView()
	view.Message = "Saved."
	if err := s.savePrompt(r.PostFormValue("id"), r.PostFormValue("title"), r.PostFormValue("prompt")); err != nil {
		view.Message, view.Error = "", err.Error()
	}
	view.Prompts = s.cfg.Copyedit.Prompts
	s.render(w, "copyedit-page", view)
}

func (s *server) deletePromptHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	view := s.copyeditConfigPageView()
	view.Message = "Deleted."
	if err := s.deletePrompt(r.PostFormValue("id")); err != nil {
		view.Message, view.Error = "", err.Error()
	}
	view.Prompts = s.cfg.Copyedit.Prompts
	s.render(w, "copyedit-page", view)
}

// runModeHandler records whether a run of several tasks goes to the model
// in one call or in one call per task.
func (s *server) runModeHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	view := s.copyeditConfigPageView()
	if err := s.setRunMode(r.PostFormValue("mode")); err != nil {
		view.Error = err.Error()
		s.render(w, "copyedit-page", view)
		return
	}
	view = s.copyeditConfigPageView()
	view.Message = "Saved."
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

// selectedTasksHandler records which tasks are ticked. The pane posts it
// whenever a box changes, so the selection outlives the page switch that
// re-renders the pane, and the restart that forgets everything the
// browser held.
func (s *server) selectedTasksHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	if err := s.setSelectedTasks(r.PostForm["selected"]); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	for _, p := range s.cfg.Copyedit.Prompts {
		if slices.Contains(picked, p.ID) {
			prompts = append(prompts, p)
		}
	}
	conn, ok := s.activeConnection()
	mode := s.runMode()
	s.mu.Unlock()

	tasks := tasksFor(prompts)
	view := suggestionsView{Model: conn.Name, Mode: modeLabel(mode)}
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
	path, body := r.PostFormValue("path"), r.PostFormValue("body")
	var sugs []suggestion
	var failed []string
	var err error
	if mode == modePerTask {
		sugs, failed, err = runCopyeditPerTask(conn, tasks, path, body)
	} else {
		sugs, err = runCopyedit(conn, tasks, path, body)
	}
	if err != nil {
		view.Error = err.Error()
		s.render(w, "copyedit-suggestions", view)
		return
	}
	// A run of one call per task can lose a task and keep the rest; what
	// the others found is shown, with the failure beside it.
	if len(failed) > 0 {
		view.Error = strings.Join(failed, "; ")
	}
	view.Groups, view.Count = groupSuggestions(tasks, sugs), len(sugs)
	s.render(w, "copyedit-suggestions", view)
}
