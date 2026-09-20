package web

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAFreshInstallationGetsAConfigFileWithItsSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), configFileName)
	if _, err := newServer(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no settings file was written: %v", err)
	}
	text := string(b)
	// The schema is in the file, so the file says what belongs in it.
	for _, want := range []string{
		"#Config: {", "#Project: {", "#Preview: {", "#Copyedit: {", "#Prompt: {", "#Connection: {",
		`kind: "anthropic" | "openai"`,
		`mode: *"batched" | "per-task"`,
		"config: #Config & {",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the settings file is missing %q:\n%s", want, text)
		}
	}
	// It holds API keys, so it is the user's own.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestSettingsSurviveARoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), configFileName)
	want := defaultConfig()
	want.Recent = []string{"/books/one", "/books/two"}
	want.Projects["/books/one"] = projectPrefs{
		Topics:    []string{"calltaker"},
		Audiences: map[string][]string{"calltaker": {"pol"}},
		Formats:   []string{"handout"},
		Page:      "chapter2/second.qmd",
	}
	want.Preview.CSS = "dark.css"
	want.Copyedit.Prompts = []copyeditPrompt{{ID: "p1", Title: "Passive voice", Prompt: "Rewrite them actively."}}
	want.Copyedit.Connections = []apiConnection{{
		ID: "c1", Name: "Claude", Kind: kindAnthropic,
		BaseURL: "https://api.anthropic.com/v1", Model: "m", Key: "sk-secret",
	}}
	want.Copyedit.Active = "c1"
	want.Copyedit.Mode = modePerTask

	if err := saveConfigFile(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Recent) != 2 || got.Recent[1] != "/books/two" {
		t.Errorf("recent = %v", got.Recent)
	}
	if p := got.Projects["/books/one"]; p.Page != "chapter2/second.qmd" ||
		len(p.Topics) != 1 || p.Audiences["calltaker"][0] != "pol" || p.Formats[0] != "handout" {
		t.Errorf("project prefs = %+v", p)
	}
	if got.Preview.CSS != "dark.css" {
		t.Errorf("preview css = %q", got.Preview.CSS)
	}
	if c := got.Copyedit.Connections[0]; c.BaseURL != "https://api.anthropic.com/v1" || c.Key != "sk-secret" {
		t.Errorf("connection = %+v", c)
	}
	if got.Copyedit.Mode != modePerTask || got.Copyedit.Active != "c1" {
		t.Errorf("copyedit = %+v", got.Copyedit)
	}
}

// The point of the schema: a mistake in the file is reported, with the
// place it is in, rather than silently ignored the way an unknown JSON
// field would be.
func TestTheSchemaCatchesAMisconfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), configFileName)
	if err := saveConfigFile(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	conn := `copyedit: connections: [{id: "c1", name: "N", kind: %q, baseURL: %q, model: "m"%s}]`
	cases := map[string]struct{ edit, want string }{
		"an API that is neither": {
			`config: ` + fmt.Sprintf(conn, "gemini", "https://x/v1", ""),
			`kind`,
		},
		"a base URL that is not a URL": {
			`config: ` + fmt.Sprintf(conn, "openai", "api.example.com", ""),
			`out of bound =~"^https?://"`,
		},
		"a misspelled field": {
			`config: ` + fmt.Sprintf(conn, "openai", "https://x/v1", `, modle: "m"`),
			`modle: field not allowed`,
		},
		"a setting of the wrong shape": {
			`config: recent: "just the one"`,
			`recent`,
		},
		"a mode that is not one of the two": {
			`config: copyedit: mode: "sideways"`,
			`mode`,
		},
	}
	for name, c := range cases {
		broken := filepath.Join(t.TempDir(), configFileName)
		if err := os.WriteFile(broken, append(append([]byte{}, good...), "\n"+c.edit+"\n"...), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := loadConfigFile(broken)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error does not name %q:\n%v", name, c.want, err)
		}
		if !strings.Contains(err.Error(), configFileName+":") {
			t.Errorf("%s: error names no line in the file:\n%v", name, err)
		}
	}
}

// A file that does not check out stops the app rather than being replaced:
// the settings are the only copy of the user's connections and selections.
func TestAnInvalidFileStopsTheAppAndIsLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), configFileName)
	broken := "#Config: {mode: \"batched\" | \"per-task\"}\nconfig: #Config & {mode: \"sideways\"}\n"
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newServer(path); err == nil {
		t.Fatal("the app started on a settings file that does not check out")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != broken {
		t.Errorf("the file was not left as it was:\n%s", after)
	}
}

// A hand-written file may leave most of it out: lists and maps left out
// are empty, and the scalars carry the defaults the schema names.
func TestAPartialFileIsFilledFromTheSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), configFileName)
	if err := saveConfigFile(path, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	schema := string(b[:strings.Index(string(b), "config: #Config &")])
	partial := schema + "config: #Config & {copyedit: mode: \"per-task\"}\n"
	if err := os.WriteFile(path, []byte(partial), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfigFile(path)
	if err != nil {
		t.Fatalf("a partial file was refused: %v", err)
	}
	if cfg.Copyedit.Mode != modePerTask {
		t.Errorf("mode = %q, want the one that was written", cfg.Copyedit.Mode)
	}
	if cfg.Preview.CSS != defaultCSSName {
		t.Errorf("preview css = %q, want the schema's default", cfg.Preview.CSS)
	}
	if cfg.Recent == nil || len(cfg.Recent) != 0 {
		t.Errorf("recent = %v, want the empty list", cfg.Recent)
	}
}

func TestTheOldJSONFilesAreCarriedOverOnce(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(legacyPrefsFile, `{"/books/one":{"topics":["calltaker"],"formats":["handout"],"page":"index.qmd"}}`)
	write(legacyRecentFile, `["/books/one","/books/two"]`)
	write(legacyCopyedit, `{"prompts":[{"id":"p1","title":"Passive voice","prompt":"Rewrite them."}],
		"connections":[{"id":"c1","name":"Claude","kind":"anthropic","base_url":"https://api.anthropic.com/v1","model":"m","key":"sk-old"}],
		"active":"c1","mode":"per-task"}`)
	write(filepath.Join("custom-css", activeMarkerFile), "dark.css")

	srv, err := newServer(filepath.Join(dir, configFileName))
	if err != nil {
		t.Fatal(err)
	}
	cfg := srv.cfg
	if len(cfg.Recent) != 2 || cfg.Projects["/books/one"].Page != "index.qmd" {
		t.Errorf("render prefs and recents did not come over: %+v", cfg)
	}
	if cfg.Preview.CSS != "dark.css" {
		t.Errorf("the active stylesheet did not come over: %q", cfg.Preview.CSS)
	}
	if c := cfg.Copyedit.Connections; len(c) != 1 || c[0].BaseURL != "https://api.anthropic.com/v1" || c[0].Key != "sk-old" {
		t.Errorf("the connection did not come over (base_url renamed to baseURL): %+v", c)
	}
	if cfg.Copyedit.Mode != modePerTask || len(cfg.Copyedit.Prompts) != 1 {
		t.Errorf("the copyedit setup did not come over: %+v", cfg.Copyedit)
	}

	// The old files are kept, but out of the way, so they are neither
	// read again nor lost.
	for _, name := range []string{legacyPrefsFile, legacyRecentFile, legacyCopyedit} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s is still there as a settings file", name)
		}
		if _, err := os.Stat(filepath.Join(dir, name+".migrated")); err != nil {
			t.Errorf("%s was not kept: %v", name, err)
		}
	}
}
