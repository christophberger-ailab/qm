// Package render implements the `qm render` command.
//
// Spec: spec-render.yaml.
//
//	qm render                          -> every combination the project declares
//	qm render calltaker dispatcher     -> those two topics, all their formats
//	qm render --format handout         -> handouts only
//	qm render --audience pol --clean   -> the Polizei variants, from scratch
//
// One `quarto render --profile topic-<t>,format-<f>,audience-<a>` runs per
// combination. That command is the whole interface: it works from the CLI,
// from VS Code, and from CI without qm, and it produces correctly named
// output on its own, because the project's pre- and post-render hooks
// (`qm prepare`, `qm finalize`) do the parts Quarto's configuration cannot
// express. `qm render` only walks the matrix.
package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/christophberger-ailab/qm/internal/bookrender"
	"github.com/christophberger-ailab/qm/internal/cli"
	"github.com/christophberger-ailab/qm/internal/qmcore"
	"github.com/christophberger/start"
	flag "github.com/spf13/pflag"
)

var (
	projectPath  *string
	topicFlag    *string
	formatFlag   *string
	audienceFlag *string
	cleanFlag    *bool
	dryRunFlag   *bool
)

// Register wires the `render` command into start. `render` has no
// sub-commands, so — as with `lint` — it is registered as a leaf.
func Register(projectFlag *string) {
	projectPath = projectFlag
	topicFlag = flag.String("topic", "",
		"Comma-separated topics to render (default: all)")
	formatFlag = flag.String("format", "",
		"Comma-separated output formats to render (default: all)")
	audienceFlag = flag.String("audience", "",
		"Comma-separated audiences to render (default: all)")
	cleanFlag = flag.Bool("clean", false,
		"Empty the output directories of the selected formats first")
	dryRunFlag = flag.Bool("dry-run", false,
		"Print the quarto invocations without running them")

	start.Add(&start.Command{
		Name:  "render",
		Short: "Render the project's topic/format/audience matrix",
		Long: "Render every topic × format × audience combination the project " +
			"declares, or the subset selected by the flags. Usage: " +
			"qm render [<topic>...]. Each combination is one " +
			"`quarto render --profile topic-<t>,format-<f>,audience-<a>`. " +
			"Which formats and audiences a topic takes part in is declared in " +
			"its own profile, under `qm: formats:` and `qm: audiences:`.",
		Flags: []string{"project", "topic", "format", "audience", "clean", "dry-run"},
		Cmd:   cli.Guard(cmd),
	})
}

func cmd(c *start.Command) error {
	if projectPath == nil {
		return fmt.Errorf("render: --project flag not initialised")
	}
	docPath, err := filepath.Abs(*projectPath)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}
	// Positional arguments name topics, the axis one usually varies.
	topics := append(splitList(deref(topicFlag)), c.Args...)
	req := qmcore.Matrix{
		Topics:    topics,
		Formats:   splitList(deref(formatFlag)),
		Audiences: splitList(deref(audienceFlag)),
	}
	return Run(docPath, req, derefBool(cleanFlag), derefBool(dryRunFlag))
}

// Run renders the requested part of the project's matrix. Progress is
// written to stdout.
func Run(docPath string, req qmcore.Matrix, clean, dryRun bool) error {
	opts, err := BuildOptions(docPath, req, clean, dryRun)
	if err != nil {
		return err
	}
	log := func(format string, args ...any) {
		fmt.Fprintf(os.Stdout, format+"\n", args...)
	}
	log("%d render(s) selected", len(opts.Selections))
	if err := bookrender.Run(opts, log); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	return nil
}

// BuildOptions turns the command line into a render request. Exposed for
// tests. docPath must be absolute.
func BuildOptions(docPath string, req qmcore.Matrix, clean, dryRun bool) (bookrender.Options, error) {
	sels, err := qmcore.BuildMatrix(docPath, req)
	if err != nil {
		return bookrender.Options{}, fmt.Errorf("render: %w", err)
	}
	return bookrender.Options{
		Root:       docPath,
		Selections: sels,
		Clean:      clean,
		DryRun:     dryRun,
	}, nil
}

// splitList parses a comma-separated flag value, discarding empty entries so
// that `--format ""` or a trailing comma does not produce phantom items.
func splitList(arg string) []string {
	var out []string
	for _, p := range strings.Split(arg, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefBool(b *bool) bool { return b != nil && *b }
