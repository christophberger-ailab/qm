// Command qm manages a Quarto documentation tree.
//
// Usage: qm <object> <subcommand> [flags] [args]
//
// The object "chapters" carries the subcommands `update`, `insert`,
// `move`, and `remove`; the objects "lint", "render", and "web" have no
// subcommands. The "chapters" command itself is a dummy wrapper required
// because github.com/christophberger/start has no native notion of an
// "object" parameter (see spec.yaml, constraint SUBCOMMANDS.3).
package main

import (
	"fmt"
	"os"

	"github.com/cboct/qm/insert"
	"github.com/cboct/qm/lint"
	"github.com/cboct/qm/move"
	"github.com/cboct/qm/remove"
	"github.com/cboct/qm/render"
	"github.com/cboct/qm/update"
	"github.com/cboct/qm/web"

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
		Short: "Manage book chapters (subcommands: update, insert, move, remove)",
		Long: "Manage the chapters of a Quarto book. Use one of the subcommands " +
			"(update, insert, move, remove). With no subcommand, lists them.",
	}); err != nil {
		fatal(err)
	}

	// Register each subcommand under "chapters" as its own package.
	update.Register("chapters", projectFlag)
	insert.Register("chapters", projectFlag)
	move.Register("chapters", projectFlag)
	remove.Register("chapters", projectFlag)

	// The `lint` object has no sub-commands, so it is registered as a
	// leaf command directly (SUBCOMMANDS.3 does not apply when there is
	// nothing to dispatch to).
	lint.Register(projectFlag)

	// The `render` object also has no sub-commands.
	render.Register(projectFlag)

	// The `web` object serves the sorter UI; no sub-commands either.
	web.Register(projectFlag)

	start.Up()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
