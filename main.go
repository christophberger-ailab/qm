// Command qm manages a Quarto documentation tree.
//
// Usage: qm <object> <subcommand> [flags] [args]
//
// The object "chapters" carries the subcommands `insert`, `move`, and
// `remove`; "lint", "flatten", "render", "prepare", "finalize", and "web"
// have no subcommands. The "chapters" command itself is a dummy wrapper
// required because github.com/christophberger/start has no native notion of
// an "object" parameter (see spec.yaml, constraint SUBCOMMANDS.3).
package main

import (
	"fmt"
	"os"

	"github.com/christophberger-ailab/qm/finalize"
	"github.com/christophberger-ailab/qm/flatten"
	"github.com/christophberger-ailab/qm/insert"
	"github.com/christophberger-ailab/qm/internal/cli"
	"github.com/christophberger-ailab/qm/lint"
	"github.com/christophberger-ailab/qm/move"
	"github.com/christophberger-ailab/qm/prepare"
	"github.com/christophberger-ailab/qm/remove"
	"github.com/christophberger-ailab/qm/render"
	"github.com/christophberger-ailab/qm/web"

	"github.com/christophberger/start"
	flag "github.com/spf13/pflag"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	// Global flag(s) — shared by all subcommands. Per spec
	// constraint SUBCOMMANDS.2-2 these have matching environment
	// variables (QM_PROJECT) and config file entries (project=).
	projectFlag := flag.StringP("project", "p", ".",
		"Path to the Quarto document tree (default: current directory)")

	// The profile selection is shared by the two Quarto hooks, `prepare`
	// and `finalize`, so it is declared once here rather than by either of
	// them. Both default to $QUARTO_PROFILE when it is not given.
	profileFlag := flag.String("profile", "",
		"Comma-separated profile selection topic-<t>,format-<f>,audience-<a> "+
			"(default: $QUARTO_PROFILE)")

	start.SetDescription("qm manages a Quarto documentation tree. " +
		"Usage: qm <object> <subcommand> [flags] [args].")
	start.SetVersion(version)
	start.SetInitFunc(func() error { return nil })

	// Register the dummy "chapters" object command. start has no notion
	// of an <object> parameter, so we model it as a parent command that
	// only dispatches to its subcommands (SUBCOMMANDS.3). Leaving Cmd
	// nil lets start auto-generate the "missing subcommand" usage
	// (and avoids a panic in start v0.6.0 when Cmd is set but no
	// subcommand was supplied).
	if err := start.Add(&start.Command{
		Name:  "chapters",
		Short: "Manage book chapters (subcommands: insert, move, remove)",
		Long: "Manage the chapters of a Quarto book. Use one of the subcommands " +
			"(insert, move, remove). With no subcommand, lists them.",
	}); err != nil {
		fatal(err)
	}

	// Register each subcommand under "chapters" as its own package.
	insert.Register("chapters", projectFlag)
	move.Register("chapters", projectFlag)
	remove.Register("chapters", projectFlag)

	// The `lint` object has no sub-commands, so it is registered as a
	// leaf command directly (SUBCOMMANDS.3 does not apply when there is
	// nothing to dispatch to).
	lint.Register(projectFlag)

	// The `render` object also has no sub-commands.
	render.Register(projectFlag)

	// The `flatten` object saves render's generated inputs without rendering.
	flatten.Register(projectFlag)

	// `prepare` and `finalize` are the project's Quarto pre- and
	// post-render hooks. They are what makes a plain
	// `quarto render --profile topic-x,format-y,audience-z` produce a
	// correctly built and correctly named artefact without qm being
	// involved in the invocation.
	prepare.Register(projectFlag, profileFlag)
	finalize.Register(projectFlag, profileFlag)

	// The `web` object serves the sorter UI; no sub-commands either.
	web.Register(projectFlag)

	start.Up()

	// start.Up() prints a command's error and returns; it does not set an
	// exit status. Quarto reads the pre-render hook's exit status to decide
	// whether to abort a render, so `qm prepare` failing silently with 0
	// would let a render continue on stale build documents. See
	// internal/cli.
	cli.Exit()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
