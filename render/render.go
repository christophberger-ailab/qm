// Package render implements the `qm render` command.
//
// Spec: spec-render.yaml.
//
//	qm render                     -> render every book folder of the project
//	qm render a b                 -> render the book folders a and b
//	qm render --profile x,y a     -> render book a once per profile
//	qm render --to pdf a          -> render book a to PDF only
//	qm render --slides a          -> also render the deck of a's slide blocks
//
// The work itself is the render flow merged in from quarto-sorter (see
// internal/bookrender): each book folder is flattened into a single .qmd
// document at the project root and handed to `quarto render`. The `qm web`
// UI drives the same flow, so both produce identical output; this command
// streams the progress to stdout instead of a browser panel.
package render

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cboct/qm/internal/bookrender"
	"github.com/cboct/qm/internal/project"
	"github.com/christophberger/start"
	flag "github.com/spf13/pflag"
)

var (
	projectPath *string
	profileFlag *string
	formatFlag  *string
	slidesFlag  *bool
)

// Register wires the `render` command into start. `render` has no
// sub-commands, so — as with `lint` — it is registered as a leaf.
func Register(projectFlag *string) {
	projectPath = projectFlag
	profileFlag = flag.String("profile", "",
		"Comma-separated Quarto profiles to render each book with "+
			"(default: the profiles named after the book)")
	formatFlag = flag.String("to", strings.Join(DefaultFormats, ","),
		"Comma-separated Quarto output formats for the book")
	slidesFlag = flag.Bool("slides", false,
		"Also render the deck built from the pages' ::: slide blocks")

	start.Add(&start.Command{
		Name:  "render",
		Short: "Render the book folders of the project",
		Long: "Flatten each book folder into a single document and render it " +
			"with Quarto. Usage: qm render [<book>...]. Without a book name, " +
			"every book folder of the project is rendered. A book is rendered " +
			"once per profile; without --profile, the profiles named after the " +
			"book (`<book>` and `<book>-*`) are used.",
		Flags: []string{"project", "profile", "to", "slides"},
		Cmd:   cmd,
	})
}

// DefaultFormats are the book output formats used when --to is not given.
// They match the formats the `qm web` render panel offers.
var DefaultFormats = []string{"pdf", "docx"}

func cmd(c *start.Command) error {
	if projectPath == nil {
		return fmt.Errorf("render: --project flag not initialised")
	}
	docPath, err := filepath.Abs(*projectPath)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}
	formats := DefaultFormats
	if formatFlag != nil {
		formats = splitList(*formatFlag)
	}
	var profiles []string
	if profileFlag != nil {
		profiles = splitList(*profileFlag)
	}
	slides := slidesFlag != nil && *slidesFlag
	return Run(docPath, c.Args, profiles, formats, slides)
}

// Run renders the named book folders of the project at docPath. With no
// book named, every book folder is rendered. profiles selects the Quarto
// profiles every book is rendered with; when empty, each book falls back to
// the profiles named after it. Progress is written to stdout.
func Run(docPath string, books, profiles, formats []string, slides bool) error {
	opts, err := BuildOptions(docPath, books, profiles, formats, slides)
	if err != nil {
		return err
	}
	log := func(format string, args ...any) {
		fmt.Fprintf(os.Stdout, format+"\n", args...)
	}
	if err := bookrender.Run(opts, log); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	return nil
}

// BuildOptions turns the command line into a render request. Exposed for
// tests. docPath must be absolute.
func BuildOptions(docPath string, books, profiles, formats []string, slides bool) (bookrender.Options, error) {
	available := bookrender.Books(docPath)
	if len(available) == 0 {
		return bookrender.Options{}, fmt.Errorf(
			"render: %s holds no book folders", docPath)
	}
	if len(books) == 0 {
		books = available
	}
	for _, b := range books {
		if !slices.Contains(available, b) {
			return bookrender.Options{}, fmt.Errorf(
				"render: %q is not a book folder of %s (have: %s)",
				b, docPath, strings.Join(available, ", "))
		}
	}
	if len(formats) == 0 && !slides {
		return bookrender.Options{}, fmt.Errorf(
			"render: no output format left; pass --to or --slides")
	}

	opts := bookrender.Options{
		Root:     docPath,
		Books:    books,
		Profiles: map[string][]string{},
		Formats:  formats,
		Slides:   slides,
	}
	// Profiles named on the command line apply to every book; otherwise each
	// book gets the profiles named after it, which is what the web UI offers
	// as its default selection too.
	if len(profiles) > 0 {
		for _, b := range books {
			opts.Profiles[b] = profiles
		}
		return opts, nil
	}
	declared, err := project.Profiles(docPath)
	if err != nil {
		return bookrender.Options{}, fmt.Errorf("render: cannot list profiles: %w", err)
	}
	for _, b := range books {
		opts.Profiles[b] = bookrender.DefaultProfiles(b, declared)
	}
	return opts, nil
}

// splitList parses a comma-separated flag value, discarding empty entries so
// that `--to ""` or a trailing comma does not produce phantom items.
func splitList(arg string) []string {
	var out []string
	for _, p := range strings.Split(arg, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
