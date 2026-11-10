// Package flatten implements the `qm flatten` command.
//
// Spec: spec-flatten.yaml.
//
// It writes the Markdown a document render of one topic parses —
// `_build/<topic>.md`: the pages in folder order, headings demoted by
// folder depth, in-book links rewritten into anchors, shortcodes resolved —
// and stops there. It is the way to look at what a render works from.
// Conditional content is still in it; a render removes what the selected
// format and audience do not show.
package flatten

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/christophberger-ailab/qm/internal/build"
	"github.com/christophberger-ailab/qm/internal/cli"
	"github.com/christophberger-ailab/qm/internal/config"
	"github.com/christophberger/start"
)

// BuildDir is where the flattened documents go.
const BuildDir = "_build"

var projectPath *string

// Register wires the `flatten` command into start.
func Register(projectFlag *string) {
	projectPath = projectFlag
	start.Add(&start.Command{
		Name:  "flatten",
		Short: "Write the flattened Markdown of one topic without rendering",
		Long: "Flatten a topic's content folder into _build/<topic>.md, then " +
			"exit. Usage: qm flatten <topic>. The topic is one of qm.yml's " +
			"topics (or, without qm.yml, a top-level content folder).",
		Flags: []string{"project"},
		Cmd:   cli.Guard(cmd),
	})
}

func cmd(c *start.Command) error {
	if projectPath == nil {
		return fmt.Errorf("flatten: --project flag not initialised")
	}
	root, err := filepath.Abs(*projectPath)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}
	if len(c.Args) != 1 {
		cfg, err := config.Load(root)
		if err != nil {
			return err
		}
		return fmt.Errorf("usage: qm flatten <topic>; the project has: %v", cfg.Topics.Names())
	}
	return Run(root, c.Args[0])
}

// Run flattens one topic and returns nothing but errors; the progress goes
// to stdout.
func Run(root, topic string) error {
	cfg, err := config.Load(root)
	if err != nil {
		return fmt.Errorf("flatten: %w", err)
	}
	t, ok := cfg.Topics.Get(topic)
	if !ok {
		return fmt.Errorf("flatten: unknown topic %q (have %v)", topic, cfg.Topics.Names())
	}
	log := func(format string, args ...any) {
		fmt.Fprintf(os.Stdout, format+"\n", args...)
	}
	sel := config.Selection{Config: cfg, Topic: t, Format: cfg.Formats[0].Value, Audience: cfg.Audiences[0].Value}
	text, _, err := build.Flatten(sel, log)
	if err != nil {
		return fmt.Errorf("flatten: %w", err)
	}
	out := filepath.Join(root, BuildDir, topic+".md")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
		return err
	}
	log("wrote %s", filepath.ToSlash(filepath.Join(BuildDir, topic+".md")))
	return nil
}
