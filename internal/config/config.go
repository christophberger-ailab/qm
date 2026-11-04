// Package config reads qm.yml, the one file that configures a qm project:
// its output formats, its audiences, and its topics.
//
// A minimal project needs no configuration at all: without qm.yml every
// top-level content folder is a topic, there is one audience, and the
// format is a website. Everything in the file only
// states what differs from those defaults:
//
//	title: Handbuch Leitstelle
//	lang: de
//	formats:
//	  web:     {type: html}
//	  handout: {type: [docx, pdf], toc: true, template: assets/templates/Template.docx, show: [explanation]}
//	audiences:
//	  std: {}
//	  pol: {show: [pol], vars: {suffix: _POL}}
//	topics:
//	  calltaker: {cover: "assets/covers/calltaker{suffix}.png"}
//	  sysadmin:  {folder: systemadministration, audiences: [std]}
//
// A render addresses one point of the topic × format × audience matrix. The
// selected format and audience decide which conditional blocks of the
// content survive (see Selection.Shows).
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the configuration file in the project root.
const FileName = "qm.yml"

// Output types a format can produce.
const (
	TypeHTML = "html"
	TypeDOCX = "docx"
	TypePDF  = "pdf"
)

// Config is the parsed qm.yml.
type Config struct {
	// Root is the project directory, absolute.
	Root string `yaml:"-"`
	// Path is the file the configuration was read from, or "" when the
	// project has none and runs on defaults.
	Path string `yaml:"-"`

	Title string `yaml:"title"`
	Lang  string `yaml:"lang"`
	// Output is the directory every format writes below, project-relative.
	// Default "_output".
	Output string `yaml:"output"`
	// Vars are the variables `{{< var name >}}` and `{name}` placeholders
	// resolve against. Formats, audiences and topics add their own.
	Vars map[string]string `yaml:"vars"`

	Formats   Entries[Format]   `yaml:"formats"`
	Audiences Entries[Audience] `yaml:"audiences"`
	Topics    Entries[Topic]    `yaml:"topics"`
}

// Format is how a render is produced.
type Format struct {
	Name string `yaml:"-"`
	// Type is what the format writes: html, docx, pdf, or a list of them
	// (docx and pdf from the same document).
	Type StringList `yaml:"type"`
	// Show lists the conditional classes this format keeps.
	Show []string `yaml:"show"`
	// TOC adds a table of contents to a document.
	TOC bool `yaml:"toc"`
	// Template is a reference DOCX whose styles, page setup, headers and
	// footers the DOCX output uses.
	Template string `yaml:"template"`
	// Dir is the output directory, below Config.Output. Default: the
	// format's name. May contain placeholders.
	Dir string `yaml:"dir"`
	// File is the file name stem of a document. Default "{topic}", plus
	// "-{audience}" when the project has more than one audience. May
	// contain placeholders.
	File string `yaml:"file"`
	// Topics and Audiences restrict the matrix from the format's side.
	Topics    []string          `yaml:"topics"`
	Audiences []string          `yaml:"audiences"`
	Vars      map[string]string `yaml:"vars"`
}

// Audience is for whom a render is made.
type Audience struct {
	Name string `yaml:"-"`
	// Title is the label the web UI shows. Default: the name.
	Title string `yaml:"title"`
	// Show lists the conditional classes this audience keeps. The
	// audience's own name is always one of them.
	Show []string          `yaml:"show"`
	Vars map[string]string `yaml:"vars"`
}

// Topic is what a render contains: one content folder.
type Topic struct {
	Name string `yaml:"-"`
	// Folder is the content folder, project-relative. Default: the name.
	Folder string `yaml:"folder"`
	// Title overrides the title of the folder's index page.
	Title string `yaml:"title"`
	// Cover is a PNG put in front of a document. May contain placeholders.
	Cover string `yaml:"cover"`
	// Formats and Audiences restrict the matrix from the topic's side.
	Formats   []string          `yaml:"formats"`
	Audiences []string          `yaml:"audiences"`
	Vars      map[string]string `yaml:"vars"`
}

// Has reports whether the format writes type t.
func (f Format) Has(t string) bool { return slices.Contains(f.Type, t) }

// IsWeb reports whether the format is a website.
func (f Format) IsWeb() bool { return f.Has(TypeHTML) }

// Load reads root/qm.yml. A project without one gets the defaults; any
// other problem — a parse error, an unknown key, a type qm cannot write —
// is an error.
func Load(root string) (*Config, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	c := &Config{Root: abs}
	path := filepath.Join(abs, FileName)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// Defaults only — unless the project is still configured for
		// Quarto, whose files qm no longer reads. Rendering it on the
		// defaults would quietly produce something else than it used to.
		if old := legacyConfig(abs); len(old) > 0 {
			return nil, fmt.Errorf("%s has no %s but still has the old Quarto configuration (%s), "+
				"which qm no longer reads; run `qm migrate` to convert it", abs, FileName, strings.Join(old, ", "))
		}
	case err != nil:
		return nil, err
	default:
		c.Path = path
		dec := yaml.NewDecoder(strings.NewReader(string(data)))
		dec.KnownFields(true)
		if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if err := c.complete(); err != nil {
		if c.Path != "" {
			return nil, fmt.Errorf("%s: %w", c.Path, err)
		}
		return nil, err
	}
	return c, nil
}

// complete fills in the defaults and validates the result.
func (c *Config) complete() error {
	if c.Output == "" {
		c.Output = "_output"
	}
	if c.Vars == nil {
		c.Vars = map[string]string{}
	}
	if len(c.Formats) == 0 {
		c.Formats = Entries[Format]{
			{Name: "web", Value: Format{Type: StringList{TypeHTML}}},
		}
	}
	for i := range c.Formats {
		f := &c.Formats[i].Value
		f.Name = c.Formats[i].Name
		if len(f.Type) == 0 {
			f.Type = StringList{guessType(f.Name)}
		}
		for _, t := range f.Type {
			switch t {
			case TypeHTML, TypeDOCX, TypePDF:
			default:
				return fmt.Errorf("format %s: unknown type %q (want html, docx or pdf)", f.Name, t)
			}
		}
		if f.Has(TypeHTML) && len(f.Type) > 1 {
			return fmt.Errorf("format %s: html cannot be combined with another type", f.Name)
		}
		if f.Dir == "" {
			f.Dir = f.Name
		}
	}
	if len(c.Audiences) == 0 {
		c.Audiences = Entries[Audience]{{Name: "all", Value: Audience{}}}
	}
	for i := range c.Audiences {
		a := &c.Audiences[i].Value
		a.Name = c.Audiences[i].Name
		if a.Title == "" {
			a.Title = a.Name
		}
	}
	if len(c.Topics) == 0 {
		folders, err := ContentFolders(c.Root)
		if err != nil {
			return err
		}
		for _, f := range folders {
			c.Topics = append(c.Topics, Entry[Topic]{Name: f})
		}
	}
	for i := range c.Topics {
		t := &c.Topics[i].Value
		t.Name = c.Topics[i].Name
		if t.Folder == "" {
			t.Folder = t.Name
		}
		t.Folder = filepath.ToSlash(filepath.Clean(t.Folder))
	}
	// Cross references must name something that exists.
	check := func(owner, axis string, names []string, known func(string) bool) error {
		for _, n := range names {
			if !known(n) {
				return fmt.Errorf("%s names unknown %s %q", owner, axis, n)
			}
		}
		return nil
	}
	isFormat := func(n string) bool { _, ok := c.Formats.Get(n); return ok }
	isAudience := func(n string) bool { _, ok := c.Audiences.Get(n); return ok }
	isTopic := func(n string) bool { _, ok := c.Topics.Get(n); return ok }
	for _, f := range c.Formats {
		if err := check("format "+f.Name, "topic", f.Value.Topics, isTopic); err != nil {
			return err
		}
		if err := check("format "+f.Name, "audience", f.Value.Audiences, isAudience); err != nil {
			return err
		}
	}
	for _, t := range c.Topics {
		if err := check("topic "+t.Name, "format", t.Value.Formats, isFormat); err != nil {
			return err
		}
		if err := check("topic "+t.Name, "audience", t.Value.Audiences, isAudience); err != nil {
			return err
		}
	}
	return nil
}

func guessType(name string) string {
	switch strings.ToLower(name) {
	case "docx", "word", "handout", "handbook":
		return TypeDOCX
	case "pdf":
		return TypePDF
	}
	return TypeHTML
}

// ContentFolders lists the directories directly below root that hold
// content (a .qmd or .md file somewhere below them), in name order. Names
// starting with "_" or "." are invisible.
func ContentFolders(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || strings.HasPrefix(n, "_") || strings.HasPrefix(n, ".") {
			continue
		}
		if hasContent(filepath.Join(root, n)) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

func hasContent(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		n := d.Name()
		if p != dir && (strings.HasPrefix(n, "_") || strings.HasPrefix(n, ".")) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && IsContentFile(n) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// IsContentFile reports whether a file name is a content page.
func IsContentFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".qmd" || ext == ".md"
}

// Selection is one point of the matrix: what a single render produces.
type Selection struct {
	Config   *Config
	Topic    Topic
	Format   Format
	Audience Audience
	// Topics is the set of topics a website render covers. It is nil for a
	// document render, which covers Topic alone.
	Topics []Topic
}

// Vars returns the variables of the selection: the project's, then the
// topic's, the audience's and the format's (later ones win), and the
// built-ins topic, format and audience.
func (s Selection) Vars() map[string]string {
	out := map[string]string{}
	for _, m := range []map[string]string{s.Config.Vars, s.Topic.Vars, s.Audience.Vars, s.Format.Vars} {
		for k, v := range m {
			out[k] = v
		}
	}
	out["topic"] = s.Topic.Name
	out["format"] = s.Format.Name
	out["audience"] = s.Audience.Name
	if s.Config.Title != "" {
		if _, ok := out["title"]; !ok {
			out["title"] = s.Config.Title
		}
	}
	return out
}

// Keep decides whether content carrying the given classes survives this
// selection.
//
// A class is conditional when a format's show list names it (a format
// class, such as explanation), or when an audience's show list names it
// or it is an audience's name (an audience class, such as pol). Content
// with format classes is kept when the selected format shows one of them;
// content with audience classes is kept when the selected audience shows
// one of them. Both conditions apply to content that carries both kinds.
// Classes that are not conditional do not matter.
func (s Selection) Keep(classes []string) bool {
	var fmtCond, fmtShown, audCond, audShown bool
	for _, c := range classes {
		if s.Config.formatClass(c) {
			fmtCond = true
			fmtShown = fmtShown || slices.Contains(s.Format.Show, c)
		}
		if s.Config.audienceClass(c) {
			audCond = true
			audShown = audShown || c == s.Audience.Name || slices.Contains(s.Audience.Show, c)
		}
	}
	return (!fmtCond || fmtShown) && (!audCond || audShown)
}

func (c *Config) formatClass(class string) bool {
	for _, f := range c.Formats {
		if slices.Contains(f.Value.Show, class) {
			return true
		}
	}
	return false
}

func (c *Config) audienceClass(class string) bool {
	for _, a := range c.Audiences {
		if a.Name == class || slices.Contains(a.Value.Show, class) {
			return true
		}
	}
	return false
}

// Types lists what Quarto's `when-format` conditions compare against for
// this selection: the format's output types and its name.
func (s Selection) Types() []string {
	return append(append([]string(nil), s.Format.Type...), s.Format.Name)
}

// OutputDir is the absolute directory the selection writes into.
func (s Selection) OutputDir() (string, error) {
	dir, err := Interpolate(s.Format.Dir, s.Vars())
	if err != nil {
		return "", fmt.Errorf("format %s: dir: %w", s.Format.Name, err)
	}
	if s.Format.IsWeb() && len(s.Config.Audiences) > 1 && !strings.Contains(s.Format.Dir, "{") {
		// One site per audience: keep them apart unless the format says
		// where each goes.
		dir += "-" + s.Audience.Name
	}
	return filepath.Join(s.Config.Root, filepath.FromSlash(s.Config.Output), filepath.FromSlash(dir)), nil
}

// FileStem is the document's file name without extension.
func (s Selection) FileStem() (string, error) {
	pattern := s.Format.File
	if pattern == "" {
		pattern = "{topic}"
		if len(s.Config.Audiences) > 1 {
			pattern += "-{audience}"
		}
	}
	stem, err := Interpolate(pattern, s.Vars())
	if err != nil {
		return "", fmt.Errorf("format %s: file: %w", s.Format.Name, err)
	}
	return stem, nil
}

// Cover is the absolute path of the topic's cover image, or "".
func (s Selection) Cover() (string, error) {
	if s.Topic.Cover == "" {
		return "", nil
	}
	p, err := Interpolate(s.Topic.Cover, s.Vars())
	if err != nil {
		return "", fmt.Errorf("topic %s: cover: %w", s.Topic.Name, err)
	}
	return filepath.Join(s.Config.Root, filepath.FromSlash(p)), nil
}

// Template is the absolute path of the format's reference DOCX, or "".
func (s Selection) Template() (string, error) {
	if s.Format.Template == "" {
		return "", nil
	}
	p, err := Interpolate(s.Format.Template, s.Vars())
	if err != nil {
		return "", fmt.Errorf("format %s: template: %w", s.Format.Name, err)
	}
	return filepath.Join(s.Config.Root, filepath.FromSlash(p)), nil
}

// String names the selection for logs.
func (s Selection) String() string {
	if s.Topics != nil {
		return fmt.Sprintf("%s · %s (site)", s.Format.Name, s.Audience.Name)
	}
	return fmt.Sprintf("%s · %s · %s", s.Topic.Name, s.Format.Name, s.Audience.Name)
}

var placeholder = regexp.MustCompile(`\{([A-Za-z][\w-]*)\}`)

// Interpolate replaces {name} placeholders by their variables. A
// placeholder without a variable is an error.
func Interpolate(s string, vars map[string]string) (string, error) {
	var missing []string
	out := placeholder.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		v, ok := vars[name]
		if !ok {
			missing = append(missing, name)
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("undefined variable %s in %q", strings.Join(missing, ", "), s)
	}
	return out, nil
}

// legacyConfig lists the project's old Quarto configuration files:
// `_quarto.yml` and the `_quarto-<axis>-<value>.yml` profiles.
func legacyConfig(root string) []string {
	var out []string
	for _, pattern := range []string{"_quarto.yml", "_quarto.yaml", "_quarto-*.yml", "_quarto-*.yaml"} {
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, m := range matches {
			out = append(out, filepath.Base(m))
		}
	}
	return out
}
