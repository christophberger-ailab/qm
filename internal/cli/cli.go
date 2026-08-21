// Package cli holds the small pieces every qm subcommand shares.
package cli

import (
	"os"
	"sync/atomic"

	"github.com/christophberger/start"
)

// failed records that a command returned an error.
var failed atomic.Bool

// Guard wraps a subcommand so that its failure reaches the process exit
// code.
//
// github.com/christophberger/start prints a command's error and then
// returns from Up() normally, leaving the exit status at 0. For most
// commands that is merely wrong; for `qm prepare` it is dangerous. Quarto
// decides whether to abort a render by the pre-render hook's *exit status*,
// so a validation failure that exits 0 lets the render continue — with the
// build documents of whatever was rendered before still in `_build/`. The
// artefact would then be the previous topic's content under the new
// selection's name: a Feuerwehr handout containing the Polizei book.
//
// Every registered command is wrapped, and main() calls Exit() after
// start.Up() returns.
func Guard(fn func(*start.Command) error) func(*start.Command) error {
	return func(c *start.Command) error {
		err := fn(c)
		if err != nil {
			failed.Store(true)
		}
		return err
	}
}

// Exit ends the process with a non-zero status if any guarded command
// failed. It is the last thing main() does.
func Exit() {
	if failed.Load() {
		os.Exit(1)
	}
}
