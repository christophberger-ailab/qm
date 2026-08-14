// Package bookrender renders the book folders of a Quarto website.
//
// A book folder is flattened into a single .qmd document (see
// internal/bookmaker) that is written to the project root and handed to
// `quarto render`. Both the `qm render` command and the `qm web` UI drive
// their renders through this package, so they behave identically; they
// differ only in where the progress lines go.
//
// The package is the render flow of quarto-sorter
// (https://github.com/christophberger-ailab/quarto-sorter), lifted out of
// that tool's server so it can be used from the command line as well.
package bookrender

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cboct/qm/internal/bookmaker"
)

// buildPrefix and slidesPrefix name the flat documents Quarto renders.
//
// The leading underscore is what tells Quarto the file is not project
// content: without it a website build renders every flat book a second time
// as a stray top-level page and `contents: auto` lists it in the sidebar. It
// also keeps the file out of the sorter's own page tree. The book document
// is not meant to render on its own anyway — the project's top-level
// index.qmd pulls it in with `{{< include >}}`. The slide deck has no such
// host page and is named on the command line, which an
// underscore-prefixed file still renders from.
const (
	buildPrefix  = "_book-build-"
	slidesPrefix = "_slides-build-"
)

// slidesFormat is what a deck is rendered to; the book's PDF/DOCX formats
// make no sense for it.
const slidesFormat = "revealjs"

// Options is one render request.
type Options struct {
	// Root is the project root; the flat documents are written there so
	// that website-absolute media paths such as /assets/x.png resolve.
	Root string
	// Books are the book folder names to render.
	Books []string
	// Profiles maps a book folder name to the Quarto profiles it is
	// rendered with. By default that is one render run per profile, since
	// the profiles named after a book (…-fw, …-pol) select alternative
	// variants of it rather than combining into one document. A book with
	// no profile is rendered once without --profile.
	Profiles map[string][]string
	// CombineProfiles hands a book's profiles to Quarto as one composed set
	// (`--profile a,b`) in a single run instead of running once per profile.
	// That is what an explicitly named profile list means: the profiles
	// contribute different parts of one configuration — the variant selects
	// the chapters, another profile the output format — and dropping any of
	// them leaves Quarto with an incomplete book.
	CombineProfiles bool
	// Formats are the Quarto output formats for the book, e.g. pdf, docx.
	Formats []string
	// Slides also renders the deck built from the pages' ::: slide blocks.
	Slides bool
}

// Logf receives the render's progress, one line per call, in the format of
// fmt.Printf. A nil Logf discards the output.
type Logf func(format string, args ...any)

// Run renders every selected book, carrying on after a failure so that one
// broken book does not hide the others. The returned error reports how many
// books failed; what went wrong has been passed to log by then.
func Run(o Options, log Logf) error {
	if log == nil {
		log = func(string, ...any) {}
	}
	failed := 0
	for _, book := range o.Books {
		if err := renderBook(o, book, log); err != nil {
			log("%s: %v", book, err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d book(s) failed to render", failed, len(o.Books))
	}
	log("done: %d book(s) rendered", len(o.Books))
	return nil
}

// renderBook flattens one book folder and feeds the result to Quarto.
//
// The flat documents are temporary: Quarto has no way to read a document
// from standard input, and it resolves media paths against the file's own
// location, so the book has to exist as a file at the project root for the
// duration of the render and is removed afterwards.
//
// The book itself is not named on the Quarto command line: the project is
// rendered as a whole, and the top-level index.qmd includes the flat
// document. That leaves the chapter list to the profile configuration,
// which is where a book project keeps it.
func renderBook(o Options, book string, log Logf) error {
	res, err := FlattenBook(o.Root, book)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		log("%s: warning: %s", book, w)
	}
	log("%s: %d pages, %d slides, %d links rewritten",
		book, res.Pages, res.SlideBlocks, res.Links)

	bookFile := filepath.Join(o.Root, buildPrefix+book+".qmd")
	if err := os.WriteFile(bookFile, []byte(res.Content), 0o644); err != nil {
		return err
	}
	defer remove(bookFile, log)

	slidesFile := ""
	switch {
	case !o.Slides:
	case res.Slides == "":
		log("%s: no ::: slide blocks; no deck rendered", book)
	default:
		slidesFile = filepath.Join(o.Root, slidesPrefix+book+".qmd")
		if err := os.WriteFile(slidesFile, []byte(res.Slides), 0o644); err != nil {
			return err
		}
		defer remove(slidesFile, log)
	}

	var failed bool
	for _, profiles := range o.runs(book) {
		for _, format := range o.Formats {
			if err := quarto(o.Root, "", format, profiles, log); err != nil {
				log("%s: %v", book, err)
				failed = true
			}
		}
		if slidesFile != "" {
			if err := quarto(o.Root, slidesFile, slidesFormat, profiles, log); err != nil {
				log("%s: %v", book, err)
				failed = true
			}
		}
	}
	if failed {
		return fmt.Errorf("one or more render runs failed")
	}
	return nil
}

// runs returns the profile sets a book is rendered with, one set per render
// run. Without CombineProfiles every profile is a run of its own; with it,
// all profiles go into a single run. Either way a book with no profile
// still renders once, with the project's default configuration.
func (o Options) runs(book string) [][]string {
	profiles := o.Profiles[book]
	if len(profiles) == 0 {
		return [][]string{nil}
	}
	if o.CombineProfiles {
		return [][]string{profiles}
	}
	out := make([][]string, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, []string{p})
	}
	return out
}

// FlattenBook turns a book folder into the flat book and slide documents.
func FlattenBook(root, book string) (*bookmaker.Result, error) {
	dir := filepath.Join(root, book)
	tree, err := bookmaker.LoadTree(dir, root)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, fmt.Errorf("%s holds no .qmd files", book)
	}
	return bookmaker.Flatten(tree, bookmaker.Options{
		ProjectRoot:   root,
		RewriteLinks:  true,
		WrapAudience:  true,
		ExtractSlides: true,
	})
}

// QuartoCommand is the executable that renders; tests and unusual
// installations replace it.
var QuartoCommand = "quarto"

// quarto runs one `quarto render` and streams its output into the log. An
// empty input renders the project rather than a single document.
//
// All profiles of the run go into one --profile argument, as Quarto's own
// comma-separated list, so that none of them is lost: leave one out and the
// configuration it holds — the chapter list, say — is missing from the
// render.
//
// --no-clean keeps Quarto from emptying the output directory before each
// run: the books, profiles, and formats are rendered one after another into
// the same directory, so cleaning would throw away what the previous runs
// just produced. No --output or --output-dir is passed either, leaving the
// output paths and file names to the profile configuration.
func quarto(root, input, format string, profiles []string, log Logf) error {
	args := []string{"render"}
	if input != "" {
		args = append(args, filepath.Base(input))
	}
	args = append(args, "--to", format, "--no-clean")
	if len(profiles) > 0 {
		args = append(args, "--profile", strings.Join(profiles, ","))
	}
	log("$ %s %s", QuartoCommand, strings.Join(args, " "))

	cmd := exec.Command(QuartoCommand, args...)
	cmd.Dir = root // profiles and project config are found from here
	w := &lineWriter{log: log}
	cmd.Stdout, cmd.Stderr = w, w

	err := cmd.Run()
	w.flush()
	if err != nil {
		what := "the project"
		if input != "" {
			what = filepath.Base(input)
		}
		return fmt.Errorf("%s --to %s: %w", what, format, err)
	}
	return nil
}

// remove deletes a generated build file, reporting a failure into the log
// rather than to the caller: the render itself already succeeded.
func remove(file string, log Logf) {
	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		log("could not remove %s: %v", filepath.Base(file), err)
	}
}

// lineWriter feeds a command's output into the log line by line.
type lineWriter struct {
	log Logf
	buf bytes.Buffer
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// Keep the partial line for the next write.
			w.buf.Reset()
			w.buf.WriteString(line)
			return len(p), nil
		}
		w.log("%s", strings.TrimRight(line, "\r\n"))
	}
}

// flush emits whatever the command left without a trailing newline.
func (w *lineWriter) flush() {
	if rest := strings.TrimRight(w.buf.String(), "\r\n"); rest != "" {
		w.log("%s", rest)
	}
	w.buf.Reset()
}

// Books lists the project's book folders by name: the first-level folders
// that hold Quarto content. Media folders, dot/underscore entries, and
// folders with a Quarto project config of their own are not books.
func Books(root string) []string {
	dirs, err := bookmaker.BookFolders(root)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(dirs))
	for _, d := range dirs {
		names = append(names, filepath.Base(d))
	}
	return names
}

// DefaultProfiles returns the profiles that belong to a book by name:
// `dispatcher` and `dispatcher-fw` both belong to the `dispatcher` folder.
// It is what a book is rendered with when nothing was selected for it.
func DefaultProfiles(book string, available []string) []string {
	var out []string
	for _, a := range available {
		if a == book || strings.HasPrefix(a, book+"-") {
			out = append(out, a)
		}
	}
	return out
}
