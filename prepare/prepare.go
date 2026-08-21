// Package prepare implements the `qm prepare` command, the project's
// Quarto pre-render hook.
//
// Spec: spec-prepare.yaml.
//
//	# _quarto.yml
//	project:
//	  pre-render: qm prepare
//
// It does the two things Quarto's configuration cannot:
//
//  1. It puts the selected topic into `_build/book.qmd` and
//     `_build/slides.qmd`. Quarto books are flat — one file per chapter, no
//     folder depth — so a multi-level content folder has to be flattened
//     before it can be a book at all. The build documents have fixed names,
//     which is what lets index.qmd hold a single, topic-independent include.
//
//  2. It copies the files a format needs under a fixed name because
//     Quarto's format-level keys do not resolve `{{< var >}}` — the PPTX
//     reference template, whose name depends on the format *and* the
//     audience.
//
// Before either, it validates the profile selection. This is the more
// important half: Quarto accepts a misspelled profile without a word (the
// missing file is ignored, and the variables it should have set resolve to
// the literal `?var:audience`, which lands in the output file name), and it
// accepts two profiles of the same group by merging them (their chapter
// lists concatenate). A failing pre-render aborts the render with a
// non-zero exit code, so these become errors instead of a wrong handout.
package prepare

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cboct/qm/internal/bookrender"
	"github.com/cboct/qm/internal/cli"
	"github.com/cboct/qm/internal/qmcore"
	"github.com/christophberger/start"
)

var (
	projectPath *string
	profileFlag *string
)

// Register wires the `prepare` command into start. It has no sub-commands.
// profileFlagPtr is the qm-wide --profile flag; `qm finalize` shares it.
func Register(projectFlag, profileFlagPtr *string) {
	projectPath = projectFlag
	profileFlag = profileFlagPtr
	start.Add(&start.Command{
		Name:  "prepare",
		Short: "Pre-render hook: validate the profile selection and build the topic",
		Long: "Validate the active topic/format/audience profiles, flatten the " +
			"selected topic into _build/book.qmd and _build/slides.qmd, and copy " +
			"the files the format profile asks for. Usage: qm prepare " +
			"[--profile <t>,<f>,<a>]. Without --profile the selection is read " +
			"from $QUARTO_PROFILE, which is how Quarto invokes it as a " +
			"project pre-render hook.",
		Flags: []string{"project", "profile"},
		Cmd:   cli.Guard(cmd),
	})
}

func cmd(c *start.Command) error {
	if projectPath == nil {
		return fmt.Errorf("prepare: --project flag not initialised")
	}
	root, err := filepath.Abs(*projectPath)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}
	arg := ""
	if profileFlag != nil {
		arg = *profileFlag
	}
	return Run(root, ActiveProfiles(arg), logf)
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stdout, "qm prepare: "+format+"\n", args...)
}

// ActiveProfiles returns the profile names to work from: the --profile
// argument if given, else $QUARTO_PROFILE.
//
// $QUARTO_PROFILE is the *effective* selection, not the one typed on the
// command line: Quarto has already appended the defaults of any profile
// group the user left out. That is what makes validating it worthwhile.
func ActiveProfiles(arg string) []string {
	if strings.TrimSpace(arg) == "" {
		arg = os.Getenv("QUARTO_PROFILE")
	}
	var out []string
	for _, p := range strings.Split(arg, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Run validates the selection and writes everything the render needs.
func Run(root string, profiles []string, log bookrender.Logf) error {
	if log == nil {
		log = func(string, ...any) {}
	}
	if len(profiles) == 0 {
		return fmt.Errorf("no profile selected; pass --profile or set QUARTO_PROFILE")
	}
	sel, err := qmcore.ParseSelection(profiles)
	if err != nil {
		return err
	}
	ps, err := qmcore.LoadSelection(root, sel)
	if err != nil {
		return err
	}
	if err := checkVariables(ps); err != nil {
		return err
	}

	folder := ps.Folder()
	if folder != "" {
		if err := checkFolder(root, ps, folder); err != nil {
			return err
		}
	}
	title, err := ps.Title()
	if err != nil {
		return err
	}
	log("%s", sel)
	if err := bookrender.WriteBuild(root, folder, title, log); err != nil {
		return err
	}
	return copyFiles(root, ps, log)
}

// checkVariables resolves every `{{< var >}}` the selection relies on, so
// that an undefined one stops the render here instead of turning into a
// file called `calltaker-?var:audience.docx`.
func checkVariables(ps *qmcore.Profiles) error {
	vars := ps.Vars()
	for _, t := range interpolated(ps) {
		if _, err := qmcore.ResolveVars(t.value, vars); err != nil {
			return fmt.Errorf("%s: %s: %w", t.path, t.key, err)
		}
	}
	return nil
}

type template struct{ path, key, value string }

// interpolated lists every configured value the selection interpolates:
// the two keys Quarto resolves variables in, plus qm's own.
func interpolated(ps *qmcore.Profiles) []template {
	var out []template
	add := func(p *qmcore.Profile, key, value string) {
		if p != nil && value != "" {
			out = append(out, template{p.Path, key, value})
		}
	}
	for _, p := range ps.All() {
		add(p, "book: title", p.Book.Title)
		add(p, "book: output-file", p.Book.OutputFile)
		add(p, "qm: output-file", p.QM.OutputFile)
		add(p, "qm: output-dir-suffix", p.QM.OutputDirSuffix)
		for target, source := range p.QM.Copy {
			add(p, "qm: copy: "+target, source)
		}
	}
	return out
}

// checkFolder makes sure the topic's content folder is really there. The
// name comes from the profile (`qm: folder:`, defaulting to the topic name)
// and a typo would otherwise produce an empty book.
func checkFolder(root string, ps *qmcore.Profiles, folder string) error {
	abs, err := qmcore.ProjectPath(root, folder)
	if err != nil {
		return fmt.Errorf("%s: qm: folder: %w", ps.Topic.Path, err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		have := bookrender.ContentFolders(root)
		sort.Strings(have)
		return fmt.Errorf(
			"%s: content folder %q does not exist; the project has: %s",
			ps.Topic.Path, folder, strings.Join(have, ", "))
	}
	return nil
}

// copyFiles carries out the format profile's `qm: copy:` map.
//
// The target is a fixed path a format-level key such as
// `format: pptx: reference-doc:` can point at. The source may vary with any
// axis, because it is resolved here rather than by Quarto.
func copyFiles(root string, ps *qmcore.Profiles, log bookrender.Logf) error {
	vars := ps.Vars()
	for _, p := range ps.All() {
		targets := make([]string, 0, len(p.QM.Copy))
		for t := range p.QM.Copy {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		for _, target := range targets {
			source, err := qmcore.ResolveVars(p.QM.Copy[target], vars)
			if err != nil {
				return fmt.Errorf("%s: qm: copy: %s: %w", p.Path, target, err)
			}
			if err := copyFile(root, source, target); err != nil {
				return fmt.Errorf("%s: qm: copy: %w", p.Path, err)
			}
			log("copied %s -> %s", source, target)
		}
	}
	return nil
}

func copyFile(root, source, target string) error {
	src, err := qmcore.ProjectPath(root, source)
	if err != nil {
		return err
	}
	dst, err := qmcore.ProjectPath(root, target)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
