// Package web implements the `qm web` command: a local web app for
// reordering the pages of a Quarto project by drag and drop, editing them,
// and rendering its book folders.
//
// Spec: spec-web.yaml.
//
//	qm web                 -> serve the project at --project on the default port
//	qm web <path>          -> serve the project at <path>
//	qm web --addr :9000    -> listen elsewhere
//
// The package is the quarto-sorter tool
// (https://github.com/christophberger-ailab/quarto-sorter) merged into qm;
// its render panel drives the same internal/bookrender flow that
// `qm render` runs from the command line.
package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/christophberger/start"
	flag "github.com/spf13/pflag"
)

var (
	projectPath *string
	addr        *string
)

// Register wires the top-level `web` command into start. Like `lint` and
// `render`, `web` carries no sub-commands and is registered as a leaf
// (SUBCOMMANDS.3 does not apply when there is nothing to dispatch to).
func Register(projectFlag *string) {
	projectPath = projectFlag
	addr = flag.String("addr", "localhost:8199",
		"Address the web UI listens on")

	start.Add(&start.Command{
		Name:  "web",
		Short: "Serve the web UI for sorting, editing, and rendering pages",
		Long: "Serve a local web app that shows the project's page tree, " +
			"reorders pages by drag and drop, edits their content, and " +
			"renders the project's book folders. Usage: qm web [<path>]. " +
			"Without a path, the tree at --project is opened.",
		Flags: []string{"project", "addr"},
		Cmd:   cmd,
	})
}

func cmd(c *start.Command) error {
	if projectPath == nil {
		return fmt.Errorf("web: --project flag not initialised")
	}
	// A positional path wins over --project so that `qm web ../book` reads
	// as it looks; both end up as the initially opened project.
	root := *projectPath
	if len(c.Args) >= 1 && c.Args[0] != "" {
		root = c.Args[0]
	}
	docPath, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}
	return Serve(*addr, docPath)
}

// Serve runs the web UI on listenAddr with the project at root open. It
// blocks until the server stops.
func Serve(listenAddr, root string) error {
	srv, err := newServer(defaultPrefsFile())
	if err != nil {
		return fmt.Errorf("web: %w", err)
	}
	if root != "" {
		if err := srv.setRoot(root); err != nil {
			return fmt.Errorf("web: cannot open %s: %w", root, err)
		}
	}
	fmt.Fprintf(os.Stderr, "qm web running on http://%s\n", listenAddr)
	if err := http.ListenAndServe(listenAddr, srv); err != nil {
		return fmt.Errorf("web: %w", err)
	}
	return nil
}
