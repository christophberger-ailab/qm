package qmcore

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// This file reads the parts of a `_quarto-<axis>-<value>.yml` profile that
// qm itself acts on.
//
// Two of them are Quarto's:
//
//	_quarto-vars:   the variables `{{< var x >}}` resolves against. Quarto
//	                resolves them in `book: output-file` and `book: title`,
//	                which is what lets an output path be composed from two
//	                independent profiles — the piece that makes the three-
//	                axis scheme work at all.
//	book: title:    the book's title, used as the slide deck's title too.
//
// One is qm's own:
//
//	qm:             what Quarto cannot express. `project: output-dir` and
//	                format-level keys such as `reference-doc` do *not*
//	                resolve `{{< var >}}` (only `book: output-file` and
//	                `book: title` do), so everything that has to vary along
//	                two axes at once is declared here and carried out by
//	                `qm prepare` / `qm finalize`.
//
// Quarto ignores the unknown root key `qm:`; it travels into the pandoc
// metadata and is otherwise inert.

// QM is the `qm:` section of a profile.
type QM struct {
	// Folder is the content folder a topic profile renders (topic axis).
	// Empty means "same name as the topic", e.g. topic-calltaker →
	// calltaker/. The topic `none` renders no folder at all.
	Folder string `yaml:"folder"`

	// Formats, Audiences, and Topics restrict the matrix `qm render`
	// builds. A topic profile names the formats and audiences it takes part
	// in — sysadmin has no agency variants and no dark slide deck — and a
	// format profile may do the same from its side, e.g. the authors'
	// complete website only makes sense for the `all` audience. Empty means
	// "every value the project offers on that axis"; where both sides
	// declare one, both have to agree.
	Formats   []string `yaml:"formats"`
	Audiences []string `yaml:"audiences"`
	Topics    []string `yaml:"topics"`

	// Copy is a target→source map of files `qm prepare` copies into place
	// before Quarto runs (format axis). It exists because format-level
	// keys do not resolve `{{< var >}}`: a PPTX reference template that
	// depends on both the format (dark or not) and the audience (POL, FW)
	// cannot be named in `format: pptx: reference-doc:`. The format profile
	// points that key at a fixed path instead, and the right template is
	// copied there. Paths are project-relative; the source may contain
	// `{{< var >}}`.
	Copy map[string]string `yaml:"copy"`

	// OutputFile renames what Quarto produced (format axis). Quarto honours
	// `book: output-file` for book projects, so books do not need it; a
	// `type: default` project rendering `_build/slides.qmd` writes
	// `<output-dir>/_build/slides.pptx` and has no interpolating key to fix
	// that with. The value is a file name stem and may contain
	// `{{< var >}}`; the extension of each produced file is kept.
	OutputFile string `yaml:"output-file"`

	// OutputDirSuffix is appended to the project's output directory after
	// the render (format axis). The website's output directory depends on
	// the *audience* (`_output/site-pol`), and `project: output-dir` is one
	// of the keys that does not interpolate. May contain `{{< var >}}`; an
	// empty result leaves the directory alone.
	OutputDirSuffix string `yaml:"output-dir-suffix"`
}

// Profile is one `_quarto-<axis>-<value>.yml` file, as far as qm reads it.
type Profile struct {
	// Name is the profile name as Quarto knows it, e.g. "topic-calltaker".
	Name  string
	Axis  Axis
	Value string
	// Path is the file the profile was read from.
	Path string

	Vars    map[string]string `yaml:"_quarto-vars"`
	QM      QM                `yaml:"qm"`
	Project struct {
		OutputDir string `yaml:"output-dir"`
	} `yaml:"project"`
	Book struct {
		Title      string `yaml:"title"`
		OutputFile string `yaml:"output-file"`
		Chapters   []any  `yaml:"chapters"`
	} `yaml:"book"`
}

// FormatOutputDir returns the `project: output-dir` a format profile
// declares, project-relative, or "" when it declares none. The value is
// taken verbatim: Quarto does not resolve `{{< var >}}` in it (it creates a
// directory called `{{< var format >}}` instead), which is why the
// audience-dependent part of a path is `qm: output-dir-suffix:` instead.
func FormatOutputDir(p *Profile) (string, error) {
	if p == nil {
		return "", nil
	}
	if varRef.MatchString(p.Project.OutputDir) {
		return "", fmt.Errorf(
			"%s: project: output-dir: %q contains a variable, but Quarto does "+
				"not resolve {{< var >}} there; use qm: output-dir-suffix: instead",
			p.Path, p.Project.OutputDir)
	}
	return p.Project.OutputDir, nil
}

// LoadProfile reads the profile named name (with or without the `_quarto-`
// prefix) from docRoot.
//
// A missing file is an error. Quarto passes over one silently, which turns
// a typo into an output file called `calltaker-?var:audience.docx` — the
// failure this loader exists to prevent.
func LoadProfile(docRoot, name string) (*Profile, error) {
	axis, value, ok := SplitProfileName(name)
	if !ok {
		return nil, fmt.Errorf("profile %q has no known axis prefix", name)
	}
	full := NormalizeProfileArg(axis.ProfileName(value))
	path := ResolveProfilePath(docRoot, full)
	if _, err := os.Stat(path); err != nil {
		// ResolveProfilePath falls back to .yaml; report the name, not the
		// guessed extension.
		return nil, fmt.Errorf("no profile file %s.yml in %s",
			full, docRoot)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	p := &Profile{Name: axis.ProfileName(value), Axis: axis, Value: value, Path: path}
	if err := yaml.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", path, err)
	}
	if len(p.Book.Chapters) > 0 {
		// Quarto concatenates array keys across the base config and every
		// active profile instead of replacing them, so a chapter list here
		// is added to the one in _quarto.yml rather than overriding it —
		// the flattened book would render twice, once per chapter.
		return nil, fmt.Errorf(
			"%s sets book: chapters:; the chapter list belongs in _quarto.yml "+
				"only (Quarto concatenates it with the profile's)", path)
	}
	return p, nil
}

// Profiles holds the three profiles of one selection, in axis order.
type Profiles struct {
	Selection Selection
	Topic     *Profile
	Format    *Profile
	Audience  *Profile
}

// LoadSelection reads the three profile files of a selection.
func LoadSelection(docRoot string, sel Selection) (*Profiles, error) {
	ps := &Profiles{Selection: sel}
	for _, a := range Axes {
		p, err := LoadProfile(docRoot, a.ProfileName(sel.Get(a)))
		if err != nil {
			return nil, err
		}
		switch a {
		case AxisTopic:
			ps.Topic = p
		case AxisFormat:
			ps.Format = p
		case AxisAudience:
			ps.Audience = p
		}
	}
	return ps, nil
}

// All returns the three profiles in the order Quarto merges them, which is
// the order --profile lists them in: topic, format, audience.
func (p *Profiles) All() []*Profile { return []*Profile{p.Topic, p.Format, p.Audience} }

// Vars merges the profiles' `_quarto-vars` the way Quarto merges profile
// configuration: the first profile that defines a key wins.
func (p *Profiles) Vars() map[string]string {
	out := map[string]string{}
	for _, prof := range p.All() {
		if prof == nil {
			continue
		}
		for k, v := range prof.Vars {
			if _, taken := out[k]; !taken {
				out[k] = v
			}
		}
	}
	return out
}

// Folder is the content folder the selected topic renders, or "" when the
// topic selects no folder (topic-none, i.e. a website render).
func (p *Profiles) Folder() string {
	if p.Selection.Topic == NoTopic {
		return ""
	}
	if p.Topic != nil && p.Topic.QM.Folder != "" {
		return p.Topic.QM.Folder
	}
	return p.Selection.Topic
}

// Title is the book title with its variables resolved — the title of the
// slide deck as well, which has no other source for one.
func (p *Profiles) Title() (string, error) {
	if p.Topic == nil || p.Topic.Book.Title == "" {
		return "", nil
	}
	return ResolveVars(p.Topic.Book.Title, p.Vars())
}

// varRef matches Quarto's `{{< var name >}}` shortcode.
var varRef = regexp.MustCompile(`\{\{<\s*var\s+([^\s>]+)\s*>\}\}`)

// ResolveVars substitutes `{{< var name >}}` from vars, the way Quarto
// resolves the shortcode in `book: output-file` and `book: title`.
//
// An undefined variable is an error rather than Quarto's `?var:name`
// placeholder: the placeholder's only visible effect is a wrongly named
// output file, discovered long after the render.
func ResolveVars(s string, vars map[string]string) (string, error) {
	var missing []string
	out := varRef.ReplaceAllStringFunc(s, func(m string) string {
		name := varRef.FindStringSubmatch(m)[1]
		v, ok := vars[name]
		if !ok {
			missing = append(missing, name)
			return ""
		}
		return v
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		known := make([]string, 0, len(vars))
		for k := range vars {
			known = append(known, k)
		}
		sort.Strings(known)
		return "", fmt.Errorf(
			"undefined variable(s) %s in %q; the selected profiles define %s",
			strings.Join(missing, ", "), s, strings.Join(known, ", "))
	}
	return out, nil
}

// ProjectPath joins a project-relative path from a profile onto docRoot and
// refuses to leave the project.
func ProjectPath(docRoot, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("%q must be a project-relative path", rel)
	}
	abs := filepath.Join(docRoot, clean)
	inside, err := filepath.Rel(docRoot, abs)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q points outside the project", rel)
	}
	return abs, nil
}
