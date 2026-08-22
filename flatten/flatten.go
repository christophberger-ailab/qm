// Package flatten implements the `qm flatten` command.
//
// Spec: spec-flatten.yaml.
//
// It writes the same `_build/book.qmd` and `_build/slides.qmd` that
// `qm prepare` writes during a render, and stops there. It is the way to
// look at what a render will actually feed to Quarto — heading demotion,
// rewritten in-book links, extracted slides — without waiting for pandoc.
package flatten

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/christophberger-ailab/qm/internal/bookrender"
	"github.com/christophberger-ailab/qm/internal/cli"
	"github.com/christophberger-ailab/qm/internal/qmcore"
	"github.com/christophberger/start"
)

var projectPath *string

// Register wires the `flatten` command into start.
func Register(projectFlag *string) {
	projectPath = projectFlag
	start.Add(&start.Command{
		Name:  "flatten",
		Short: "Write the build documents of one topic without rendering",
		Long: "Flatten a topic's content folder into _build/book.qmd and " +
			"_build/slides.qmd, then exit. Usage: qm flatten <topic>. The " +
			"topic is the value of a `topic-<name>` profile; its content " +
			"folder is the profile's `qm: folder:`, or the topic name.",
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
		topics, _ := qmcore.AxisValues(root, qmcore.AxisTopic)
		return fmt.Errorf("usage: qm flatten <topic>; the project has: %v", topics)
	}
	return Run(root, c.Args[0])
}

// Run flattens one topic into the build documents.
//
// Only the topic profile is read, so the deck's title keeps whatever
// variables the other two axes would have supplied; a render resolves them
// through `qm prepare`. Everything else — the flattening itself — is
// audience- and format-independent: the audience divs are reproduced around
// the content and filtered at render time.
func Run(root, topic string) error {
	p, err := qmcore.LoadProfile(root, qmcore.AxisTopic.ProfileName(topic))
	if err != nil {
		return fmt.Errorf("flatten: %w", err)
	}
	folder := p.QM.Folder
	if folder == "" {
		folder = topic
	}
	if topic == qmcore.NoTopic {
		folder = ""
	}
	log := func(format string, args ...any) {
		fmt.Fprintf(os.Stdout, format+"\n", args...)
	}
	if err := bookrender.WriteBuild(root, folder, p.Book.Title, log); err != nil {
		return fmt.Errorf("flatten: %w", err)
	}
	log("wrote %s and %s",
		rel(root, bookrender.BookFile(root)), rel(root, bookrender.SlidesFile(root)))
	return nil
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}
