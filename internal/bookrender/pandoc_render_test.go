package bookrender

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/christophberger-ailab/qm/internal/qmcore"
)

// pandocProject builds a minimal project the Pandoc renderer can build: one
// topic folder, a handout format that selects the renderer, and the audience
// profiles. Callers overwrite files to vary it.
func pandocProject(t *testing.T, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"_quarto.yml": "lang: de\nmake:\n  audiences: [perle, pol, fw]\nfilters:\n  - audience.lua\n",
		"audience.lua": "function Div(el)\n  if el.classes:includes('private') and not quarto.metadata.get('make.pol') then return {} end\nend\n" +
			"function Span(el)\n  if el.classes:includes('fw') and not quarto.metadata.get('make.fw') then return {} end\nend\n",
		"_quarto-topic-book.yml": "book:\n  title: \"Book {{< var audience-label >}}\"\n_quarto-vars:\n  topic: book\n" +
			"qm:\n  cover: cover.png\n",
		"_quarto-topic-none.yml": "_quarto-vars:\n  topic: \"\"\n",
		"_quarto-format-handout.yml": "project:\n  type: book\n  output-dir: _output/handout\n" +
			"qm:\n  renderer: pandoc\n  pdf:\n    toc: true\n" +
			"book:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n" +
			"make:\n  explanation: true\n" +
			"format:\n  docx:\n    toc: true\n",
		"_quarto-format-slides.yml": "project:\n  type: default\n  output-dir: _output/slides\n" +
			"qm:\n  renderer: pandoc\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n" +
			"  copy:\n    _build/reference.pptx: \"templates/deck{{< var audience-template >}}.pptx\"\n" +
			"make:\n  slide: true\n" +
			"format:\n  pptx:\n    slide-level: 2\n    reference-doc: _build/reference.pptx\n",
		"_quarto-audience-std.yml": "make:\n  perle: true\n  pol: false\n  fw: false\n_quarto-vars:\n  audience: \"\"\n  audience-label: \"\"\n  audience-template: \"\"\n",
		"_quarto-audience-pol.yml": "make:\n  perle: true\n  pol: true\n  fw: false\n_quarto-vars:\n  audience: \"-pol\"\n  audience-label: Polizei\n  audience-template: _POL\n",
		"book/index.qmd":           "---\norder: 1\n---\n# Überblick\n\nText.\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writePNG(t, filepath.Join(root, "cover.png"))
	return root
}

func loadPlan(t *testing.T, root, topic, format, audience string) (*pandocPlan, error) {
	t.Helper()
	ps, err := qmcore.LoadSelection(root, sel(topic, format, audience))
	if err != nil {
		t.Fatal(err)
	}
	return newPandocPlan(root, ps)
}

func TestRendererOf(t *testing.T) {
	for value, want := range map[string]string{"": "quarto", "quarto": "quarto", "qm": "qm", "pandoc": "pandoc"} {
		got, err := RendererOf(&qmcore.Profile{QM: qmcore.QM{Renderer: value}})
		if err != nil || got != want {
			t.Errorf("renderer %q: got %q, %v; want %q", value, got, err, want)
		}
	}
	if _, err := RendererOf(&qmcore.Profile{Path: "p.yml", QM: qmcore.QM{Renderer: "latex"}}); err == nil {
		t.Error("an unknown renderer was accepted")
	}
}

// The hooks must not let Quarto render a format that qm builds itself: it
// would write a DOCX without its PDF and its cover.
func TestLoadNativeRefusesAPandocFormat(t *testing.T) {
	root := pandocProject(t, nil)
	ps, err := qmcore.LoadSelection(root, sel("book", "handout", "std"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = LoadNative(root, ps)
	if err == nil || !strings.Contains(err.Error(), "qm render book --format handout --audience std") {
		t.Errorf("want an error that names the qm command, got %v", err)
	}
}

func TestShortcodes(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("assets/colophon.md", "Colophon with {{< var who >}}.\n\n{{< include /assets/nested.md >}}\n")
	write("assets/nested.md", "Nested text.\n")
	write("assets/loop.md", "{{< include /assets/loop.md >}}\n")

	var warnings []string
	sc := newShortcodes(root, map[string]string{"who": "Octave", "metadata.version": "1.2"}, func(f string, a ...any) {
		warnings = append(warnings, strings.TrimSpace(f))
	})
	in := strings.Join([]string{
		"Intro {{< var who >}} v{{< var metadata.version >}}.",
		"{{< include /assets/colophon.md >}}",
		"{{< pagebreak >}}",
		"```",
		"{{< include /assets/colophon.md >}}",
		"```",
		"```{mermaid}",
		"graph TD",
		"```",
		"Unknown {{< kbd Ctrl >}} twice {{< kbd Alt >}}, and {{< var missing >}}.",
		"Literal {{{< include x >}}}.",
	}, "\n")
	out, err := sc.Expand(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Intro Octave v1.2.",
		"Colophon with Octave.",
		"Nested text.",
		"::: {.qm-pagebreak}",
		"Literal {{< include x >}}.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// Inside a fence, text stays as written.
	if !strings.Contains(out, "```\n{{< include /assets/colophon.md >}}\n```") {
		t.Errorf("an include inside a code fence was expanded:\n%s", out)
	}
	if sc.mermaid != 1 {
		t.Errorf("mermaid blocks: got %d, want 1", sc.mermaid)
	}
	// Each unsupported shortcode is reported once; so is an undefined variable.
	if len(warnings) != 2 {
		t.Errorf("warnings = %q; want one for the kbd shortcode and one for the variable", warnings)
	}

	for name, in := range map[string]string{
		"relative":   "{{< include assets/nested.md >}}",
		"outside":    "{{< include /../secret.md >}}",
		"missing":    "{{< include /assets/none.md >}}",
		"recursive":  "{{< include /assets/loop.md >}}",
		"no file":    "{{< include >}}",
		"quoted bad": `{{< include "assets/x.md" >}}`,
	} {
		if _, err := newShortcodes(root, nil, nil).Expand(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLuaString(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:        `"plain"`,
		`a"b\c`:        `"a\"b\\c"`,
		"line\nbreak":  `"line\nbreak"`,
		"tab\there":    `"tab\there"`,
		"bell\a":       `"bell\007"`,
		"Ünïcödé":      `"Ünïcödé"`,
		`C:\dir\x.lua`: `"C:\\dir\\x.lua"`,
	} {
		if got := luaString(in); got != want {
			t.Errorf("luaString(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestFilterScript(t *testing.T) {
	s := filterConfig{
		Kind: "docx", Filters: []string{"/p/a.lua", `/p/b "x".lua`}, Icons: "/icons",
		Titles: map[string]string{"tip": "Tipp", "note": "Hinweis"}, Capture: "/tmp/doc.json",
	}.Script()
	for _, want := range []string{
		`kind = "docx"`, `filters = {"/p/a.lua", "/p/b \"x\".lua"}`, `icons = "/icons"`,
		`titles = {note = "Hinweis", tip = "Tipp"}`, `capture = "/tmp/doc.json"`, "local chain",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %s", want)
		}
	}
	bare := filterConfig{Kind: "pptx", Titles: map[string]string{}}.Script()
	header, _, _ := strings.Cut(bare, "}\n\n")
	if strings.Contains(header, "icons =") || strings.Contains(header, "capture =") {
		t.Error("an empty icon directory or capture path was written out")
	}
}

func TestProjectFilters(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("own.lua", "")
	write("_extensions/org/gate/_extension.yml", "title: Gate\ncontributes:\n  filters:\n    - gate.lua\n    - path: more.lua\n")
	write("_extensions/org/bad/_extension.yml", "contributes:\n  filters:\n    - thing.ts\n")

	got, err := projectFilters(root, []any{
		"own.lua", "org/gate", "quarto",
		map[string]any{"path": "_build/qm-capture.lua", "at": "post-finalize"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "own.lua"),
		filepath.Join(root, "_extensions/org/gate/gate.lua"),
		filepath.Join(root, "_extensions/org/gate/more.lua"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("filters = %v, want %v", got, want)
	}
	for name, entries := range map[string][]any{
		"entry point":   {map[string]any{"path": "own.lua", "at": "pre-quarto"}},
		"missing lua":   {"missing.lua"},
		"no extension":  {"org/nothing"},
		"non-lua":       {"org/bad"},
		"outside":       {"../x.lua"},
		"strange entry": {42},
	} {
		if _, err := projectFilters(root, entries); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPandocArgs(t *testing.T) {
	tool := &pandocTool{argv: []string{"pandoc"}, major: 3, minor: 10, tableAttributes: true}
	args := strings.Join(tool.args(pandocJob{
		Kind: "docx", Input: "in.md", Output: "out.docx", Metadata: "m.yaml", Filter: "f.lua",
		Reference: "ref.docx", TOC: true, Highlight: "arrow.theme",
	}), " ")
	for _, want := range []string{
		"-f markdown-table_attributes", "-t docx", "-o out.docx", "--metadata-file m.yaml", "--lua-filter f.lua",
		"--default-image-extension=png", "--reference-doc=ref.docx", "--toc", "--syntax-highlighting=arrow.theme",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q: %s", want, args)
		}
	}
	if !strings.HasSuffix(args, " in.md") {
		t.Errorf("the input must come last: %s", args)
	}
	if strings.Contains(args, "--slide-level") {
		t.Error("slide level set for a DOCX")
	}

	old := &pandocTool{argv: []string{"pandoc"}, major: 3, minor: 5}
	a := strings.Join(old.args(pandocJob{Kind: "pptx", SlideLevel: 2, Highlight: "kate"}), " ")
	for _, want := range []string{"-f markdown ", "--slide-level=2", "--highlight-style=kate"} {
		if !strings.Contains(a, want) {
			t.Errorf("older pandoc: args lack %q: %s", want, a)
		}
	}
	if strings.Contains(a, "--toc") || strings.Contains(a, "table_attributes") {
		t.Errorf("unexpected options: %s", a)
	}
}

func TestNewPandocPlan(t *testing.T) {
	root := pandocProject(t, map[string]string{"templates/deck_POL.pptx": ""})
	t.Setenv("QM_QUARTO_SHARE", t.TempDir()) // no Quarto themes: the default highlight style stays unset

	p, err := loadPlan(t, root, "book", "handout", "pol")
	if err != nil {
		t.Fatal(err)
	}
	if p.kind != "docx" || p.folder != "book" || p.stem != "book-pol" || p.title != "Book Polizei" || p.lang != "de" {
		t.Errorf("plan = %+v", p)
	}
	if p.outDir != filepath.Join(root, "_output", "handout") || !p.options.TOC || p.native == nil {
		t.Errorf("plan = %+v", p)
	}
	if p.make["pol"] != true || p.make["explanation"] != true {
		t.Errorf("make flags = %v", p.make)
	}
	if len(p.filters) != 1 || p.filters[0] != filepath.Join(root, "audience.lua") {
		t.Errorf("filters = %v", p.filters)
	}

	// A deck: named by qm: output-file, its template found through qm: copy.
	d, err := loadPlan(t, root, "book", "slides", "pol")
	if err != nil {
		t.Fatal(err)
	}
	if d.kind != "pptx" || d.stem != "book-pol" || d.reference != filepath.Join(root, "templates", "deck_POL.pptx") || d.options.SlideLevel != 2 || d.native != nil {
		t.Errorf("deck plan = %+v", d)
	}

	// Each way a format can be misconfigured is an error before any work.
	format := func(body string) {
		os.WriteFile(filepath.Join(root, "_quarto-format-handout.yml"), []byte(body), 0o644)
	}
	const head = "project:\n  type: book\n  output-dir: _output/handout\nqm:\n  renderer: pandoc\n" +
		"book:\n  output-file: out\n"
	for name, body := range map[string]string{
		"both formats":      head + "format:\n  docx: default\n  pptx: default\n",
		"pdf":               head + "format:\n  pdf: default\n",
		"html":              head + "format:\n  html: default\n",
		"no format":         head,
		"no output dir":     strings.Replace(head+"format:\n  docx: default\n", "  output-dir: _output/handout\n", "", 1),
		"no file name":      strings.Replace(head+"format:\n  docx: default\n", "book:\n  output-file: out\n", "", 1),
		"bad variable":      strings.Replace(head+"format:\n  docx: default\n", "output-file: out", `output-file: "{{< var nope >}}"`, 1),
		"missing reference": head + "format:\n  docx:\n    reference-doc: nope.docx\n",
	} {
		format(body)
		if _, err := loadPlan(t, root, "book", "handout", "std"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	format(head + "format:\n  docx: default\n")
	if _, err := loadPlan(t, root, "none", "handout", "std"); err == nil || !strings.Contains(err.Error(), "whole project") {
		t.Errorf("a whole-project topic: got %v", err)
	}
}

func TestPandocMetadata(t *testing.T) {
	book := &pandocPlan{kind: "docx", title: "Titel", lang: "de-DE", make: map[string]any{"pol": true},
		options: qmcore.FormatOptions{TOC: true}}
	m, err := book.metadata()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"title: Titel", "lang: de-DE", "toc-title: Inhaltsverzeichnis", "pol: true"} {
		if !strings.Contains(m, want) {
			t.Errorf("metadata lacks %q:\n%s", want, m)
		}
	}
	deck := &pandocPlan{kind: "pptx", title: "Deck", make: map[string]any{}, options: qmcore.FormatOptions{TOC: true}}
	if m, _ = deck.metadata(); strings.Contains(m, "toc-title") || strings.Contains(m, "lang:") || !strings.Contains(m, "title: Deck") {
		t.Errorf("deck metadata:\n%s", m)
	}
	if languageKey("fr") != "en" || languageKey("DE-at") != "de" {
		t.Error("languageKey")
	}
}

func TestResolveHighlight(t *testing.T) {
	root := t.TempDir()
	share := t.TempDir()
	t.Setenv("QM_QUARTO_SHARE", share)
	themes := filepath.Join(share, "pandoc", "highlight-styles")
	os.MkdirAll(themes, 0o755)
	os.WriteFile(filepath.Join(themes, "arrow-light.theme"), nil, 0o644)
	os.WriteFile(filepath.Join(themes, "github.theme"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "mine.theme"), nil, 0o644)

	for value, want := range map[string]string{
		"":           filepath.Join(themes, "arrow-light.theme"), // Quarto's own default
		"arrow":      filepath.Join(themes, "arrow-light.theme"),
		"github":     filepath.Join(themes, "github.theme"),
		"mine.theme": filepath.Join(root, "mine.theme"),
		"pygments":   "pygments", // one of Pandoc's own
	} {
		if got := resolveHighlight(root, value); got != want {
			t.Errorf("resolveHighlight(%q) = %q, want %q", value, got, want)
		}
	}
	t.Setenv("QM_QUARTO_SHARE", "")
	t.Setenv("PATH", t.TempDir()) // no Quarto to ask
	if got := resolveHighlight(root, ""); got != "" {
		t.Errorf("without Quarto the default is Pandoc's own, got %q", got)
	}
}

func TestFindPandoc(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub pandoc is a shell script")
	}
	stub := func(version string) string {
		p := filepath.Join(t.TempDir(), "pandoc")
		body := "#!/bin/sh\ncase \"$1\" in\n--version) echo \"pandoc " + version + "\";;\n--list-extensions=markdown) echo +smart; echo +table_attributes;;\nesac\n"
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := PandocCommand
	t.Cleanup(func() { PandocCommand = old })

	PandocCommand = stub("3.10")
	tool, err := findPandoc()
	if err != nil || tool.major != 3 || tool.minor != 10 || !tool.tableAttributes {
		t.Fatalf("got %+v, %v", tool, err)
	}
	if tool.highlightFlag() != "--syntax-highlighting" {
		t.Error("highlight flag for Pandoc 3.10")
	}
	PandocCommand = stub("2.19.2")
	if _, err := findPandoc(); err == nil || !strings.Contains(err.Error(), "3.0") {
		t.Errorf("an old Pandoc: got %v", err)
	}
	PandocCommand = filepath.Join(t.TempDir(), "missing")
	if _, err := findPandoc(); err == nil {
		t.Error("a missing Pandoc was found")
	}
}

// A dry run prints the Pandoc command and creates nothing: no output, no
// working files, no _build/.
func TestPandocRenderDryRun(t *testing.T) {
	root := pandocProject(t, nil)
	var log []string
	if err := renderSelection(root, sel("book", "handout", "pol"), true, func(f string, a ...any) {
		log = append(log, fmt.Sprintf(f, a...))
	}); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(log, "\n")
	for _, want := range []string{"pandoc renderer", "$ pandoc -f markdown", "-o _output/handout/book-pol.docx", "--toc", "cover.png"} {
		if !strings.Contains(all, want) {
			t.Errorf("dry run output lacks %q:\n%s", want, all)
		}
	}
	for _, path := range []string{"_output", "_build"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("a dry run created %s", path)
		}
	}
}

// zipText returns the text of every XML part of a zip, keyed by name.
func zipParts(t *testing.T, path string) map[string]string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	parts := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r)
		r.Close()
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
			parts[f.Name] = string(b)
		} else {
			parts[f.Name] = ""
		}
	}
	return parts
}

// The whole chain with a real Pandoc: the project's Quarto-style filter, the
// callouts, includes, page breaks, image paths, the DOCX, its PDF and cover,
// and a deck. Skipped where no Pandoc is installed.
func TestPandocRendererIntegration(t *testing.T) {
	if _, err := findPandoc(); err != nil {
		t.Skip(err)
	}
	t.Setenv("QM_QUARTO_SHARE", "")
	root := pandocProject(t, map[string]string{
		"book/index.qmd": "---\norder: 1\n---\n# Überblick\n\n" +
			"Sichtbar {{< var topic >}} [nur FW]{.fw} Rest.\n\n" +
			"::: private\nNUR-POLIZEI-TEXT\n:::\n\n" +
			"![Ein Bild](/assets/logo.png){width=20%}\n\n" +
			"::: {.callout-note}\nHinweistext erste Zeile.\n\nZweiter Absatz.\n:::\n\n" +
			"::: {.callout-warning title=\"Achtung\"}\nWarntext.\n:::\n\n" +
			"::: {.callout}\n## Kopf der grauen Box\n\nGraue Box.\n:::\n\n" +
			"{{< pagebreak >}}\n\n" +
			"{{< include /assets/colophon.md >}}\n\n" +
			"| A | B |\n|---|---|\n| eins | zwei |\n\n: Beschriftung\n\n" +
			"::: slide\n## Folie\n\n- Punkt\n\n::: {.callout-tip}\nFolientipp\n:::\n\n:::: notes\nSprechernotiz\n::::\n:::\n",
		"assets/colophon.md": "Kolophon-Text",
		// Icons the project brings along (Quarto's would do as well).
		"assets/callout-icons/note.png":    "",
		"assets/callout-icons/warning.png": "",
		"assets/callout-icons/tip.png":     "",
		"templates/deck_POL.pptx":          "",
	})
	writePNG(t, filepath.Join(root, "assets", "logo.png"))
	for _, k := range []string{"note", "warning", "tip"} {
		writePNG(t, filepath.Join(root, "assets", "callout-icons", k+".png"))
	}
	// A real PPTX template: Pandoc's own default, which it prints on stdout.
	pandoc, _ := findPandoc()
	ref, err := exec.Command(pandoc.argv[0], append(append([]string{}, pandoc.argv[1:]...), "--print-default-data-file=reference.pptx")...).Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "templates", "deck_POL.pptx"), ref, 0o644); err != nil {
		t.Fatal(err)
	}

	var log []string
	logf := func(f string, a ...any) { log = append(log, f) }
	if err := renderSelection(root, sel("book", "handout", "pol"), false, logf); err != nil {
		t.Fatalf("handout: %v\n%s", err, strings.Join(log, "\n"))
	}
	docx := filepath.Join(root, "_output/handout/book-pol.docx")
	parts := zipParts(t, docx)
	body := parts["word/document.xml"]
	for _, want := range []string{
		"Überblick", "Sichtbar", "Hinweistext erste Zeile", "Warntext", "Achtung", "Hinweis", // default German title
		"Kopf der grauen Box", "Graue Box", "Kolophon-Text", "Beschriftung", `w:type="page"`,
		`w:fill="dae6fb"`, `w:fill="fcefdc"`, `w:color="909090"`, // callout colors; the bare callout's gray bar
		`name="qm cover"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("DOCX lacks %q", want)
		}
	}
	// The project's filter ran with Quarto's metadata call: POL content stays, FW content goes.
	if !strings.Contains(body, "NUR-POLIZEI-TEXT") || strings.Contains(body, "nur FW") {
		t.Error("the audience filter did not apply")
	}
	if strings.Contains(body, "{{<") {
		t.Error("a shortcode was left in the text")
	}
	// Word merges tables that touch: the two callouts in a row need a paragraph between them.
	if compact := regexp.MustCompile(`>\s+<`).ReplaceAllString(body, "><"); strings.Contains(compact, "</w:tbl><w:tbl>") {
		t.Error("two callout tables touch; Word would merge them")
	}
	if _, ok := parts["word/media/logo.png"]; !ok && !hasMedia(parts) {
		t.Error("the image was not embedded (the /assets path was not rebased)")
	}
	if !strings.Contains(parts["word/settings.xml"]+body, "TOC") && !strings.Contains(body, "Inhaltsverzeichnis") {
		t.Error("no table of contents")
	}
	pdf, err := os.ReadFile(filepath.Join(root, "_output/handout/book-pol.pdf"))
	if err != nil || len(pdf) < 1000 || !strings.HasPrefix(string(pdf), "%PDF") {
		t.Fatalf("PDF missing or invalid: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "_build")); !os.IsNotExist(err) {
		t.Error("the Pandoc renderer wrote to _build/")
	}
	if entries, _ := filepath.Glob(filepath.Join(root, "_output/handout/.qm-*")); len(entries) != 0 {
		t.Errorf("temporary files left behind: %v", entries)
	}

	// The audience filter's other side.
	if err := renderSelection(root, sel("book", "handout", "std"), false, logf); err != nil {
		t.Fatal(err)
	}
	std := zipParts(t, filepath.Join(root, "_output/handout/book.docx"))["word/document.xml"]
	if strings.Contains(std, "NUR-POLIZEI-TEXT") {
		t.Error("POL-only content in the std handout")
	}

	// A deck: the title slide, the slide, the callout as a block quote, the notes.
	if err := renderSelection(root, sel("book", "slides", "pol"), false, logf); err != nil {
		t.Fatalf("slides: %v", err)
	}
	deck := zipParts(t, filepath.Join(root, "_output/slides/book-pol.pptx"))
	var text strings.Builder
	for name, part := range deck {
		if strings.HasPrefix(name, "ppt/slides/slide") || strings.HasPrefix(name, "ppt/notesSlides/") {
			text.WriteString(part)
		}
	}
	for _, want := range []string{"Folie", "Punkt", "Folientipp", "Tipp", "Sprechernotiz", "Book Polizei"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("deck lacks %q", want)
		}
	}
	if strings.Contains(text.String(), "NUR-POLIZEI-TEXT") {
		t.Error("book content in the deck")
	}
}

func hasMedia(parts map[string]string) bool {
	for name := range parts {
		if strings.HasPrefix(name, "word/media/") && !strings.Contains(name, "qm-cover") && !strings.Contains(name, "callout") {
			return true
		}
	}
	return false
}
