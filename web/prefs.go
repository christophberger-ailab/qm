package web

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/cboct/qm/internal/bookrender"
)

// renderPrefs is a project's remembered render selection.
type renderPrefs struct {
	// Books are the selected book folder names.
	Books []string `json:"books"`
	// Profiles maps a book folder name to its selected Quarto profiles.
	Profiles map[string][]string `json:"profiles,omitempty"`
	// Formats are the selected output formats (pdf, docx).
	Formats []string `json:"formats"`
	// Slides selects the deck built from the pages' ::: slide blocks.
	Slides bool `json:"slides"`
}

// renderFormats are the book output formats the UI offers.
var renderFormats = []string{"pdf", "docx"}

// defaultPrefs is what a project gets before the user picks anything: no
// book selected, so the Render button never kicks off a long run by
// accident, but the usual pair of formats ready to go.
func defaultPrefs() renderPrefs {
	return renderPrefs{Formats: slices.Clone(renderFormats)}
}

// profilesFor returns the profiles selected for a book, defaulting — for a
// book the user never configured — to the profiles named after it:
// `dispatcher` and `dispatcher-fw` both belong to the `dispatcher` folder.
func (p renderPrefs) profilesFor(book string, available []string) []string {
	if sel, ok := p.Profiles[book]; ok {
		return intersect(sel, available)
	}
	return bookrender.DefaultProfiles(book, available)
}

// intersect keeps the entries of sel that still exist in available, so a
// saved selection naming a profile or book that has since been deleted
// neither shows up nor breaks the restore.
func intersect(sel, available []string) []string {
	out := make([]string, 0, len(sel))
	for _, s := range sel {
		if slices.Contains(available, s) {
			out = append(out, s)
		}
	}
	return out
}

// defaultPrefsFile returns the render selections file in the user's config
// directory, or "" if no config directory is available.
func defaultPrefsFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "qm", "render.json")
}

// previewCSSFileForPrefs returns the custom preview stylesheet path that
// lives beside the render prefs, or "" when persistence is disabled.
func previewCSSFileForPrefs(prefsFile string) string {
	if prefsFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(prefsFile), "preview.css")
}

// prefsFor returns the saved selection of the open project, or the default
// one. The caller must hold s.mu.
func (s *server) prefsFor(root string) renderPrefs {
	if p, ok := s.prefs[root]; ok {
		return p
	}
	return defaultPrefs()
}

// savePrefs writes the per-project render selections to the prefs file.
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
	s.prefs = map[string]renderPrefs{}
	if s.prefsFile == "" {
		return
	}
	if b, err := os.ReadFile(s.prefsFile); err == nil {
		json.Unmarshal(b, &s.prefs)
	}
}

// loadPreviewCSS reads the user stylesheet. Missing or unreadable files
// behave like an empty stylesheet because the preview must still load.
// The caller must hold s.mu.
func (s *server) loadPreviewCSS() string {
	if s.previewCSSFile == "" {
		return ""
	}
	b, err := os.ReadFile(s.previewCSSFile)
	if err != nil {
		return ""
	}
	return string(b)
}

// savePreviewCSS writes the user stylesheet. The caller reports failures in
// the UI; the in-memory request has already been handled, so the server
// keeps running even when persistence fails. The caller must hold s.mu.
func (s *server) savePreviewCSS(css string) error {
	if s.previewCSSFile == "" {
		return errors.New("no config directory available")
	}
	if err := os.MkdirAll(filepath.Dir(s.previewCSSFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.previewCSSFile, []byte(css), 0o644)
}
