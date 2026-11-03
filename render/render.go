// Package render implements the `qm render` command.
//
// Spec: spec-render.yaml.
//
//	qm render                          -> every combination the project declares
//	qm render calltaker dispatcher     -> those two topics, all their formats
//	qm render --format handout         -> handouts only
//	qm render --audience pol --clean   -> the Polizei variants, from scratch
//
// The matrix is the one qm.yml declares (see internal/config); every
// combination is built by qm itself, without Quarto or Pandoc (see
// internal/build).
package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/christophberger-ailab/qm/internal/build"
	"github.com/christophberger-ailab/qm/internal/cli"
	"github.com/christophberger-ailab/qm/internal/config"
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
		"Print the render pipeline without running it")

	start.Add(&start.Command{
		Name:  "render",
		Short: "Render the project's topic/format/audience matrix",
		Long: "Render every topic × format × audience combination the project " +
			"declares in qm.yml, or the subset selected by the flags. Usage: " +
			"qm render [<topic>...]. A website covers all selected topics at " +
			"once; a document (DOCX, PDF) is written per topic and audience. " +
			"Which formats and audiences a topic takes part in is declared " +
			"under the topic in qm.yml, as `formats:` and `audiences:`.",
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
	req := config.Request{
		Topics:    topics,
		Formats:   splitList(deref(formatFlag)),
		Audiences: splitList(deref(audienceFlag)),
	}
	return Run(docPath, req, derefBool(cleanFlag), derefBool(dryRunFlag))
}

// Run renders the requested part of the project's matrix. Progress is
// written to stdout.
func Run(docPath string, req config.Request, clean, dryRun bool) error {
	opts, err := BuildOptions(docPath, req, clean, dryRun)
	if err != nil {
		return err
	}
	log := func(format string, args ...any) {
		fmt.Fprintf(os.Stdout, format+"\n", args...)
	}
	log("%d render(s) selected", len(opts.Selections))
	if err := build.Run(opts, log); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	return nil
}

// BuildOptions turns the command line into a render request. Exposed for
// tests. docPath must be absolute.
func BuildOptions(docPath string, req config.Request, clean, dryRun bool) (build.Options, error) {
	cfg, err := config.Load(docPath)
	if err != nil {
		return build.Options{}, fmt.Errorf("render: %w", err)
	}
	sels, err := cfg.Expand(req)
	if err != nil {
		return build.Options{}, fmt.Errorf("render: %w", err)
	}
	return build.Options{
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
