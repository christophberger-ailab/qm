package web

import (
	"embed"
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

	"github.com/cboct/qm/internal/bookrender"
	"github.com/cboct/qm/internal/project"
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
	prefs     map[string]renderPrefs

	// job is the background render. It has its own lock: a render takes
	// minutes and must not block the tree handlers.
	job job
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
	s := &server{mux: http.NewServeMux(), tmpl: tmpl, prefsFile: prefsFile}
	s.loadPrefs()
	static, err := iofs.Sub(assets, "assets/static")
	if err != nil {
		return nil, err
	}
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	s.mux.HandleFunc("GET /{$}", s.page)
	s.mux.HandleFunc("POST /open", s.open)
	s.mux.HandleFunc("GET /tree", s.treeHandler)
	s.mux.HandleFunc("GET /watch", s.watch)
	s.mux.HandleFunc("POST /move", s.move)
	s.mux.HandleFunc("POST /create", s.create)
	s.mux.HandleFunc("POST /delete", s.delete)
	s.mux.HandleFunc("GET /content", s.content)
	s.mux.HandleFunc("GET /media/{path...}", s.media)
	s.mux.HandleFunc("POST /save", s.save)
	s.mux.HandleFunc("POST /render", s.startRender)
	s.mux.HandleFunc("POST /render/select", s.selectRender)
	s.mux.HandleFunc("GET /render/status", s.renderStatus)
	return s, nil
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// state bundles everything the page templates need.
type state struct {
	Root   string
	Tree   *project.Tree
	Render renderView
	Error  string
}

// renderView is what the render panel shows: the project's book folders
// with the name-matched profiles and formats currently selected for them.
type renderView struct {
	Books   []bookView
	Formats []checkbox
	Slides  bool
}

// bookView is one book folder in the render panel. Profiles holds only the
// Quarto profiles that belong to the folder by name.
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
	st := state{Root: s.root}
	if s.root == "" {
		return st, nil
	}
	st.Render = s.renderView()
	tree, err := project.Load(s.root)
	st.Tree = tree
	return st, err
}

// renderView assembles the render panel from the project's book folders and
// book profiles, marked up with the saved selection. The caller must hold
// s.mu.
func (s *server) renderView() renderView {
	prefs := s.prefsFor(s.root)
	available, err := project.Profiles(s.root)
	if err != nil {
		available = nil
	}

	v := renderView{Slides: prefs.Slides}
	for _, name := range bookrender.Books(s.root) {
		b := bookView{Name: name, Selected: slices.Contains(prefs.Books, name)}
		matching := bookrender.DefaultProfiles(name, available)
		on := prefs.profilesFor(name, available)
		for _, p := range matching {
			b.Profiles = append(b.Profiles, checkbox{p, slices.Contains(on, p)})
		}
		v.Books = append(v.Books, b)
	}
	for _, f := range renderFormats {
		v.Formats = append(v.Formats, checkbox{f, slices.Contains(prefs.Formats, f)})
	}
	return v
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
// produce ourselves. The caller must hold s.mu.
func (s *server) rememberFP() {
	if s.root != "" {
		s.fp = fingerprint(s.root)
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
	s.render(w, "page", st)
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
	s.render(w, "main", st)
	// The panel lives in the header, which the #main swap does not reach.
	fmt.Fprint(w, `<div hx-swap-oob="innerHTML:#render-panel">`)
	s.render(w, "render", st)
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

func (s *server) move(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pos, err := strconv.Atoi(r.FormValue("pos"))
	if err != nil {
		http.Error(w, "bad pos", http.StatusBadRequest)
		return
	}
	s.apply(w, func(t *project.Tree) error {
		return t.Move(r.FormValue("src"), r.FormValue("parent"), pos)
	})
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
	s.apply(w, func(t *project.Tree) error {
		return t.DeletePage(r.FormValue("path"))
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
	s.render(w, "content", struct {
		Title, Path, Body string
	}{title, rel, string(body)})
	// The reload button also refreshes the tree: outside edits may have
	// changed titles or the chapter order.
	if r.URL.Query().Get("reload") != "" {
		s.renderTreeOOB(w)
	}
}

// save writes the edited body back to an existing page. The editor pane is
// left untouched so autosave never steals the cursor; only the heading is
// updated out of band, plus the tree if the title changed.
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
	body := []byte(r.FormValue("body"))
	if err := os.WriteFile(abs, body, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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

// formValues reads the render panel's form into a selection. The profile
// boxes of a book are named "profile.<book>" so that each book keeps its
// own set.
func formValues(form map[string][]string, root string) renderPrefs {
	p := renderPrefs{
		Books:    form["book"],
		Formats:  form["format"],
		Profiles: map[string][]string{},
		Slides:   len(form["slides"]) > 0,
	}
	for _, b := range bookrender.Books(root) {
		if sel := form["profile."+b]; len(sel) > 0 {
			p.Profiles[b] = sel
		} else {
			// An empty entry is a real choice — "render this book with no
			// profile" — and must not fall back to the name-matched default.
			p.Profiles[b] = []string{}
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
	s.prefs[s.root] = formValues(r.Form, s.root)
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
	prefs := formValues(r.Form, s.root)
	s.prefs[s.root] = prefs
	s.savePrefs()

	switch {
	case len(prefs.Books) == 0:
		s.renderLog(w, jobState{Lines: []string{"select at least one book to render"}, Failed: true})
		return
	case len(prefs.Formats) == 0 && !prefs.Slides:
		s.renderLog(w, jobState{Lines: []string{"select at least one output format"}, Failed: true})
		return
	}

	opts := bookrender.Options{
		Root:     s.root,
		Books:    prefs.Books,
		Profiles: prefs.Profiles,
		Formats:  prefs.Formats,
		Slides:   prefs.Slides,
	}
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
