package web

// settings.go — the one file the app keeps its settings in.
//
// (config_test.go beside it tests the /config *pages*; this is the file
// those pages, and everything else the UI remembers, are stored in.)
//
// Everything the UI remembers between runs lives in `config.cue` in the
// user's config directory: the projects opened before, what is selected
// per project, which preview stylesheet is active, and the copyediting
// setup. Only the stylesheets themselves stay outside it, being CSS
// rather than settings.
//
// The file is CUE rather than JSON for one reason: it carries its own
// schema. The definitions are written into the file above the settings,
// so the file says what belongs in it and in what shape, and every read
// checks the settings against them. A typo'd field name, a `kind` that is
// none of the three, a base URL that is not a URL — each is reported with
// the line it is on instead of being silently dropped, the way an unknown
// JSON field would be.
//
// qm rewrites the file whenever something changes in the UI, and it
// rewrites the schema with it, so the schema in the file is always the one
// the running binary checks against. Hand-edited *values* are read back;
// hand-written comments in the settings are not kept.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
)

// storedConfig is the whole of what qm remembers. The field names are the
// field names in the file: the JSON tags are what CUE encodes and decodes
// by, so the Go struct and #Config below are two spellings of one thing
// and have to be changed together.
type storedConfig struct {
	// Recent are the projects the Open field offers, most recent first.
	Recent []string `json:"recent"`
	// Projects is what the UI remembers per project, keyed by its path.
	Projects map[string]projectPrefs `json:"projects"`
	Preview  previewConfig           `json:"preview"`
	Copyedit copyeditConfig          `json:"copyedit"`
	Editor   editorConfig            `json:"editor"`
}

// previewConfig is the Markdown preview's own settings. The stylesheets
// live in custom-css/ as the CSS files they are; which of them the preview
// uses is a setting and lives here.
type previewConfig struct {
	CSS string `json:"css"`
}

// configSchema is the schema written into every config file, and the one
// every read checks against. It is a Go constant so that the file on disk
// and the binary reading it can never drift apart: a version of qm that
// knows a new setting writes the definition for it the next time it saves.
const configSchema = `// ---------------------------------------------------------------- schema
//
// Every setting below is checked against these definitions when qm reads
// the file. The definitions are closed: a field that is not named here is
// an error rather than an unnoticed typo.
//
// A field you leave out is not an error: a list or a map left out is
// simply empty, and the scalars carry the default written after "*".

#Config: {
	// The projects the Open field offers, most recent first.
	recent: [...string]

	// What the UI remembers per project, keyed by the project's path.
	projects: {[string]: #Project}

	preview:  #Preview
	copyedit: #Copyedit
	editor:   #Editor
}

#Project: {
	// The render selection: topics, the audiences chosen per topic, and
	// the output formats.
	topics: [...string]
	audiences: {[string]: [...string]}
	formats: [...string]

	// The page the editor last had open, relative to the project root.
	page: string | *""
}

#Preview: {
	// The stylesheet the live preview is shown in: a file name in the
	// custom-css directory beside this file.
	css: string | *"custom.css"
}

#Editor: {
	// The command line the top bar's Editor button runs. {root} is the
	// project's directory, {file} the page open in the app; an argument
	// naming {file} (with an option right before it) is left out when no
	// page is open. Quotes keep an argument with spaces together.
	command: string | *"code {root} --goto {file}"
}

#Copyedit: {
	// The editing tasks the copyedit tab offers.
	prompts: [...#Prompt]

	// The models those tasks can be run on.
	connections: [...#Connection]

	// The connection a run goes to: the id of one of the above. Empty
	// means the first one.
	active: string | *""

	// Whether a run of several tasks is one call carrying all of them,
	// or one call per task.
	mode: *"batched" | "per-task"

	// The tasks ticked in the pane, by id. Kept so that a selection
	// outlives a page switch and a restart; an id naming no task is
	// ignored.
	selected: [...string]
}

#Prompt: {
	// Set by qm when the task is added; leave it alone unless you are
	// writing a task by hand, in which case any distinct text will do.
	id: string & !=""

	// What the copyedit tab lists.
	title: string & !=""

	// What the model is told to do with the page.
	prompt: string & !=""
}

#Connection: {
	id:   string & !=""
	name: string & !=""

	// How the model is reached. "anthropic" posts to <baseURL>/messages
	// and "openai" to <baseURL>/chat/completions; "copilot" is not an
	// endpoint at all but the GitHub Copilot CLI, run as a child
	// process, which must be installed and signed in.
	kind: "anthropic" | "openai" | "copilot"

	// The base URL, or the whole endpoint: qm appends the path above
	// only when it is not already there. Empty on the "copilot" kind,
	// which is reached through the CLI rather than at an address.
	baseURL: (string & =~"^https?://") | *""

	// The model to ask for, as the provider names it.
	model: string & !=""

	// The API key. A local model usually needs none, and neither does
	// the "copilot" kind, which authenticates as whoever the CLI is
	// signed in as; a GitHub token here is used in that user's place.
	key: string | *""
}
`

// configHeader tops the file: what it is, who writes it, and what a hand
// edit can and cannot expect.
const configHeader = `// qm settings.
//
// qm reads this file when it starts and rewrites it whenever a setting
// changes in the web UI. The schema below says what belongs in it, and
// every read is checked against it, so a mistake is reported with the line
// it is on rather than quietly ignored.
//
// Editing the values by hand is fine -- they are read back as they stand.
// Comments you add below are not: a rewrite replaces everything under
// "settings" with what the UI holds. The schema itself is written by qm.
//
// The Markdown preview's stylesheets are not in here; they are CSS, and
// they live in the custom-css directory beside this file.
`

// configSettingsHeader introduces the data half of the file.
const configSettingsHeader = `// -------------------------------------------------------------- settings

config: #Config & `

// configFileName is the settings file inside the config directory.
const configFileName = "config.cue"

// defaultConfigFile returns the settings file in the user's config
// directory, or "" when there is no config directory to put it in — in
// which case the app runs with settings that last as long as it does.
func defaultConfigFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "qm", configFileName)
}

// defaultConfig is what a fresh installation starts with: nothing opened,
// nothing selected, and the copyediting tool waiting to be configured.
func defaultConfig() storedConfig {
	return storedConfig{
		Recent:   []string{},
		Projects: map[string]projectPrefs{},
		Preview:  previewConfig{CSS: defaultCSSName},
		Copyedit: copyeditConfig{
			Prompts:     []copyeditPrompt{},
			Connections: []apiConnection{},
			Mode:        modeBatched,
			Selected:    []string{},
		},
		Editor: editorConfig{Command: defaultEditorCommand},
	}
}

// normalize fills in what the file may leave out, so that the settings are
// written whole and the Go side never has to reason about nil maps.
func (c *storedConfig) normalize() {
	if c.Recent == nil {
		c.Recent = []string{}
	}
	if c.Projects == nil {
		c.Projects = map[string]projectPrefs{}
	}
	if c.Preview.CSS == "" {
		c.Preview.CSS = defaultCSSName
	}
	if c.Copyedit.Prompts == nil {
		c.Copyedit.Prompts = []copyeditPrompt{}
	}
	if c.Copyedit.Connections == nil {
		c.Copyedit.Connections = []apiConnection{}
	}
	if c.Copyedit.Mode != modePerTask {
		c.Copyedit.Mode = modeBatched
	}
	if c.Editor.Command == "" {
		c.Editor.Command = defaultEditorCommand
	}
	if c.Copyedit.Selected == nil {
		c.Copyedit.Selected = []string{}
	}
	for name, p := range c.Projects {
		if p.Topics == nil {
			p.Topics = []string{}
		}
		if p.Audiences == nil {
			p.Audiences = map[string][]string{}
		}
		if p.Formats == nil {
			p.Formats = []string{}
		}
		c.Projects[name] = p
	}
}

// loadConfigFile reads and checks the settings file. The error it returns
// is the user's to act on — it names the line — so it is passed through to
// them rather than swallowed: a settings file that does not say what it
// means is not something to guess at, least of all by overwriting it.
func loadConfigFile(path string) (storedConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return storedConfig{}, err
	}
	return decodeConfig(b, path)
}

// decodeConfig compiles the file, checks the settings against the schema
// the file itself carries, and reads them out.
func decodeConfig(b []byte, path string) (storedConfig, error) {
	ctx := cuecontext.New()
	v := ctx.CompileBytes(b, cue.Filename(path))
	if err := v.Err(); err != nil {
		return storedConfig{}, configError(path, err)
	}
	schema := v.LookupPath(cue.ParsePath("#Config"))
	if !schema.Exists() {
		return storedConfig{}, fmt.Errorf("%s: no #Config schema in the file; delete the file to have qm write a fresh one", path)
	}
	data := v.LookupPath(cue.ParsePath("config"))
	if !data.Exists() {
		return storedConfig{}, fmt.Errorf("%s: no `config:` settings in the file; delete the file to have qm write a fresh one", path)
	}
	checked := schema.Unify(data)
	if err := checked.Validate(cue.Concrete(true)); err != nil {
		return storedConfig{}, configError(path, err)
	}
	var cfg storedConfig
	if err := checked.Decode(&cfg); err != nil {
		return storedConfig{}, configError(path, err)
	}
	cfg.normalize()
	return cfg, nil
}

// configError turns a CUE error into one worth reading: cue's Details
// carries the file, line, and column of every conflict it found.
func configError(path string, err error) error {
	return fmt.Errorf("%s is not valid:\n%s", path, cueerrors.Details(err, nil))
}

// renderConfig writes the settings out as the whole file: the header, the
// schema, and the settings under it.
func renderConfig(c storedConfig) ([]byte, error) {
	c.normalize()
	ctx := cuecontext.New()
	v := ctx.Encode(c)
	if err := v.Err(); err != nil {
		return nil, err
	}
	body, err := format.Node(v.Syntax(cue.Final(), cue.Concrete(true)), format.Simplify())
	if err != nil {
		return nil, err
	}
	out := append([]byte(configHeader), '\n')
	out = append(out, configSchema...)
	out = append(out, '\n')
	out = append(out, configSettingsHeader...)
	out = append(out, body...)
	out = append(out, '\n')
	return out, nil
}

// saveConfigFile writes the settings. The file holds API keys, so it is
// written for its owner alone.
//
// What is written is read back before it replaces the file: the settings
// are the only copy of the user's connections and selections, and a write
// that the next start cannot read would lose them. It is our own schema on
// both sides, so this should never fail — which is exactly why it is worth
// finding out here rather than at the next start.
func saveConfigFile(path string, c storedConfig) error {
	if path == "" {
		return errors.New("no config directory available")
	}
	b, err := renderConfig(c)
	if err != nil {
		return err
	}
	if _, err := decodeConfig(b, path); err != nil {
		return fmt.Errorf("refusing to write settings that cannot be read back: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// openConfig is what the server starts from: the settings file if there is
// one, the settings the older JSON files held if there is not, and the
// defaults if there is neither. A file that exists but does not check out
// is an error and stays untouched.
func openConfig(path string) (storedConfig, error) {
	if path == "" {
		return defaultConfig(), nil // no config directory: this run only
	}
	cfg, err := loadConfigFile(path)
	switch {
	case err == nil:
		return cfg, nil
	case !os.IsNotExist(err):
		return storedConfig{}, err
	}
	cfg, found := migrateLegacy(filepath.Dir(path))
	if err := saveConfigFile(path, cfg); err != nil {
		return cfg, err
	}
	if found {
		retireLegacy(filepath.Dir(path))
		fmt.Fprintf(os.Stderr, "qm: settings moved into %s; the files they came from are kept beside it as *.migrated\n", path)
	}
	return cfg, nil
}
