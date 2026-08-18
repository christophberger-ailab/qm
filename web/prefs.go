package web

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/cboct/qm/internal/bookrender"
)

// projectPrefs is what the UI remembers about a project: the render
// selection and the page last opened in the editor.
type projectPrefs struct {
	// Books are the selected book folder names.
	Books []string `json:"books"`
	// Profiles maps a book folder name to its selected Quarto profiles.
	Profiles map[string][]string `json:"profiles,omitempty"`
	// Formats are the selected output formats (pdf, docx).
	Formats []string `json:"formats"`
	// Slides selects the deck built from the pages' ::: slide blocks.
	Slides bool `json:"slides"`
	// Page is the project-relative path of the page last opened in the
	// editor.
	Page string `json:"page,omitempty"`
}

// renderFormats are the book output formats the UI offers.
var renderFormats = []string{"pdf", "docx"}

// defaultPrefs is what a project gets before the user picks anything: no
// book selected, so the Render button never kicks off a long run by
// accident, but the usual pair of formats ready to go.
func defaultPrefs() projectPrefs {
	return projectPrefs{Formats: slices.Clone(renderFormats)}
}

// profilesFor returns the profiles selected for a book, defaulting — for a
// book the user never configured — to the profiles named after it:
// `dispatcher` and `dispatcher-fw` both belong to the `dispatcher` folder.
func (p projectPrefs) profilesFor(book string, available []string) []string {
	matching := bookrender.DefaultProfiles(book, available)
	if sel, ok := p.Profiles[book]; ok {
		return intersect(sel, matching)
	}
	return matching
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

// defaultPrefsFile returns the per-project preferences file in the user's
// config directory, or "" if no config directory is available. The file is
// still called render.json: it began as the render selection alone, and
// renaming it would drop every selection users have already made.
func defaultPrefsFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "qm", "render.json")
}

// cssDirForPrefs returns the directory that holds the custom preview
// stylesheets, beside the render prefs, or "" when persistence is
// disabled.
func cssDirForPrefs(prefsFile string) string {
	if prefsFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(prefsFile), "custom-css")
}

// prefsFor returns the saved selection of the open project, or the default
// one. The caller must hold s.mu.
func (s *server) prefsFor(root string) projectPrefs {
	if p, ok := s.prefs[root]; ok {
		return p
	}
	return defaultPrefs()
}

// savePrefs writes the per-project preferences to the prefs file.
// Persistence is best effort: the in-memory state is already updated, so a
// write failure only loses the selection across restarts. The caller must
// hold s.mu.
func (s *server) savePrefs() {
	if s.prefsFile == "" {
		return
	}
	b, err := json.MarshalIndent(s.prefs, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.prefsFile), 0o755); err != nil {
		return
	}
	os.WriteFile(s.prefsFile, b, 0o644)
}

// loadPrefs reads the prefs file. A missing or unreadable file just means
// no saved selections yet.
func (s *server) loadPrefs() {
	s.prefs = map[string]projectPrefs{}
	if s.prefsFile == "" {
		return
	}
	if b, err := os.ReadFile(s.prefsFile); err == nil {
		json.Unmarshal(b, &s.prefs)
	}
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
	s.prefs[s.root] = p
	s.savePrefs()
}

// forgetPage drops the remembered page when it is the one named by rel, so
// a deleted page does not stay on record. The caller must hold s.mu.
func (s *server) forgetPage(rel string) {
	if s.root == "" {
		return
	}
	if p := s.prefsFor(s.root); p.Page == rel {
		p.Page = ""
		s.prefs[s.root] = p
		s.savePrefs()
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

// activeMarkerFile is the file inside the css directory that records which
// stylesheet is currently shown by the live preview.
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
// stylesheet when nothing was selected yet or the selection no longer
// exists. The caller must hold s.mu.
func (s *server) activeCSS() string {
	files := s.cssFiles()
	if s.cssDir != "" {
		if b, err := os.ReadFile(filepath.Join(s.cssDir, activeMarkerFile)); err == nil {
			if name := strings.TrimSpace(string(b)); slices.Contains(files, name) {
				return name
			}
		}
	}
	return files[0]
}

// setActiveCSS remembers name as the stylesheet the live preview shows.
// The caller must hold s.mu.
func (s *server) setActiveCSS(name string) error {
	if s.cssDir == "" {
		return nil
	}
	if !slices.Contains(s.cssFiles(), name) {
		return errors.New("no such stylesheet")
	}
	if err := os.MkdirAll(s.cssDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.cssDir, activeMarkerFile), []byte(name), 0o644)
}
