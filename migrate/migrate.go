// Package migrate implements the `qm migrate` subcommand.
//
// It reads the old Quarto-based configuration of a project — one
// _quarto.yml plus one profile file per axis value (_quarto-topic-<t>.yml,
// _quarto-format-<f>.yml, _quarto-audience-<a>.yml) — and writes the
// equivalent qm.yml, so a project does not have to be converted by hand.
package migrate

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/christophberger-ailab/qm/internal/cli"
	"github.com/christophberger-ailab/qm/internal/config"
	"github.com/christophberger-ailab/qm/internal/qmcore"
	"github.com/christophberger/start"
	flag "github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

var (
	projectPath *string
	forceFlag   *bool
)

// Register wires the `migrate` command into start.
func Register(projectFlag *string) {
	projectPath = projectFlag
	forceFlag = flag.Bool("force", false, "Overwrite an existing qm.yml")
	// `render` defines --dry-run too; a flag can only be defined once.
	if f := flag.Lookup("dry-run"); f != nil {
		dryRunLookup = func() bool { return f.Value.String() == "true" }
	} else {
		d := flag.Bool("dry-run", false, "Print the qm.yml instead of writing it")
		dryRunLookup = func() bool { return *d }
	}

	start.Add(&start.Command{
		Name:  "migrate",
		Short: "Write qm.yml from the project's Quarto configuration",
		Long: "Read _quarto.yml and the _quarto-topic-*.yml, _quarto-format-*.yml " +
			"and _quarto-audience-*.yml profiles and write the equivalent " +
			"qm.yml. Use --dry-run to print it instead, --force to overwrite " +
			"an existing qm.yml.",
		Flags: []string{"project", "force", "dry-run"},
		Cmd:   cli.Guard(cmd),
	})
}

var dryRunLookup = func() bool { return false }

func cmd(c *start.Command) error {
	if projectPath == nil {
		return fmt.Errorf("migrate: --project flag not initialised")
	}
	root, err := filepath.Abs(*projectPath)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}
	res, err := Run(root, Options{Force: *forceFlag, DryRun: dryRunLookup()})
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err != nil {
		return err
	}
	if res.Written {
		fmt.Printf("wrote %s\n", filepath.Join(root, config.FileName))
	} else {
		fmt.Print(res.YAML)
	}
	return nil
}

// Options control Run.
type Options struct {
	Force  bool // overwrite an existing qm.yml
	DryRun bool // do not write the file
}

// Result is what Run produced.
type Result struct {
	YAML     string
	Warnings []string
	Written  bool
}

// varRef matches Quarto's `{{< var name >}}` shortcode.
var varRef = regexp.MustCompile(`\{\{<\s*var\s+([^\s>]+)\s*>\}\}`)

var builtins = map[string]qmcore.Axis{
	"topic": qmcore.AxisTopic, "format": qmcore.AxisFormat, "audience": qmcore.AxisAudience,
}

// rawQuarto is the part of a Quarto config file qm.yml needs beyond what
// qmcore reads.
type rawQuarto struct {
	Project struct {
		Type string `yaml:"type"`
	} `yaml:"project"`
	Book struct {
		Title string `yaml:"title"`
	} `yaml:"book"`
	Website struct {
		Title string `yaml:"title"`
	} `yaml:"website"`
	Filters []any `yaml:"filters"`
}

func readRaw(p string) rawQuarto {
	var r rawQuarto
	if data, err := os.ReadFile(p); err == nil {
		_ = yaml.Unmarshal(data, &r)
	}
	return r
}

type migrator struct {
	warnings []string
	renames  map[string]string // built-in var name → new name
}

func (m *migrator) warn(format string, args ...any) {
	m.warnings = append(m.warnings, fmt.Sprintf(format, args...))
}

// conv turns `{{< var x >}}` into `{x}`, applying the variable renames.
func (m *migrator) conv(s string) string {
	return varRef.ReplaceAllStringFunc(s, func(x string) string {
		name := varRef.FindStringSubmatch(x)[1]
		if n, ok := m.renames[name]; ok {
			name = n
		}
		return "{" + name + "}"
	})
}

// Run converts the project in root. The result is verified with
// config.Load before it is reported as written.
func Run(root string, opts Options) (*Result, error) {
	res := &Result{}
	target := filepath.Join(root, config.FileName)
	if !opts.DryRun && !opts.Force {
		if _, err := os.Stat(target); err == nil {
			return res, fmt.Errorf("%s exists; use --force to overwrite it", target)
		}
	}
	m := &migrator{renames: map[string]string{}}
	doc, err := m.build(root)
	res.Warnings = m.warnings
	if err != nil {
		return res, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return res, err
	}
	_ = enc.Close()
	res.YAML = buf.String()

	// Verify: parse what would be written.
	dir := root
	if opts.DryRun {
		dir, err = os.MkdirTemp("", "qm-migrate-")
		if err != nil {
			return res, err
		}
		defer os.RemoveAll(dir)
	}
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(res.YAML), 0o644); err != nil {
		return res, err
	}
	if _, err := config.Load(dir); err != nil {
		return res, fmt.Errorf("generated qm.yml does not load: %w", err)
	}
	res.Written = !opts.DryRun
	return res, nil
}

type profiles struct {
	topics, formats, audiences []*qmcore.Profile
}

func loadAll(root string) (profiles, error) {
	var ps profiles
	for _, ax := range qmcore.Axes {
		names, err := qmcore.AxisValues(root, ax)
		if err != nil {
			return ps, err
		}
		for _, n := range names {
			p, err := qmcore.LoadProfile(root, ax.ProfileName(n))
			if err != nil {
				return ps, err
			}
			switch ax {
			case qmcore.AxisTopic:
				ps.topics = append(ps.topics, p)
			case qmcore.AxisFormat:
				ps.formats = append(ps.formats, p)
			case qmcore.AxisAudience:
				ps.audiences = append(ps.audiences, p)
			}
		}
	}
	return ps, nil
}

func (m *migrator) build(root string) (*yaml.Node, error) {
	base, err := qmcore.LoadBase(root)
	if err != nil {
		return nil, err
	}
	ps, err := loadAll(root)
	if err != nil {
		return nil, err
	}
	if len(ps.topics)+len(ps.formats)+len(ps.audiences) == 0 {
		return nil, fmt.Errorf("no _quarto-topic-*, _quarto-format-* or _quarto-audience-* profiles found in %s", root)
	}
	var sources, filters []string
	addFilter := func(f any) {
		s := ""
		switch v := f.(type) {
		case string:
			s = v
		case map[string]any:
			s, _ = v["path"].(string)
		}
		if s == "" || s == "quarto" {
			return
		}
		for _, x := range filters {
			if x == s {
				return
			}
		}
		filters = append(filters, s)
	}
	var rawBase rawQuarto
	if base.Path != "" {
		sources = append(sources, filepath.Base(base.Path))
		rawBase = readRaw(base.Path)
	}
	for _, f := range base.Filters {
		addFilter(f)
	}
	all := append(append(append([]*qmcore.Profile{}, ps.topics...), ps.formats...), ps.audiences...)
	for _, p := range all {
		sources = append(sources, filepath.Base(p.Path))
		for _, f := range p.Filters {
			addFilter(f)
		}
	}

	// Built-in variable names that old profiles define with another value.
	for _, p := range all {
		for k, v := range p.Vars {
			ax, ok := builtins[k]
			if !ok || (ax == p.Axis && v == p.Value) {
				continue
			}
			m.renames[k] = k + "_suffix"
		}
	}
	vars := func(p *qmcore.Profile) *yaml.Node {
		out := map[string]string{}
		for k, v := range p.Vars {
			if ax, ok := builtins[k]; ok && ax == p.Axis && v == p.Value {
				continue
			}
			if n, ok := m.renames[k]; ok {
				k = n
			}
			out[k] = v
		}
		return strMap(out)
	}

	// Which topics and formats and audiences survive.
	topicNames := map[string]bool{}
	for _, p := range ps.topics {
		if !qmcore.IsWholeProject(p.Value) {
			topicNames[p.Value] = true
		}
	}
	audNames := map[string]bool{}
	for _, p := range ps.audiences {
		audNames[p.Value] = true
	}
	fmtInfo := map[string]*formatInfo{}
	for _, p := range ps.formats {
		fi := m.analyze(p, rawBase)
		if fi == nil {
			continue
		}
		fmtInfo[p.Value] = fi
	}
	filterList := func(owner, kind string, in []string, known map[string]bool) []string {
		var out []string
		for _, n := range in {
			switch {
			case qmcore.IsWholeProject(n) && kind == "topic":
			case known[n]:
				out = append(out, n)
			default:
				m.warn("%s: dropped %s %q from its list (no such %s in the new configuration)", owner, kind, n, kind)
			}
		}
		return out
	}
	fmtKnown := map[string]bool{}
	for n := range fmtInfo {
		fmtKnown[n] = true
	}

	// Output directory: the common parent of the formats' output dirs.
	var dirs []string
	for _, p := range ps.formats {
		if _, ok := fmtInfo[p.Value]; ok && p.Project.OutputDir != "" {
			dirs = append(dirs, path.Clean(varRef.ReplaceAllString(filepath.ToSlash(p.Project.OutputDir), "x")))
		}
	}
	output := "_output"
	if len(dirs) > 0 {
		output = commonParent(dirs)
	}

	doc := &yaml.Node{Kind: yaml.MappingNode}
	title := rawBase.Book.Title
	if title == "" {
		title = rawBase.Website.Title
	}
	if title != "" {
		if varRef.MatchString(title) {
			m.warn("project title %q contains a variable; not copied", title)
		} else {
			add(doc, "title", str(title))
		}
	}
	lang := base.Lang
	if lang == "" {
		for _, p := range all {
			if p.Lang != "" {
				lang = p.Lang
				break
			}
		}
	}
	if lang != "" {
		add(doc, "lang", str(lang))
	}
	if output != "_output" {
		add(doc, "output", str(output))
	}

	// formats
	nAud := len(ps.audiences)
	fm := &yaml.Node{Kind: yaml.MappingNode}
	for _, p := range ps.formats {
		fi := fmtInfo[p.Value]
		if fi == nil {
			continue
		}
		e := &yaml.Node{Kind: yaml.MappingNode}
		add(e, "type", typeNode(fi.types))
		if len(fi.show) > 0 {
			add(e, "show", flow(fi.show))
		}
		if fi.toc {
			add(e, "toc", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
		}
		if fi.template != "" {
			add(e, "template", str(m.conv(fi.template)))
		}
		dir := ""
		if p.Project.OutputDir != "" {
			rel, err := relPath(output, m.conv(filepath.ToSlash(p.Project.OutputDir)))
			if err != nil {
				m.warn("format %s: output-dir %q is outside %s", p.Value, p.Project.OutputDir, output)
			} else {
				dir = rel
			}
		}
		if dir == "" {
			dir = p.Value
		}
		dir += m.conv(p.QM.OutputDirSuffix)
		if dir != p.Value {
			add(e, "dir", str(dir))
		}
		file := p.Book.OutputFile
		if file == "" {
			file = p.QM.OutputFile
		}
		if file != "" {
			file = m.conv(file)
			def := "{topic}"
			if nAud > 1 {
				def += "-{audience}"
			}
			if file != def {
				add(e, "file", str(file))
			}
		}
		if l := filterList("format "+p.Value, "topic", p.QM.Topics, topicNames); len(l) > 0 {
			add(e, "topics", flow(l))
		}
		if l := filterList("format "+p.Value, "audience", p.QM.Audiences, audNames); len(l) > 0 {
			add(e, "audiences", flow(l))
		}
		if v := vars(p); v != nil {
			add(e, "vars", v)
		}
		add(fm, p.Value, e)
	}
	if len(fm.Content) > 0 {
		add(doc, "formats", fm)
	}

	// audiences
	am := &yaml.Node{Kind: yaml.MappingNode}
	for _, p := range ps.audiences {
		e := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
		var show []string
		for _, k := range trueKeys(p.Make) {
			if k != p.Value {
				show = append(show, k)
			}
		}
		if len(show) > 0 {
			add(e, "show", flow(show))
		}
		if v := vars(p); v != nil {
			add(e, "vars", v)
		}
		if len(e.Content) > 2 {
			e.Style = 0
		}
		add(am, p.Value, e)
	}
	if len(am.Content) > 0 {
		add(doc, "audiences", am)
	}

	// topics
	tm := &yaml.Node{Kind: yaml.MappingNode}
	for _, p := range ps.topics {
		if qmcore.IsWholeProject(p.Value) {
			continue
		}
		e := &yaml.Node{Kind: yaml.MappingNode}
		if p.QM.Folder != "" && p.QM.Folder != p.Value {
			add(e, "folder", str(p.QM.Folder))
		}
		if t := p.Book.Title; t != "" {
			if varRef.MatchString(t) {
				m.warn("topic %s: title %q contains a variable; dropped", p.Value, t)
			} else {
				add(e, "title", str(t))
			}
		}
		if p.QM.Cover != "" {
			add(e, "cover", str(m.conv(p.QM.Cover)))
		}
		if l := filterList("topic "+p.Value, "format", p.QM.Formats, fmtKnown); len(l) > 0 {
			add(e, "formats", flow(l))
		} else if len(p.QM.Formats) > 0 {
			m.warn("topic %s: none of its formats survive; it now takes part in every format", p.Value)
		}
		if l := filterList("topic "+p.Value, "audience", p.QM.Audiences, audNames); len(l) > 0 {
			add(e, "audiences", flow(l))
		}
		if v := vars(p); v != nil {
			add(e, "vars", v)
		}
		if len(e.Content) == 0 {
			e.Style = yaml.FlowStyle
		}
		add(tm, p.Value, e)
	}
	if len(tm.Content) > 0 {
		add(doc, "topics", tm)
	}

	// Header comment.
	var h strings.Builder
	h.WriteString("Generated by `qm migrate` from: " + strings.Join(sources, ", ") + ".\n")
	h.WriteString("Once you have checked that `qm render` produces what it used to, delete\n")
	h.WriteString("these files; qm no longer reads them.\n")
	if len(filters) > 0 {
		h.WriteString("\nThe project's Lua filters are no longer run: " + strings.Join(filters, ", ") + ".\n")
		h.WriteString("Conditional content is selected by `show:` of the format and the audience.\n")
	}
	if len(m.renames) > 0 {
		var l []string
		for k, v := range m.renames {
			l = append(l, fmt.Sprintf("%s -> %s", k, v))
		}
		sort.Strings(l)
		h.WriteString("\n`topic`, `format` and `audience` are built-in variables that always hold the\n")
		h.WriteString("entry's name. Old variables of those names with another value were renamed\n")
		h.WriteString("(" + strings.Join(l, ", ") + ") and every placeholder that used them.\n")
	}
	doc.HeadComment = strings.TrimRight(h.String(), "\n")
	return doc, nil
}

type formatInfo struct {
	types    []string
	toc      bool
	template string
	show     []string
}

// analyze derives what a format profile writes; nil when it is skipped.
func (m *migrator) analyze(p *qmcore.Profile, rawBase rawQuarto) *formatInfo {
	raw := readRaw(p.Path)
	_, hasHTML := p.Format["html"]
	docx, hasDOCX := p.Format["docx"]
	_, hasPDF := p.Format["pdf"]
	_, hasPPTX := p.Format["pptx"]
	renderer := p.QM.Renderer
	web := hasHTML || raw.Project.Type == "website" || (raw.Project.Type == "" && rawBase.Project.Type == "website" && !hasDOCX && !hasPPTX && !hasPDF)

	fi := &formatInfo{show: trueKeys(p.Make)}
	switch {
	case web:
		fi.types = []string{config.TypeHTML}
	case hasDOCX || (renderer == "qm" && !hasPPTX):
		fi.types = []string{config.TypeDOCX}
		if renderer == "qm" || renderer == "pandoc" {
			fi.types = append(fi.types, config.TypePDF)
		}
		fi.toc = docx.TOC || p.QM.PDF.TOC
		if t := docx.ReferenceDoc; t != "" {
			if strings.HasPrefix(path.Clean(t), "_build/") {
				if src, ok := lookupCopy(p.QM.Copy, t); ok {
					t = src
				} else {
					m.warn("format %s: template %s is generated into _build/ and no qm: copy: entry names its source; check `template:`", p.Value, t)
				}
			}
			fi.template = t
		}
	case hasPDF:
		fi.types = []string{config.TypePDF}
	case hasPPTX:
		m.warn("format %s: slide decks are not supported yet; format skipped", p.Value)
		return nil
	default:
		m.warn("format %s: no known output format (html, docx, pdf); format skipped", p.Value)
		return nil
	}
	if hasPPTX && !web {
		m.warn("format %s: its pptx output is dropped (slide decks are not supported yet)", p.Value)
	}
	return fi
}

func lookupCopy(cp map[string]string, target string) (string, bool) {
	t := path.Clean(target)
	for k, v := range cp {
		if path.Clean(k) == t {
			return v, true
		}
	}
	return "", false
}

// trueKeys lists the keys of a make: map whose value is true, sorted.
func trueKeys(mk map[string]any) []string {
	var out []string
	for k, v := range mk {
		if b, ok := v.(bool); ok && b {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// commonParent is the longest directory that is a parent of every dir.
func commonParent(dirs []string) string {
	var common []string
	for i, d := range dirs {
		parts := strings.Split(path.Dir(d), "/")
		if path.Dir(d) == "." {
			parts = nil
		}
		if i == 0 {
			common = parts
			continue
		}
		n := 0
		for n < len(common) && n < len(parts) && common[n] == parts[n] {
			n++
		}
		common = common[:n]
	}
	if len(common) == 0 {
		return "."
	}
	return strings.Join(common, "/")
}

func relPath(base, p string) (string, error) {
	p = path.Clean(p)
	if base == "." {
		return p, nil
	}
	if p == base {
		return "", nil
	}
	if !strings.HasPrefix(p, base+"/") {
		return "", io.ErrUnexpectedEOF
	}
	return strings.TrimPrefix(p, base+"/"), nil
}

// YAML node helpers.

func str(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

func add(m *yaml.Node, key string, v *yaml.Node) {
	m.Content = append(m.Content, str(key), v)
}

func flow(l []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, s := range l {
		n.Content = append(n.Content, str(s))
	}
	return n
}

func typeNode(l []string) *yaml.Node {
	if len(l) == 1 {
		return str(l[0])
	}
	return flow(l)
}

// strMap is a flow mapping of string values, nil when empty.
func strMap(m map[string]string) *yaml.Node {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	n := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
	for _, k := range keys {
		add(n, k, str(m[k]))
	}
	return n
}
