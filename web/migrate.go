package web

// migrate.go — the settings qm used to keep in several JSON files, read
// once into the one CUE file that replaces them.
//
// Nothing is thrown away: what the old files held is carried over, and the
// files themselves are kept beside the new one under a *.migrated name.
// They are no longer read, and they can be deleted; keeping them costs a
// few bytes and means a mistake here is recoverable.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The files qm wrote before config.cue. render.json is still called that
// in the wild although it grew beyond the render selection, which is why
// its name says so little.
const (
	legacyPrefsFile  = "render.json"
	legacyRecentFile = "recent.json"
	legacyCopyedit   = "copyedit.json"
)

// legacyConnection is an API connection as copyedit.json held it. Only the
// base URL's field name differs from the current one — JSON's snake_case
// against the file's camelCase — but that is enough to need a type of its
// own to read the old file with.
type legacyConnection struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Key     string `json:"key"`
}

type legacyCopyeditConfig struct {
	Prompts     []copyeditPrompt   `json:"prompts"`
	Connections []legacyConnection `json:"connections"`
	Active      string             `json:"active"`
	Mode        string             `json:"mode"`
}

// migrateLegacy builds the settings from whatever the old files in dir
// hold. It reports whether it found any of them, which is what tells a
// migration from a fresh installation.
func migrateLegacy(dir string) (storedConfig, bool) {
	cfg := defaultConfig()
	found := false

	if b, err := os.ReadFile(filepath.Join(dir, legacyPrefsFile)); err == nil {
		projects := map[string]projectPrefs{}
		if json.Unmarshal(b, &projects) == nil {
			cfg.Projects, found = projects, true
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, legacyRecentFile)); err == nil {
		var recent []string
		if json.Unmarshal(b, &recent) == nil {
			cfg.Recent, found = recent, true
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, legacyCopyedit)); err == nil {
		var old legacyCopyeditConfig
		if json.Unmarshal(b, &old) == nil {
			cfg.Copyedit.Prompts = old.Prompts
			cfg.Copyedit.Active = old.Active
			cfg.Copyedit.Mode = old.Mode
			cfg.Copyedit.Connections = nil
			for _, c := range old.Connections {
				cfg.Copyedit.Connections = append(cfg.Copyedit.Connections, apiConnection{
					ID: c.ID, Name: c.Name, Kind: c.Kind,
					BaseURL: c.BaseURL, Model: c.Model, Key: c.Key,
				})
			}
			found = true
		}
	}
	// Which stylesheet is active was a marker file inside the css
	// directory. It is a setting, so it moves into the settings.
	if b, err := os.ReadFile(filepath.Join(dir, "custom-css", activeMarkerFile)); err == nil {
		if name := strings.TrimSpace(string(b)); name != "" {
			cfg.Preview.CSS, found = name, true
		}
	}
	cfg.normalize()
	return cfg, found
}

// retireLegacy renames the files that have been read into the new one, so
// that they are neither read again nor lost. A failure is not worth
// reporting: the settings are already safe in the new file, and the worst
// case is an old file left lying about.
func retireLegacy(dir string) {
	for _, name := range []string{
		filepath.Join(dir, legacyPrefsFile),
		filepath.Join(dir, legacyRecentFile),
		filepath.Join(dir, legacyCopyedit),
		filepath.Join(dir, "custom-css", activeMarkerFile),
	} {
		if _, err := os.Stat(name); err == nil {
			os.Rename(name, name+".migrated")
		}
	}
}
