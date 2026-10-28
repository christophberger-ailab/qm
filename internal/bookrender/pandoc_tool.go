package bookrender

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// PandocCommand is the Pandoc executable the Pandoc renderer runs. Empty
// means: find one (see findPandoc). Tests and unusual installations replace
// it, the way they replace QuartoCommand.
var PandocCommand = ""

// minPandoc is the oldest Pandoc the filter chain works with: it relies on
// Lua filters over lists of blocks and on `pandoc.read` / `pandoc.write`.
const minPandocMajor = 3

// pandocTool is a Pandoc that is known to run.
type pandocTool struct {
	// argv is the command and any arguments that always come first.
	argv         []string
	major, minor int
	// tableAttributes is whether the Markdown reader has the
	// `table_attributes` extension. Quarto switches it off, and so does qm.
	tableAttributes bool
}

// findPandoc looks for Pandoc: $QM_PANDOC, one on the PATH, the Pandoc that
// ships inside a Quarto installation, and last `quarto pandoc`. A Quarto
// install is how Pandoc reaches most machines that build these documents,
// since the website still needs Quarto.
func findPandoc() (*pandocTool, error) {
	var candidates [][]string
	if PandocCommand != "" {
		candidates = append(candidates, []string{PandocCommand})
	} else {
		if env := os.Getenv("QM_PANDOC"); env != "" {
			candidates = append(candidates, []string{env})
		}
		if p, err := exec.LookPath("pandoc"); err == nil {
			candidates = append(candidates, []string{p})
		}
		if bin := quartoBinDir(); bin != "" {
			exe := "pandoc"
			if runtime.GOOS == "windows" {
				exe += ".exe"
			}
			if found, _ := filepath.Glob(filepath.Join(bin, "tools", "*", exe)); len(found) > 0 {
				candidates = append(candidates, []string{found[0]})
			}
		}
		if q, err := exec.LookPath(QuartoCommand); err == nil {
			candidates = append(candidates, []string{q, "pandoc"})
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no Pandoc found: install Pandoc (https://pandoc.org) or Quarto, " +
			"or point QM_PANDOC at a pandoc executable")
	}
	var lastErr error
	for _, argv := range candidates {
		t, err := probePandoc(argv)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

func probePandoc(argv []string) (*pandocTool, error) {
	out, err := exec.Command(argv[0], append(append([]string{}, argv[1:]...), "--version")...).Output()
	if err != nil {
		return nil, fmt.Errorf("%s --version: %w", strings.Join(argv, " "), err)
	}
	first, _, _ := strings.Cut(string(out), "\n")
	m := versionRe.FindStringSubmatch(first)
	if m == nil {
		return nil, fmt.Errorf("%s: cannot read a version from %q", strings.Join(argv, " "), first)
	}
	t := &pandocTool{argv: argv}
	t.major, _ = strconv.Atoi(m[1])
	t.minor, _ = strconv.Atoi(m[2])
	if t.major < minPandocMajor {
		return nil, fmt.Errorf("%s is Pandoc %d.%d; the Pandoc renderer needs %d.0 or newer",
			strings.Join(argv, " "), t.major, t.minor, minPandocMajor)
	}
	if exts, err := exec.Command(argv[0], append(append([]string{}, argv[1:]...), "--list-extensions=markdown")...).Output(); err == nil {
		t.tableAttributes = bytes.Contains(exts, []byte("table_attributes"))
	}
	return t, nil
}

// quartoBinDir and quartoShareDir locate a Quarto installation, for its
// bundled Pandoc and for the callout icons and highlight themes that ship
// with it. QUARTO_SHARE_PATH is the variable Quarto itself sets while it
// runs a hook. Both are "" without Quarto.
func quartoBinDir() string   { return quartoPath(0) }
func quartoShareDir() string { return quartoPath(1) }

func quartoPath(which int) string {
	if which == 1 {
		for _, env := range []string{"QM_QUARTO_SHARE", "QUARTO_SHARE_PATH"} {
			if v := os.Getenv(env); v != "" {
				return v
			}
		}
	}
	q, err := exec.LookPath(QuartoCommand)
	if err != nil {
		return ""
	}
	out, err := exec.Command(q, "--paths").Output()
	if err != nil {
		return ""
	}
	// One path per line; a path may contain spaces (C:\Program Files\Quarto).
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) <= which {
		return ""
	}
	return lines[which]
}

// highlightFlag is the option that selects a code highlighting style.
// Pandoc 3.8 renamed it.
func (t *pandocTool) highlightFlag() string {
	if t.major > 3 || (t.major == 3 && t.minor >= 8) {
		return "--syntax-highlighting"
	}
	return "--highlight-style"
}

// pandocJob is one Pandoc run.
type pandocJob struct {
	// Kind is the output format: "docx" or "pptx".
	Kind string
	// Input, Output, Metadata and Filter are file paths.
	Input, Output, Metadata, Filter string
	// Reference is the DOCX/PPTX template, or "".
	Reference string
	TOC       bool
	// SlideLevel is the heading level that starts a slide (pptx); 0 leaves
	// Pandoc's own choice.
	SlideLevel int
	// Highlight is a highlight style name or theme file, or "".
	Highlight string
}

// args builds Pandoc's argument list for the job (without the command).
//
// The reader is Pandoc's own Markdown with Quarto's one deviation, which is
// what makes the content parse exactly as it does under Quarto: Quarto
// turns `table_attributes` off.
func (t *pandocTool) args(j pandocJob) []string {
	reader := "markdown"
	if t.tableAttributes {
		reader += "-table_attributes"
	}
	a := []string{
		"-f", reader,
		"-t", j.Kind,
		"-o", j.Output,
		"--metadata-file", j.Metadata,
		"--lua-filter", j.Filter,
		"--default-image-extension=png",
	}
	if j.Reference != "" {
		a = append(a, "--reference-doc="+j.Reference)
	}
	if j.TOC {
		a = append(a, "--toc")
	}
	if j.SlideLevel > 0 {
		a = append(a, "--slide-level="+strconv.Itoa(j.SlideLevel))
	}
	if j.Highlight != "" {
		a = append(a, t.highlightFlag()+"="+j.Highlight)
	}
	return append(a, j.Input)
}

// run executes Pandoc in dir (the project root, which is what makes
// project-relative image paths resolve) and feeds its messages to log. The
// error carries Pandoc's last lines, because a log line is easy to miss.
func (t *pandocTool) run(ctx context.Context, dir string, args []string, log Logf) error {
	cmd := exec.CommandContext(ctx, t.argv[0], append(append([]string{}, t.argv[1:]...), args...)...)
	cmd.Dir = dir
	tail := &tailWriter{max: 8}
	w := &lineWriter{log: log}
	cmd.Stdout, cmd.Stderr = w, teeWriter{w, tail}
	err := cmd.Run()
	w.flush()
	if err != nil {
		msg := strings.TrimSpace(tail.String())
		if msg != "" {
			return fmt.Errorf("pandoc: %w\n%s", err, msg)
		}
		return fmt.Errorf("pandoc: %w", err)
	}
	return nil
}

// teeWriter writes to two writers.
type teeWriter struct {
	a, b interface{ Write([]byte) (int, error) }
}

func (t teeWriter) Write(p []byte) (int, error) {
	t.a.Write(p)
	t.b.Write(p)
	return len(p), nil
}

// tailWriter remembers the last max lines written to it.
type tailWriter struct {
	max   int
	lines []string
	part  string
}

func (w *tailWriter) Write(p []byte) (int, error) {
	text := w.part + string(p)
	parts := strings.Split(text, "\n")
	w.part = parts[len(parts)-1]
	for _, l := range parts[:len(parts)-1] {
		w.lines = append(w.lines, strings.TrimRight(l, "\r"))
	}
	if len(w.lines) > w.max {
		w.lines = w.lines[len(w.lines)-w.max:]
	}
	return len(p), nil
}

func (w *tailWriter) String() string {
	lines := w.lines
	if w.part != "" {
		lines = append(append([]string{}, lines...), w.part)
	}
	return strings.Join(lines, "\n")
}
