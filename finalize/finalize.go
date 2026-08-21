// Package finalize implements the `qm finalize` command, the project's
// Quarto post-render hook.
//
// Spec: spec-finalize.yaml.
//
//	# _quarto.yml
//	project:
//	  post-render: qm finalize
//
// It puts the two output paths in place that Quarto cannot compose itself.
//
// Quarto resolves `{{< var >}}` in exactly two configuration keys,
// `book: output-file` and `book: title`. That is enough for a book: its file
// name is composed from the topic's and the audience's variables, and Quarto
// writes it to the right place unaided. It is not enough for the other two
// cases:
//
//   - `project: output-dir` does not interpolate — it would create a
//     directory literally named `{{< var audience >}}`. The website's output
//     directory, however, depends on the audience (`_output/site-pol`). So
//     the format profile names a fixed directory and declares
//     `qm: output-dir-suffix:`, and the directory is renamed here.
//
//   - A slide deck is not a book. Book projects reject pptx, so the slides
//     format is a `type: default` project rendering `_build/slides.qmd`,
//     which lands in `<output-dir>/_build/slides.pptx` — and a non-book
//     project has no interpolating key to name it with. The format profile
//     declares `qm: output-file:` and the deck is renamed here.
//
// Everything is driven by the environment Quarto sets for a post-render
// hook: QUARTO_PROFILE, QUARTO_PROJECT_OUTPUT_DIR and
// QUARTO_PROJECT_OUTPUT_FILES.
package finalize

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cboct/qm/internal/bookrender"
	"github.com/cboct/qm/internal/cli"
	"github.com/cboct/qm/internal/qmcore"
	"github.com/cboct/qm/prepare"
	"github.com/christophberger/start"
	flag "github.com/spf13/pflag"
)

var (
	projectPath *string
	profileFlag *string
	outputDir   *string
	outputFiles *string
)

// Register wires the `finalize` command into start. It has no sub-commands.
// profileFlagPtr is the qm-wide --profile flag, shared with `qm prepare`.
func Register(projectFlag, profileFlagPtr *string) {
	projectPath = projectFlag
	profileFlag = profileFlagPtr
	outputDir = flag.String("output-dir", "",
		"Directory Quarto wrote to (default: $QUARTO_PROJECT_OUTPUT_DIR)")
	outputFiles = flag.String("output-files", "",
		"Newline- or comma-separated files Quarto produced "+
			"(default: $QUARTO_PROJECT_OUTPUT_FILES)")

	start.Add(&start.Command{
		Name:  "finalize",
		Short: "Post-render hook: move the output where the profiles say it belongs",
		Long: "Apply the active format profile's `qm: output-file:` and " +
			"`qm: output-dir-suffix:` to what Quarto just produced. Usage: " +
			"qm finalize [--profile <t>,<f>,<a>]. Without flags everything is " +
			"read from the environment Quarto sets for a project post-render " +
			"hook.",
		Flags: []string{"project", "profile", "output-dir", "output-files"},
		Cmd:   cli.Guard(cmd),
	})
}

func cmd(c *start.Command) error {
	if projectPath == nil {
		return fmt.Errorf("finalize: --project flag not initialised")
	}
	root, err := filepath.Abs(*projectPath)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}
	arg := ""
	if profileFlag != nil {
		arg = *profileFlag
	}
	return Run(root, prepare.ActiveProfiles(arg), envOr(outputDir, "QUARTO_PROJECT_OUTPUT_DIR"),
		splitFiles(envOr(outputFiles, "QUARTO_PROJECT_OUTPUT_FILES")), logf)
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stdout, "qm finalize: "+format+"\n", args...)
}

func envOr(flagValue *string, env string) string {
	if flagValue != nil && strings.TrimSpace(*flagValue) != "" {
		return *flagValue
	}
	return os.Getenv(env)
}

// splitFiles parses QUARTO_PROJECT_OUTPUT_FILES, which lists one
// project-relative path per line. Commas are accepted too, for the flag.
func splitFiles(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	}) {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Run applies the format profile's output rules to a finished render.
func Run(root string, profiles []string, outDir string, files []string, log bookrender.Logf) error {
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
	vars := ps.Vars()

	stem, err := qmcore.ResolveVars(ps.Format.QM.OutputFile, vars)
	if err != nil {
		return fmt.Errorf("%s: qm: output-file: %w", ps.Format.Path, err)
	}
	suffix, err := qmcore.ResolveVars(ps.Format.QM.OutputDirSuffix, vars)
	if err != nil {
		return fmt.Errorf("%s: qm: output-dir-suffix: %w", ps.Format.Path, err)
	}
	if stem == "" && suffix == "" {
		return nil // the format needs no fixing up; a book usually does not
	}

	dir, err := resolveOutputDir(root, outDir, ps)
	if err != nil {
		return err
	}
	if stem != "" {
		if err := renameOutputs(root, dir, stem, files, log); err != nil {
			return err
		}
	}
	if suffix != "" {
		if err := renameDir(dir, dir+suffix, log); err != nil {
			return err
		}
	}
	return nil
}

// resolveOutputDir returns the absolute output directory, preferring what
// Quarto reported and falling back to the format profile's own
// `project: output-dir` (for a `qm finalize` run by hand).
func resolveOutputDir(root, reported string, ps *qmcore.Profiles) (string, error) {
	if reported != "" {
		if filepath.IsAbs(reported) {
			return filepath.Clean(reported), nil
		}
		return qmcore.ProjectPath(root, reported)
	}
	rel, err := qmcore.FormatOutputDir(ps.Format)
	if err != nil {
		return "", err
	}
	if rel == "" {
		return "", fmt.Errorf(
			"%s declares no project: output-dir:, and QUARTO_PROJECT_OUTPUT_DIR "+
				"is not set", ps.Format.Path)
	}
	return qmcore.ProjectPath(root, rel)
}

// renameOutputs moves every produced file to `<output-dir>/<stem><ext>`.
//
// The move is to the top of the output directory on purpose: a `type:
// default` project mirrors the source layout, so the slide deck built from
// `_build/slides.qmd` arrives at `<output-dir>/_build/slides.pptx`. The
// source directories it leaves behind are removed if they end up empty.
func renameOutputs(root, dir, stem string, files []string, log bookrender.Logf) error {
	if len(files) == 0 {
		return fmt.Errorf("qm: output-file: is set but Quarto reported no output files")
	}
	for _, rel := range files {
		src, err := qmcore.ProjectPath(root, rel)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, stem+filepath.Ext(src))
		if src == dst {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("cannot move %s: %w", rel, err)
		}
		log("%s -> %s", rel, mustRel(root, dst))
		pruneEmpty(filepath.Dir(src), dir)
	}
	return nil
}

// renameDir moves the output directory to its audience-specific name,
// replacing whatever an earlier render left there.
func renameDir(from, to string, log bookrender.Logf) error {
	if from == to {
		return nil
	}
	if _, err := os.Stat(from); err != nil {
		return fmt.Errorf("cannot rename output directory: %w", err)
	}
	if err := os.RemoveAll(to); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("cannot rename %s to %s: %w", from, to, err)
	}
	log("%s/ -> %s/", filepath.Base(from), filepath.Base(to))
	return nil
}

// pruneEmpty removes now-empty directories left behind by a move, walking
// up to (but not including) stop.
func pruneEmpty(dir, stop string) {
	for dir != stop && strings.HasPrefix(dir, stop) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func mustRel(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(rel)
	}
	return p
}
