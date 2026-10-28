package bookrender

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/christophberger-ailab/qm/internal/qmcore"
)

// The Pandoc renderer (`qm: renderer: pandoc`) builds a selection without
// Quarto and without the project's hooks:
//
//  1. the topic's folder is flattened in memory (the same flattening the
//     Quarto renderers use, see internal/bookmaker);
//  2. the shortcodes the content uses are resolved (see shortcodes.go);
//  3. Pandoc reads the result with the project's own filter chain (see
//     pandoc_filter.lua) and writes the DOCX or PPTX;
//  4. for a DOCX, the filter chain also saves the document it is about to
//     write, and qm lays out the PDF from that and adds the cover to both.
//
// Nothing is written to `_build/`. A render has no state to go stale: the
// failure of a Quarto render with an old `_build/book.qmd` — a handout with
// the right title and the wrong topic's content — cannot happen here.

// pandocPlan is a selection's Pandoc renderer configuration, resolved and
// checked. Every check runs before anything is written.
type pandocPlan struct {
	kind      string // "docx" or "pptx"
	folder    string
	title     string
	lang      string
	make      map[string]any
	filters   []string
	options   qmcore.FormatOptions
	outDir    string // absolute
	stem      string
	reference string // absolute, or "" for Pandoc's own template
	highlight string
	native    *Native // DOCX only: the PDF and the cover
}

func newPandocPlan(root string, ps *qmcore.Profiles) (*pandocPlan, error) {
	f := ps.Format
	p := &pandocPlan{folder: ps.Folder()}
	if p.folder == "" {
		return nil, fmt.Errorf("%s: qm: renderer: pandoc builds one topic's folder, but topic %q "+
			"addresses the whole project; use a website format for that", f.Path, ps.Selection.Topic)
	}
	for name := range f.Format {
		switch name {
		case "docx", "pptx":
			if p.kind != "" {
				return nil, fmt.Errorf("%s: qm: renderer: pandoc builds one of docx and pptx, not both", f.Path)
			}
			p.kind = name
		case "pdf":
			return nil, fmt.Errorf("%s: qm: renderer: pandoc writes the PDF itself; remove format: pdf: "+
				"and set its options under qm: pdf:", f.Path)
		default:
			return nil, fmt.Errorf("%s: qm: renderer: pandoc supports format: docx and pptx, not %s", f.Path, name)
		}
	}
	if p.kind == "" {
		return nil, fmt.Errorf("%s: qm: renderer: pandoc requires format: docx or format: pptx", f.Path)
	}
	p.options = f.Format[p.kind]
	vars := ps.Vars()

	outDir, err := qmcore.FormatOutputDir(f)
	if err != nil {
		return nil, err
	}
	if outDir == "" {
		return nil, fmt.Errorf("%s: qm: renderer: pandoc needs project: output-dir:", f.Path)
	}
	if p.outDir, err = qmcore.ProjectPath(root, outDir); err != nil {
		return nil, fmt.Errorf("%s: output-dir: %w", f.Path, err)
	}

	// A book's file name is `book: output-file`; a deck has no book, so it
	// carries its name under `qm: output-file`.
	pattern := f.QM.OutputFile
	if pattern == "" {
		pattern = f.Book.OutputFile
	}
	if pattern == "" {
		return nil, fmt.Errorf("%s: qm: renderer: pandoc needs book: output-file: (or qm: output-file:) "+
			"to name its output", f.Path)
	}
	if p.stem, err = qmcore.ResolveVars(pattern, vars); err != nil {
		return nil, fmt.Errorf("%s: output file name: %w", f.Path, err)
	}
	if p.stem == "" {
		return nil, fmt.Errorf("%s: the output file name resolves to nothing", f.Path)
	}

	if p.reference, err = referenceDoc(root, ps, p.options.ReferenceDoc); err != nil {
		return nil, err
	}
	if p.title, err = ps.Title(); err != nil {
		return nil, err
	}
	base, err := qmcore.LoadBase(root)
	if err != nil {
		return nil, err
	}
	p.lang = ps.Language(base)
	p.make = ps.MakeFlags(base)
	if p.filters, err = projectFilters(root, ps.FilterEntries(base)); err != nil {
		return nil, err
	}
	if p.kind == "docx" {
		p.highlight = resolveHighlight(root, firstNonEmpty(p.options.SyntaxHighlighting, p.options.HighlightStyle))
		if p.native, err = newNative(root, ps); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// referenceDoc resolves a format's `reference-doc:`. The key does not
// resolve `{{< var >}}`, so a template that depends on the audience is named
// by a `qm: copy:` target (`_build/reference-slides.pptx`) that `qm prepare`
// would fill in. Here the copy is not needed: the target's source is the
// template.
func referenceDoc(root string, ps *qmcore.Profiles, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	for _, prof := range ps.All() {
		if prof == nil {
			continue
		}
		for target, source := range prof.QM.Copy {
			if filepath.ToSlash(filepath.Clean(target)) != filepath.ToSlash(filepath.Clean(ref)) {
				continue
			}
			resolved, err := qmcore.ResolveVars(source, ps.Vars())
			if err != nil {
				return "", fmt.Errorf("%s: qm: copy: %s: %w", prof.Path, target, err)
			}
			ref = resolved
		}
	}
	abs, err := qmcore.ProjectPath(root, ref)
	if err != nil {
		return "", fmt.Errorf("%s: reference-doc: %w", ps.Format.Path, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("%s: reference-doc: %w", ps.Format.Path, err)
	}
	return abs, nil
}

// resolveHighlight picks the code highlighting style. A style is a theme
// file of the project, one of the themes Quarto ships, or the name of one of
// Pandoc's own. Left unset it is the style Quarto uses, when Quarto is
// installed, so that the code in a DOCX looks as it did under Quarto.
func resolveHighlight(root, value string) string {
	share := quartoShareDir()
	themes := filepath.Join(share, "pandoc", "highlight-styles")
	if value == "" {
		if share == "" {
			return ""
		}
		value = "arrow"
	}
	if abs, err := qmcore.ProjectPath(root, value); err == nil && fileExists(abs) {
		return abs
	}
	if share != "" {
		for _, name := range []string{value, value + "-light"} {
			if p := filepath.Join(themes, name+".theme"); fileExists(p) {
				return p
			}
		}
	}
	return value
}

// The callout titles and the contents heading Quarto writes per language.
var (
	calloutTitles = map[string]map[string]string{
		"de": {"note": "Hinweis", "tip": "Tipp", "warning": "Warnung", "important": "Wichtig", "caution": "Vorsicht"},
		"en": {"note": "Note", "tip": "Tip", "warning": "Warning", "important": "Important", "caution": "Caution"},
	}
	tocTitles = map[string]string{"de": "Inhaltsverzeichnis", "en": "Table of Contents"}
)

// languageKey reduces a language tag (`de-DE`) to the key of the tables
// above; a language they do not cover falls back to English.
func languageKey(lang string) string {
	key, _, _ := strings.Cut(strings.ToLower(lang), "-")
	if _, ok := calloutTitles[key]; ok {
		return key
	}
	return "en"
}

// calloutIconDir finds the callout icons: the project's own
// `assets/callout-icons/`, else the ones Quarto ships. Without either a
// callout is drawn without its icon.
func calloutIconDir(root string) string {
	dirs := []string{filepath.Join(root, "assets", "callout-icons")}
	if share := quartoShareDir(); share != "" {
		dirs = append(dirs, filepath.Join(share, "formats", "docx"))
	}
	for _, dir := range dirs {
		if fileExists(filepath.Join(dir, "note.png")) {
			return filepath.ToSlash(dir)
		}
	}
	return ""
}

// renderPandoc builds one selection with Pandoc.
func renderPandoc(root string, ps *qmcore.Profiles, dryRun bool, log Logf) (err error) {
	log = log.or()
	plan, err := newPandocPlan(root, ps)
	if err != nil {
		return err
	}
	final := filepath.Join(plan.outDir, plan.stem+"."+plan.kind)
	if plan.kind == "docx" {
		cover := "none"
		if plan.native.Cover != "" {
			cover = relTo(root, plan.native.Cover)
		}
		log("pandoc renderer: Pandoc writes the DOCX; qm adds the PDF and the cover (%s)", cover)
	} else {
		log("pandoc renderer: Pandoc writes the PPTX")
	}

	var tool *pandocTool
	if dryRun {
		tool = &pandocTool{argv: []string{"pandoc"}, major: 3, minor: 10, tableAttributes: true}
	} else if tool, err = findPandoc(); err != nil {
		return err
	}

	book, slides, err := BuildDocuments(root, plan.folder, plan.title, log)
	if err != nil {
		return err
	}
	input := book
	if plan.kind == "pptx" {
		input = slides
		if strings.TrimSpace(input) == "" {
			// A topic without slides still gets a deck: a title slide.
			input = generatedNote + "---\ntitle: " + yamlQuote(plan.title) + "\n---\n"
		}
	}
	sc := newShortcodes(root, projectVariables(root, ps), func(format string, args ...any) {
		log("warning: "+format, args...)
	})
	text, err := sc.Expand(input)
	if err != nil {
		return err
	}
	if sc.mermaid > 0 {
		log("warning: %d mermaid diagram(s) stay code blocks: the pandoc renderer cannot draw them; "+
			"export them as images", sc.mermaid)
	}

	cfg := filterConfig{
		Kind:    plan.kind,
		Filters: plan.filters,
		Titles:  calloutTitles[languageKey(plan.lang)],
	}
	if plan.kind == "docx" {
		if cfg.Icons = calloutIconDir(root); cfg.Icons == "" && strings.Contains(text, "callout") {
			log("warning: no callout icons found (assets/callout-icons/ or a Quarto installation); " +
				"callouts are drawn without them")
		}
	}

	if dryRun {
		job := pandocJob{
			Kind: plan.kind, Input: "<tmp>/input.md", Output: relTo(root, final), Metadata: "<tmp>/meta.yaml",
			Filter: "<tmp>/filter.lua", Reference: relTo(root, plan.reference), TOC: plan.options.TOC,
			SlideLevel: plan.options.SlideLevel, Highlight: plan.highlight,
		}
		log("$ pandoc %s", strings.Join(tool.args(job), " "))
		return nil
	}

	tmp, err := os.MkdirTemp("", "qm-pandoc-")
	if err != nil {
		return err
	}
	// A failed render keeps its working files: the captured document is
	// what a PDF layout problem is diagnosed from. QM_KEEP_WORKDIR=1 keeps
	// them after a successful render too.
	keep := true
	defer func() {
		if keep || os.Getenv("QM_KEEP_WORKDIR") != "" {
			log("working files retained for diagnosis: %s", tmp)
		} else {
			os.RemoveAll(tmp)
		}
	}()
	if plan.kind == "docx" {
		cfg.Capture = filepath.ToSlash(filepath.Join(tmp, "document.json"))
	}
	files := map[string]string{
		"input.md":   text,
		"filter.lua": cfg.Script(),
	}
	meta, err := plan.metadata()
	if err != nil {
		return err
	}
	files["meta.yaml"] = meta
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(content), 0o644); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(plan.outDir, 0o755); err != nil {
		return err
	}
	out, err := os.CreateTemp(plan.outDir, ".qm-*."+plan.kind)
	if err != nil {
		return err
	}
	out.Close()
	defer os.Remove(out.Name())

	job := pandocJob{
		Kind: plan.kind, Input: filepath.Join(tmp, "input.md"), Output: out.Name(),
		Metadata: filepath.Join(tmp, "meta.yaml"), Filter: filepath.Join(tmp, "filter.lua"),
		Reference: plan.reference, TOC: plan.options.TOC, SlideLevel: plan.options.SlideLevel,
		Highlight: plan.highlight,
	}
	args := tool.args(job)
	log("$ %s %s", strings.Join(tool.argv, " "), strings.Join(args, " "))
	if err := tool.run(context.Background(), root, args, log); err != nil {
		return err
	}
	if err := os.Chmod(out.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(out.Name(), final); err != nil {
		return err
	}
	if plan.kind == "docx" {
		for _, note := range plan.native.FontNotes {
			log("%s", note)
		}
		if err := plan.native.FinishFrom(root, final, filepath.Join(tmp, "document.json"), log); err != nil {
			return err
		}
	} else {
		log("created %s", relTo(root, final))
	}
	keep = false
	return nil
}

// metadata is the document metadata Pandoc gets besides the document
// itself: the flags the project's filter reads, the language, the book's (or
// deck's) title, and the heading of a DOCX's contents list.
//
// The title cannot come from the document: the generated documents begin
// with a comment line, which keeps Pandoc from reading their front matter.
func (p *pandocPlan) metadata() (string, error) {
	meta := map[string]any{"make": p.make}
	if p.lang != "" {
		meta["lang"] = p.lang
	}
	if p.title != "" {
		meta["title"] = p.title
	}
	if p.kind == "docx" && p.options.TOC {
		if t, ok := tocTitles[languageKey(p.lang)]; ok {
			meta["toc-title"] = t
		}
	}
	out, err := yaml.Marshal(meta)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
