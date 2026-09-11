package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html/template"
	iofs "io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/christophberger-ailab/qm/internal/bookrender"
	"github.com/christophberger-ailab/qm/internal/project"
	"github.com/christophberger-ailab/qm/internal/qmcore"
)

//go:embed assets
var assets embed.FS

// server holds the currently open project. A mutex serializes all access:
// this is a single-user local tool.
type server struct {
	mux  *http.ServeMux
	tmpl *template.Template

	mu   sync.Mutex
	root string

	// fp is the on-disk fingerprint of the project state that was last
	// rendered; /watch re-renders only when the disk no longer matches.
	fp string

	// prefsFile persists the render selection per project root across
	// restarts; empty disables persistence. prefs holds its content.
	prefsFile string
	prefs     map[string]projectPrefs

	// recentFile persists the projects opened through the Open field,
	// most recent first; empty keeps the list to this run. recent holds
	// its content.
	recentFile string
	recent     []string

	// cssDir holds the custom preview stylesheets, next to the render
	// prefs; empty disables persistence and serves the baked-in default
	// from memory instead.
	cssDir string

	// base is the text the open editor started from. A save is replayed
	// onto the tree's own writes against it (see project.Rebase); it is
	// what makes an autosave posted after a move keep the move.
	base editorBase

	// job is the background render. It has its own lock: a render takes
	// minutes and must not block the tree handlers.
	job job

	// index is the word index the search field is answered from. Like the
	// render, it is built in the background and locked separately.
	index searchIndex
}

func newServer(prefsFile string) (*server, error) {
	funcs := template.FuncMap{
		"group": func(parent string, pages []*project.Page) any {
			return struct {
				Parent string
				Pages  []*project.Page
			}{parent, pages}
		},
	}
	tmpl, err := template.New("").Funcs(funcs).ParseFS(assets, "assets/templates/*.tmpl")
	if err != nil {
		return nil, err
	}
	s := &server{
		mux:        http.NewServeMux(),
		tmpl:       tmpl,
		prefsFile:  prefsFile,
		recentFile: recentFileForPrefs(prefsFile),
		cssDir:     cssDirForPrefs(prefsFile),
	}
	ensureDefaultCSS(s.cssDir)
	s.loadPrefs()
	s.loadRecent()
	static, err := iofs.Sub(assets, "assets/static")
	if err != nil {
		return nil, err
	}
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	s.mux.HandleFunc("GET /{$}", s.page)
	s.mux.HandleFunc("GET /config", s.config)
	s.mux.HandleFunc("GET /config/preview-css", s.previewCSSEditor)
	s.mux.HandleFunc("POST /config/preview-css", s.savePreviewCSSHandler)
	s.mux.HandleFunc("POST /config/preview-css/new", s.newPreviewCSSHandler)
	s.mux.HandleFunc("GET /config/preview.css", s.previewStylesheet)
	s.mux.HandleFunc("POST /config/active-css", s.setActiveCSSHandler)
	s.mux.HandleFunc("POST /open", s.open)
	s.mux.HandleFunc("GET /tree", s.treeHandler)
	s.mux.HandleFunc("GET /watch", s.watch)
	s.mux.HandleFunc("POST /move", s.move)
	s.mux.HandleFunc("POST /create", s.create)
	s.mux.HandleFunc("POST /delete", s.delete)
	s.mux.HandleFunc("GET /content", s.content)
	s.mux.HandleFunc("GET /search", s.search)
	s.mux.HandleFunc("GET /complete", s.complete)
	s.mux.HandleFunc("GET /media/{path...}", s.media)
	s.mux.HandleFunc("POST /save", s.save)
	s.mux.HandleFunc("POST /render", s.startRender)
	s.mux.HandleFunc("POST /render/select", s.selectRender)
	s.mux.HandleFunc("GET /render/status", s.renderStatus)
	s.mux.HandleFunc("GET /git", s.gitStatusHandler)
	s.mux.HandleFunc("GET /git/diff", s.gitDiffHandler)
	s.mux.HandleFunc("POST /git/stage", s.gitStageHandler)
	s.mux.HandleFunc("POST /git/unstage", s.gitUnstageHandler)
	s.mux.HandleFunc("POST /git/commit", s.gitCommitHandler)
	s.mux.HandleFunc("POST /git/push", s.gitPushHandler)
	return s, nil
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// state bundles everything the page templates need.
type state struct {
	Root string
	// Open is the Open field: the project it shows and the recently
	// opened ones its dropdown offers.
	Open openView
	Tree *project.Tree
	// Page is the page the editor opens with — the one this project last
	// had open — or nil when there is none to restore.
	Page   *contentView
	Render renderView
	Error  string
	// CSSFiles are the custom preview stylesheets available; ActiveCSS is
	// the one currently shown by the preview. A dropdown to switch between
	// them appears above the preview only when there is more than one.
	CSSFiles  []string
	ActiveCSS string
}

// openView is the Open field in the top bar. The field itself shows only
// the last element of a path — the project's folder name is what tells one
// project from another, and the rest of the path is what makes the field
// too narrow to read — while the full path travels with it, so a submit
// still opens the project the label stands for. Recent lists the projects
// opened before, most recent first.
type openView struct {
	// Path is the full path of the open project, "" when none is open.
	Path string
	// Label is what the field shows: the last element of Path.
	Label string
	// Recent are the projects the dropdown offers.
	Recent []recentPath
}

// recentPath is one entry of the Open field's dropdown: the folder name it
// shows, and the full path it opens.
type recentPath struct {
	Path  string
	Label string
}

// pathLabel is the last element of a path: what the Open field shows. A
// path that has no last element to speak of — the root directory, an empty
// path — is shown as it is.
func pathLabel(p string) string {
	if p == "" {
		return ""
	}
	base := filepath.Base(p)
	if base == "." || base == string(filepath.Separator) {
		return p
	}
	return base
}

// openViewFor assembles the Open field from the project now open and the
// recently opened ones. The caller must hold s.mu.
func (s *server) openViewFor() openView {
	v := openView{Path: s.root, Label: pathLabel(s.root)}
	for _, p := range s.recent {
		v.Recent = append(v.Recent, recentPath{Path: p, Label: pathLabel(p)})
	}
	return v
}

// contentView is the editor pane: a page's title, its project-relative
// path, and its text, plus the custom stylesheet choices the preview
// dropdown above it needs.
type contentView struct {
	Title     string
	Path      string
	Body      string
	CSSFiles  []string
	ActiveCSS string
}

// editorBase is the text the browser's editor is working from: the page it
// holds, and the bytes it started with. "Started with" means the last text
// that passed between the two — what the server served, or what the editor
// last posted — because that is the text the user's next edit is built on,
// and so the one a save has to be replayed against.
//
// The tool is single-user and local (one editor, one project at a time), so
// one of these describes the whole client. An empty path means there is
// nothing to replay against, and a save then writes straight through, the
// way it always did.
type editorBase struct {
	path string
	body []byte
}

// rebaseOn records the text the editor is now working from. The caller must
// hold s.mu.
func (s *server) rebaseOn(rel string, body []byte) {
	s.base = editorBase{path: rel, body: bytes.Clone(body)}
}

// rebaseOnState records the editor pane a whole-page response carries, if
// it carries one. Only the two handlers that send the pane to the browser
// call this: load() runs on every tree re-render as well, and recording
// there would replace the text the editor is really holding with whatever
// the tree op just wrote — which is precisely the text a save has to be
// replayed against. The caller must hold s.mu.
func (s *server) rebaseOnState(st state) {
	if st.Page != nil {
		s.rebaseOn(st.Page.Path, []byte(st.Page.Body))
	}
}

// forgetBase drops the recorded text, so the next save writes through
// rather than replay against a page the editor no longer holds. The caller
// must hold s.mu.
func (s *server) forgetBase() {
	s.base = editorBase{}
}

// configView is the list of configuration entries shown by /config.
type configView struct {
	Entries []configEntry
}

// configEntry is one link from the config overview to a concrete editor.
type configEntry struct {
	Title       string
	Description string
	Href        string
}

// previewCSSView is the page data for the custom preview stylesheet editor.
type previewCSSView struct {
	// File is the stylesheet currently being edited; Files lists all of
	// them so the editor can offer a way to switch and to add another.
	File    string
	Files   []string
	CSS     string
	Message string
	Error   string
}

// renderView is what the render panel shows: the project's topics with the
// audiences each of them takes part in, and the project's output formats.
type renderView struct {
	Books   []bookView
	Formats []checkbox
}

// bookView is one topic in the render panel. Profiles holds the audiences
// the topic declares, which is what varies per topic; the formats are a
// project-wide choice and live beside the list.
type bookView struct {
	Name     string
	Selected bool
	Profiles []checkbox
}

// checkbox is a named on/off choice in the render panel.
type checkbox struct {
	Name     string
	Selected bool
}

// load builds the current template state; the caller must hold s.mu.
func (s *server) load() (state, error) {
	st := state{Root: s.root, Open: s.openViewFor(), CSSFiles: s.cssFiles(), ActiveCSS: s.activeCSS()}
	if s.root == "" {
		return st, nil
	}
	st.Render = s.renderView()
	st.Page = s.lastPage()
	tree, err := project.Load(s.root)
	if tree != nil {
		flattenTreeSpans(tree.Pages, spanSymbols(s.loadCSS(st.ActiveCSS)))
	}
	st.Tree = tree
	return st, err
}

// lastPage is the editor pane the app page starts with: the page this
// project last had open. It returns nil when the project never had one, or
// when the page has since been deleted, moved, or renamed — the app then
// opens with an empty editor instead of an error. The caller must hold
// s.mu.
func (s *server) lastPage() *contentView {
	rel := s.prefsFor(s.root).Page
	if rel == "" {
		return nil
	}
	abs, err := s.resolvePath(rel)
	if err != nil {
		return nil
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	return &contentView{
		Title:     project.ParseFrontmatter(body).Title,
		Path:      rel,
		Body:      string(body),
		CSSFiles:  s.cssFiles(),
		ActiveCSS: s.activeCSS(),
	}
}

// renderView assembles the render panel from the project's topics and the
// axes each of them declares. The caller must hold s.mu.
func (s *server) renderView() renderView {
	prefs := s.prefsFor(s.root)
	topics, _ := qmcore.AxisValues(s.root, qmcore.AxisTopic)
	formats, _ := qmcore.AxisValues(s.root, qmcore.AxisFormat)

	var v renderView
	for _, name := range topics {
		b := bookView{Name: name, Selected: slices.Contains(prefs.Topics, name)}
		on := prefs.audiencesFor(name, s.audiencesOf(name))
		for _, a := range s.audiencesOf(name) {
			b.Profiles = append(b.Profiles, checkbox{a, slices.Contains(on, a)})
		}
		v.Books = append(v.Books, b)
	}
	for _, f := range formats {
		v.Formats = append(v.Formats, checkbox{f, slices.Contains(prefs.Formats, f)})
	}
	return v
}

// audiencesOf returns the audiences a topic takes part in: what its profile
// declares under `qm: audiences:`, or every audience the project offers.
func (s *server) audiencesOf(topic string) []string {
	all, _ := qmcore.AxisValues(s.root, qmcore.AxisAudience)
	p, err := qmcore.LoadProfile(s.root, qmcore.AxisTopic.ProfileName(topic))
	if err != nil || len(p.QM.Audiences) == 0 {
		return all
	}
	var out []string
	for _, a := range all {
		if slices.Contains(p.QM.Audiences, a) {
			out = append(out, a)
		}
	}
	return out
}

func (s *server) render(w http.ResponseWriter, name string, data any) {
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// fingerprint hashes everything the tree pane is rendered from: the paths,
// sizes, and mtimes of the project's .qmd files and _quarto*.yml configs.
// Equal fingerprints mean no refresh is needed.
func fingerprint(root string) string {
	h := fnv.New64a()
	filepath.WalkDir(root, func(p string, d iofs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries just drop out of the hash
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		qmd := strings.HasSuffix(name, ".qmd") && !strings.HasPrefix(name, "_")
		cfg := filepath.Dir(p) == root && strings.HasSuffix(name, ".yml") &&
			(name == "_quarto.yml" || strings.HasPrefix(name, "_quarto-"))
		if !qmd && !cfg {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			fmt.Fprintf(h, "%s|%d|%d;", p, fi.Size(), fi.ModTime().UnixNano())
		}
		return nil
	})
	return strconv.FormatUint(h.Sum64(), 16)
}

// rememberFP records the current on-disk fingerprint as rendered, so
// /watch stays quiet until something changes outside the responses we
// produce ourselves. It is also where the search index learns that the
// project moved on: the fingerprint is exactly the state both are built
// from. The caller must hold s.mu.
func (s *server) rememberFP() {
	if s.root != "" {
		s.fp = fingerprint(s.root)
		s.index.rebuild(s.root, s.fp)
	}
}

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rememberFP()
	st, err := s.load()
	if err != nil {
		st.Error = err.Error()
	}
	s.rebaseOnState(st)
	s.render(w, "page", st)
}

func (s *server) config(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.render(w, "config-page", configView{Entries: []configEntry{{
		Title:       "Preview: Custom CSS",
		Description: "Override the built-in Markdown preview styles.",
		Href:        "/config/preview-css",
	}}})
}

func (s *server) previewCSSEditor(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.cssFileParam(r.URL.Query().Get("file"))
	s.render(w, "preview-css-page", previewCSSView{File: file, Files: s.cssFiles(), CSS: s.loadCSS(file)})
}

// cssFileParam validates a requested stylesheet name against the ones that
// exist, falling back to the active stylesheet when the request names none
// or an unknown one. The caller must hold s.mu.
func (s *server) cssFileParam(name string) string {
	if name != "" && slices.Contains(s.cssFiles(), name) {
		return name
	}
	return s.activeCSS()
}

// savePreviewCSSHandler writes the named stylesheet. It parses explicitly
// because the stylesheet is the user's own text, and a failed or malformed
// form must not read as "save succeeded" by replacing it with an empty file.
func (s *server) savePreviewCSSHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := r.ParseForm(); err != nil {
		file := s.cssFileParam(r.FormValue("file"))
		s.render(w, "preview-css-page", previewCSSView{File: file, Files: s.cssFiles(), CSS: s.loadCSS(file), Error: err.Error()})
		return
	}
	file := s.cssFileParam(r.PostForm.Get("file"))
	if !r.PostForm.Has("css") {
		s.render(w, "preview-css-page", previewCSSView{File: file, Files: s.cssFiles(), CSS: s.loadCSS(file), Error: "missing css field"})
		return
	}
	css := r.PostForm.Get("css")
	view := previewCSSView{File: file, Files: s.cssFiles(), CSS: css, Message: "Saved."}
	if err := s.saveCSS(file, css); err != nil {
		view.Message = ""
		view.Error = err.Error()
	}
	s.render(w, "preview-css-page", view)
}

// newPreviewCSSHandler adds an alternate custom stylesheet, named by the
// form, and opens it in the editor.
func (s *server) newPreviewCSSHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	name, err := s.createCSS(r.FormValue("name"))
	if err != nil {
		file := s.activeCSS()
		s.render(w, "preview-css-page", previewCSSView{File: file, Files: s.cssFiles(), CSS: s.loadCSS(file), Error: err.Error()})
		return
	}
	s.render(w, "preview-css-page", previewCSSView{File: name, Files: s.cssFiles(), CSS: s.loadCSS(name), Message: "Created."})
}

// setActiveCSSHandler remembers which stylesheet the live preview shows.
// It is posted by the dropdown above the preview whenever the user
// switches stylesheets.
func (s *server) setActiveCSSHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	if err := s.setActiveCSS(r.FormValue("file")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) previewStylesheet(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "text/css")
	// The stylesheet changes underfoot, so a stale browser copy reads like a lost save.
	w.Header().Set("Cache-Control", "no-store")
	file := s.cssFileParam(r.URL.Query().Get("file"))
	fmt.Fprint(w, s.loadCSS(file))
}

func (s *server) open(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.setRoot(strings.TrimSpace(r.FormValue("path"))); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.rememberFP()
	st, err := s.load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.rebaseOnState(st)
	s.render(w, "main", st)
	// The top bar is not reached by the #main swap, so the parts of it
	// that describe the project just opened are sent out of band: the
	// render panel, the Git panel, and the Open field, whose label and
	// dropdown have both moved on.
	fmt.Fprint(w, `<div hx-swap-oob="innerHTML:#render-panel">`)
	s.render(w, "render", st)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<div hx-swap-oob="innerHTML:#git-panel">`)
	s.render(w, "git", st)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<div hx-swap-oob="innerHTML:#open-panel">`)
	s.render(w, "open-form", st)
	fmt.Fprint(w, `</div>`)
}

// setRoot switches to the project at dir, which must name an existing
// directory. The caller must hold s.mu (or not be serving yet).
func (s *server) setRoot(dir string) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	fi, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", root)
	}
	s.root = root
	s.rememberRoot(root)
	// The editor pane is replaced along with the project, so whatever text
	// it held belongs to the project being left.
	s.forgetBase()
	return nil
}

func (s *server) treeHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.renderTree(w, "")
}

// renderTree renders the tree fragment, prefixed with an error banner if
// msg is non-empty. The caller must hold s.mu.
func (s *server) renderTree(w http.ResponseWriter, msg string) {
	s.rememberFP()
	st, err := s.load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if st.Tree == nil {
		http.Error(w, "no project open", http.StatusBadRequest)
		return
	}
	st.Error = msg
	s.render(w, "treewrap", st)
}

// watch is polled by the client. It re-renders the tree only when the
// project changed on disk outside the sorter; 204 tells htmx to leave the
// page alone.
func (s *server) watch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == "" || fingerprint(s.root) == s.fp {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.renderTree(w, "")
}

// apply runs op on a freshly loaded tree and responds with the updated
// tree. Errors from op appear as a banner above the (reverted) tree.
func (s *server) apply(w http.ResponseWriter, op func(*project.Tree) error) {
	tree, err := project.Load(s.root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := op(tree); err != nil {
		s.renderTree(w, err.Error())
		return
	}
	s.renderTree(w, "")
}

// move drags a page to another place in the tree — including into another
// book, which renames its file on disk.
//
// The editor keeps the page it has open under the path the file had when
// it was opened, and that path is what autosave posts to. A move that
// renames the file out from under an open editor therefore has to tell the
// editor where its page went; otherwise every autosave from then on lands
// on a path that no longer exists, every edit made after the move is lost
// with the pane, and the only sign of it is the small "Save failed" beside
// the heading. The renames Move reports are what the new path is followed
// through — the moved page itself, and any page below it when a whole
// section moved.
func (s *server) move(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pos, err := strconv.Atoi(r.FormValue("pos"))
	if err != nil {
		http.Error(w, "bad pos", http.StatusBadRequest)
		return
	}
	// The page the editor has open, as it stands before the move. The
	// browser sends it along; the page this project last opened is the
	// fallback for a client that does not.
	open := r.FormValue("open")
	if open == "" {
		open = s.prefsFor(s.root).Page
	}

	var renames []project.Rename
	s.apply(w, func(t *project.Tree) error {
		var err error
		renames, err = t.Move(r.FormValue("src"), r.FormValue("parent"), pos)
		return err
	})

	// Renames may have happened even when Move then failed part-way, so
	// the editor is re-pointed on what actually moved rather than on
	// whether the whole move succeeded.
	if open == "" || len(renames) == 0 {
		return
	}
	moved := project.Remap(renames, open)
	if moved == open {
		return
	}
	s.rememberPage(moved)
	// The editor's text did not change with the rename, only where it
	// belongs, so the text a save is replayed against follows the file.
	if s.base.path == open {
		s.base.path = moved
	}
	s.renderPathOOB(w, moved)
}

// renderPathOOB re-points the open editor at rel: the hidden field
// autosave posts, the heading above it, and the reload button that reads
// the page from disk. The editor's text is deliberately left alone — it
// holds edits that are not on disk yet, and this response exists to get
// them saved, not to discard them. The caller must hold s.mu.
func (s *server) renderPathOOB(w http.ResponseWriter, rel string) {
	title := rel
	if abs, err := s.resolvePath(rel); err == nil {
		if body, err := os.ReadFile(abs); err == nil {
			title = project.ParseFrontmatter(body).Title
		}
	}
	s.render(w, "content-path-oob", struct{ Title, Path string }{title, rel})
}

func (s *server) create(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := r.FormValue("name")
	if name == "" {
		name = r.Header.Get("HX-Prompt") // per-node ＋ button
	}
	title := r.FormValue("title")
	if title == "" {
		title = name
	}
	// The top-bar form inserts after the page selected in the tree (its
	// path travels as "after"); the per-node ＋ button appends a child to
	// its parent instead.
	parent, after := r.FormValue("parent"), r.FormValue("after")
	s.apply(w, func(t *project.Tree) error {
		var err error
		if parent == "" && after != "" {
			_, err = t.CreatePageAfter(after, name, title)
		} else {
			_, err = t.CreatePage(parent, name, title)
		}
		return err
	})
}

func (s *server) delete(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rel := r.FormValue("path")
	s.apply(w, func(t *project.Tree) error {
		if err := t.DeletePage(rel); err != nil {
			return err
		}
		s.forgetPage(rel)
		// A page in the trash is no longer the one the editor holds.
		if s.base.path == rel {
			s.forgetBase()
		}
		return nil
	})
}

// resolvePath validates rel as a page path relative to the open project and
// returns its absolute location on disk. The caller must hold s.mu.
func (s *server) resolvePath(rel string) (string, error) {
	if s.root == "" {
		return "", fmt.Errorf("no project open")
	}
	if clean := path.Clean(rel); clean != rel || path.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid path")
	}
	return filepath.Join(s.root, filepath.FromSlash(rel)), nil
}

// mediaExts are the file types /media serves. The route exists so that the
// preview can show a page's images; keeping it to what an <img> displays
// stops it from becoming a reader for the rest of the project.
var mediaExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".avif": true, ".svg": true,
}

// media serves an image out of the open project. The preview rewrites the
// image paths of a page to this route: the pages address their media
// website-absolute (`/assets/images/x.png`, relative to the project root,
// which is what makes the flattened book render), and the browser has no
// other way to reach a file on disk.
func (s *server) media(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rel := r.PathValue("path")
	abs, err := s.resolvePath(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !mediaExts[strings.ToLower(path.Ext(rel))] {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, abs)
}

func (s *server) content(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rel := r.URL.Query().Get("path")
	abs, err := s.resolvePath(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	title := project.ParseFrontmatter(body).Title
	// Opening a page is what makes it the one to come back to, and its
	// text on disk is what the editor now works from.
	s.rememberPage(rel)
	s.rebaseOn(rel, body)
	s.render(w, "content", contentView{
		Title:     title,
		Path:      rel,
		Body:      string(body),
		CSSFiles:  s.cssFiles(),
		ActiveCSS: s.activeCSS(),
	})
	// The reload button also refreshes the tree: outside edits may have
	// changed titles or the chapter order.
	if r.URL.Query().Get("reload") != "" {
		s.renderTreeOOB(w)
	}
}

// save writes the edited body back to an existing page. The editor pane is
// left untouched so autosave never steals the cursor; only the heading is
// updated out of band, plus the tree if the title changed.
//
// The text arriving here was typed on top of the page as it stood when the
// editor opened it, and the tree may have written to the same file since:
// every move and create renumbers a sibling group's `order:`, and a move to
// another depth shifts the page's headings. Writing the editor's text out
// as it stands would undo that — drag a chapter into another book, type one
// character, and the move is back where it started. So the edit is replayed
// onto what the tree wrote rather than laid over it (see project.Rebase),
// and a change to the file that the tree cannot account for is answered as
// a conflict instead of being overwritten.
func (s *server) save(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rel := r.FormValue("path")
	abs, err := s.resolvePath(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	old, err := os.ReadFile(abs)
	if err != nil {
		http.Error(w, "no such page", http.StatusBadRequest)
		return
	}
	posted := []byte(r.FormValue("body"))
	body := posted
	// force is the user answering a conflict with "mine wins". It is the
	// only way past the check, and it has to be asked for.
	if r.FormValue("force") == "" && s.base.path == rel && s.base.body != nil {
		res, ok := project.Rebase(posted, s.base.body, old)
		if !ok {
			http.Error(w, "the file changed on disk since this page was opened", http.StatusConflict)
			return
		}
		body = res.Body
		// The editor still shows the text from before the replay. Telling
		// it what was put back is what lets it say so, rather than leave
		// the user looking at heading levels the file no longer has.
		if res.HeadingDelta != 0 || res.OrderChanged {
			w.Header().Set("HX-Trigger", rebaseEvent(res))
		}
	}
	if err := os.WriteFile(abs, body, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The editor keeps the text it posted, not the text that was written,
	// so that is what the next save is replayed against. A replay that had
	// to put something back therefore does so again on every save, which
	// costs nothing and keeps the file right until the pane is reloaded.
	s.rebaseOn(rel, posted)
	// The page just changed under the search index, which the fingerprint
	// alone may not show: an edit that keeps the page's size within one
	// mtime tick hashes to what it replaced.
	s.index.expire()
	title := project.ParseFrontmatter(body).Title
	fmt.Fprint(w, `<h2 class="content-title" id="content-title" hx-swap-oob="true">`)
	s.render(w, "content-heading", struct{ Title, Path string }{title, rel})
	fmt.Fprint(w, `</h2>`)
	if title != project.ParseFrontmatter(old).Title {
		s.renderTreeOOB(w)
	}
	// Our own write must not look like an outside change to /watch.
	s.rememberFP()
}

// rebaseEvent spells a replay out as the htmx event the editor listens
// for, so that the pane can report what the save had to put back.
func rebaseEvent(res project.Rebased) string {
	b, err := json.Marshal(map[string]any{
		"qm:rebased": map[string]any{
			"headings": res.HeadingDelta,
			"order":    res.OrderChanged,
		},
	})
	if err != nil {
		return ""
	}
	return string(b)
}

// formValues reads the render panel's form into the given preferences,
// replacing their render selection and leaving everything else the project
// remembers — the open page — alone. The audience boxes of a topic are
// named "profile.<topic>" so that each topic keeps its own set.
func formValues(p projectPrefs, form map[string][]string, root string) projectPrefs {
	p.Topics = form["book"]
	p.Formats = form["format"]
	p.Audiences = map[string][]string{}
	topics, _ := qmcore.AxisValues(root, qmcore.AxisTopic)
	for _, t := range topics {
		if sel := form["profile."+t]; len(sel) > 0 {
			p.Audiences[t] = sel
		} else {
			// An empty entry is a real choice and must not fall back to
			// "every audience the topic declares".
			p.Audiences[t] = []string{}
		}
	}
	return p
}

// selectRender remembers the render selection without starting anything.
// The panel posts here on every change, so the choice survives a restart
// even if the user never presses Render.
func (s *server) selectRender(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == "" {
		http.Error(w, "no project open", http.StatusBadRequest)
		return
	}
	r.ParseForm()
	s.prefs[s.root] = formValues(s.prefsFor(s.root), r.Form, s.root)
	s.savePrefs()
	w.WriteHeader(http.StatusNoContent)
}

// startRender saves the selection and kicks off the background render,
// answering with the log panel that polls /render/status for progress.
func (s *server) startRender(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == "" {
		http.Error(w, "no project open", http.StatusBadRequest)
		return
	}
	r.ParseForm()
	prefs := formValues(s.prefsFor(s.root), r.Form, s.root)
	s.prefs[s.root] = prefs
	s.savePrefs()

	switch {
	case len(prefs.Topics) == 0:
		s.renderLog(w, jobState{Lines: []string{"select at least one topic to render"}, Failed: true})
		return
	case len(prefs.Formats) == 0:
		s.renderLog(w, jobState{Lines: []string{"select at least one output format"}, Failed: true})
		return
	}

	// The panel's per-topic audiences cannot go into one matrix request —
	// that would apply every topic's audiences to every other topic — so the
	// matrix is built per topic and the results are concatenated.
	var sels []qmcore.Selection
	for _, t := range prefs.Topics {
		part, err := qmcore.BuildMatrix(s.root, qmcore.Matrix{
			Topics:    []string{t},
			Formats:   prefs.Formats,
			Audiences: prefs.audiencesFor(t, s.audiencesOf(t)),
		})
		if err != nil {
			s.renderLog(w, jobState{Lines: []string{err.Error()}, Failed: true})
			return
		}
		sels = append(sels, part...)
	}

	opts := bookrender.Options{Root: s.root, Selections: sels}
	if !s.job.start(opts) {
		s.renderLog(w, s.job.state())
		return
	}
	s.renderLog(w, s.job.state())
}

// renderStatus is polled by the log panel while a render runs.
func (s *server) renderStatus(w http.ResponseWriter, r *http.Request) {
	s.renderLog(w, s.job.state())
}

func (s *server) renderLog(w http.ResponseWriter, st jobState) {
	s.render(w, "render-log", st)
}

// renderTreeOOB appends an out-of-band refresh of the tree pane to the
// response. The caller must hold s.mu.
func (s *server) renderTreeOOB(w http.ResponseWriter) {
	s.rememberFP()
	if st, err := s.load(); err == nil && st.Tree != nil {
		fmt.Fprint(w, `<div hx-swap-oob="innerHTML:#tree">`)
		s.render(w, "treewrap", st)
		fmt.Fprint(w, `</div>`)
	}
}
