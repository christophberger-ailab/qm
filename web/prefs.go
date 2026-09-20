package web

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// projectPrefs is what the UI remembers about a project: the render
// selection and the page last opened in the editor.
//
// The render selection is a point in the project's three-axis matrix (see
// internal/qmcore): topics, formats, and — per topic, because not every
// topic has the same ones — audiences.
type projectPrefs struct {
	// Topics are the selected topic names.
	Topics []string `json:"topics"`
	// Audiences maps a topic to the audiences selected for it.
	Audiences map[string][]string `json:"audiences"`
	// Formats are the selected format names (handout, handbook, ...).
	Formats []string `json:"formats"`
	// Page is the project-relative path of the page last opened in the
	// editor.
	Page string `json:"page"`
}

// defaultPrefs is what a project gets before the user picks anything:
// nothing selected, so the Render button never kicks off a long run by
// accident.
func defaultPrefs() projectPrefs { return projectPrefs{} }

// audiencesFor returns the audiences selected for a topic, defaulting — for
// a topic the user never configured — to every audience the topic takes
// part in.
func (p projectPrefs) audiencesFor(topic string, available []string) []string {
	if sel, ok := p.Audiences[topic]; ok {
		return intersect(sel, available)
	}
	return available
}

// intersect keeps the entries of sel that still exist in available, so a
// saved selection naming a deleted or no longer matching profile neither
// shows up nor breaks the restore.
func intersect(sel, available []string) []string {
	out := make([]string, 0, len(sel))
	for _, s := range sel {
		if slices.Contains(available, s) {
			out = append(out, s)
		}
	}
	return out
}

// maxRecent is how many project paths the Open field's dropdown offers.
// Ten is what fits in a glance; older paths drop off the end.
const maxRecent = 10

// rememberRoot puts dir at the head of the recently opened projects, so
// the Open dropdown offers the most recent one first and never lists the
// same project twice. The caller must hold s.mu.
func (s *server) rememberRoot(dir string) {
	if dir == "" {
		return
	}
	s.cfg.Recent = append([]string{dir}, slices.DeleteFunc(s.cfg.Recent, func(p string) bool {
		return p == dir
	})...)
	if len(s.cfg.Recent) > maxRecent {
		s.cfg.Recent = s.cfg.Recent[:maxRecent]
	}
	s.saveConfig()
}

// saveConfig writes the settings file. Persistence is best effort for
// everything that goes through here: the in-memory settings are already
// updated, so a failed write only loses the change across restarts, and
// the UI has nowhere useful to report it from. The handlers that let the
// user type something worth keeping -- the editing tasks, the API
// connections -- call the store directly and do report what went wrong.
// The caller must hold s.mu.
func (s *server) saveConfig() error {
	if s.configFile == "" {
		return nil // no config directory: these settings last this run
	}
	return saveConfigFile(s.configFile, s.cfg)
}

// cssDirFor returns the directory that holds the custom preview
// stylesheets, beside the settings file, or "" when persistence is
// disabled. The stylesheets are the one thing that stays out of the
// settings file: they are CSS, and CSS belongs in .css files where an
// editor can highlight it.
func cssDirFor(configFile string) string {
	if configFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configFile), "custom-css")
}

// prefsFor returns the saved selection of the open project, or the default
// one. The caller must hold s.mu.
func (s *server) prefsFor(root string) projectPrefs {
	if p, ok := s.cfg.Projects[root]; ok {
		return p
	}
	return defaultPrefs()
}

// rememberPage records the page the editor now shows, so the app comes back
// to it: after a restart, and after the trip to the config pages and back,
// which leaves and reloads the app page. The caller must hold s.mu.
func (s *server) rememberPage(rel string) {
	if s.root == "" {
		return
	}
	p := s.prefsFor(s.root)
	if p.Page == rel {
		return
	}
	p.Page = rel
	s.cfg.Projects[s.root] = p
	s.saveConfig()
}

// forgetPage drops the remembered page when it is the one named by rel, so
// a deleted page does not stay on record. The caller must hold s.mu.
func (s *server) forgetPage(rel string) {
	if s.root == "" {
		return
	}
	if p := s.prefsFor(s.root); p.Page == rel {
		p.Page = ""
		s.cfg.Projects[s.root] = p
		s.saveConfig()
	}
}

// defaultCSSName is the custom stylesheet every project starts with: the
// one baked into the binary and materialized on disk the first time the
// app runs.
const defaultCSSName = "custom.css"

// defaultCustomCSS is the stylesheet baked into the app as the default
// content of custom.css. It ships the quarto (::: slide, ::: pol, ...)
// preview styling that used to be a hand-written custom.css; a fresh
// install now looks the same without any setup.
const defaultCustomCSS = `.quarto.slide {
    background: linear-gradient(135deg, #fefefe 0%, #f0f0f3 50%, #fefefe 100%);
}
.quarto.slide:before {
    content: "🖥️ SLIDE"
}
.quarto.pol {
    background: lightblue;
}
.quarto.pol:before {
    content: "🚔"
}
.quarto.fw {
    background: lightpink;
}
.quarto.fw:before {
    content: "🚒"
}
.quarto.perle {
    background: lightgreen;
}
.quarto.perle:before {
    content: "[⛲PERLE]"
}
.quarto.tutorial {
    background: blanchedalmond;
}
.quarto.tutorial:before {
    content: "🎓 TUTORIAL"
}
.quarto.howto {
    background: ghostwhite;
}
.quarto.howto:before {
    content: "🔨 HOWTO"
}
.quarto.reference {
    background: lemonchiffon;
}
.quarto.reference:before {
    content: "📃 REFERENCE"
}
`

// cssNamePattern restricts custom stylesheet file names to a safe, plain
// subset: no path separators or leading dots, so a name can never escape
// the css directory or collide with the ".active" marker file.
var cssNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*\.css$`)

// sanitizeCSSName normalizes a user-supplied stylesheet name: it trims
// space, adds the .css suffix if missing, and rejects anything that is not
// a plain file name.
func sanitizeCSSName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("stylesheet name is empty")
	}
	if !strings.HasSuffix(name, ".css") {
		name += ".css"
	}
	if !cssNamePattern.MatchString(name) {
		return "", errors.New("invalid stylesheet name")
	}
	return name, nil
}

// activeMarkerFile is the file the active stylesheet used to be recorded
// in, inside the css directory. It is a setting and lives in the settings
// file now; the name is kept so that the old marker can be read once and
// retired (see migrate.go).
const activeMarkerFile = ".active"

// ensureDefaultCSS materializes the baked-in default stylesheet the first
// time the app runs with this config directory: it never touches a css
// directory that already exists, so a user who deleted or renamed
// custom.css keeps that choice across restarts.
func ensureDefaultCSS(dir string) {
	if dir == "" {
		return
	}
	if _, err := os.Stat(dir); err == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	os.WriteFile(filepath.Join(dir, defaultCSSName), []byte(defaultCustomCSS), 0o644)
}

// cssFiles lists the custom stylesheets available, sorted by name. With
// persistence disabled, the single baked-in default is the only one.
// The caller must hold s.mu.
func (s *server) cssFiles() []string {
	if s.cssDir == "" {
		return []string{defaultCSSName}
	}
	entries, err := os.ReadDir(s.cssDir)
	if err != nil {
		return []string{defaultCSSName}
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".css") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return []string{defaultCSSName}
	}
	sort.Strings(names)
	return names
}

// loadCSS reads a stylesheet by name. Missing or unreadable files behave
// like an empty stylesheet because the preview must still load; the
// baked-in default is served from memory when persistence is disabled.
// The caller must hold s.mu.
func (s *server) loadCSS(name string) string {
	if s.cssDir == "" {
		if name == defaultCSSName || name == "" {
			return defaultCustomCSS
		}
		return ""
	}
	b, err := os.ReadFile(filepath.Join(s.cssDir, name))
	if err != nil {
		return ""
	}
	return string(b)
}

// saveCSS writes a stylesheet by name. The caller reports failures in the
// UI; the in-memory request has already been handled, so the server keeps
// running even when persistence fails. The caller must hold s.mu.
func (s *server) saveCSS(name, css string) error {
	if s.cssDir == "" {
		return errors.New("no config directory available")
	}
	if err := os.MkdirAll(s.cssDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.cssDir, name), []byte(css), 0o644)
}

// createCSS adds a new, empty alternate stylesheet and returns its final
// (sanitized) file name. The caller must hold s.mu.
func (s *server) createCSS(name string) (string, error) {
	clean, err := sanitizeCSSName(name)
	if err != nil {
		return "", err
	}
	if s.cssDir == "" {
		return "", errors.New("no config directory available")
	}
	if _, err := os.Stat(filepath.Join(s.cssDir, clean)); err == nil {
		return "", errors.New("a stylesheet with that name already exists")
	}
	if err := s.saveCSS(clean, ""); err != nil {
		return "", err
	}
	return clean, nil
}

// activeCSS returns the stylesheet the live preview currently shows: the
// one last selected in the dropdown, falling back to the first available
// stylesheet when nothing was selected yet or the selection names a file
// that is no longer there. The caller must hold s.mu.
func (s *server) activeCSS() string {
	files := s.cssFiles()
	if slices.Contains(files, s.cfg.Preview.CSS) {
		return s.cfg.Preview.CSS
	}
	return files[0]
}

// setActiveCSS remembers name as the stylesheet the live preview shows.
// The caller must hold s.mu.
func (s *server) setActiveCSS(name string) error {
	if !slices.Contains(s.cssFiles(), name) {
		return errors.New("no such stylesheet")
	}
	s.cfg.Preview.CSS = name
	return s.saveConfig()
}
