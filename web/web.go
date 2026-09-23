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
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/christophberger-ailab/qm/internal/cli"
	"github.com/christophberger/start"
	flag "github.com/spf13/pflag"
)

// defaultAddr is where the UI listens unless told otherwise. When its port
// is taken -- by another qm web, most likely -- the next ones are tried.
const defaultAddr = "localhost:8199"

// maxPortTries bounds the search for a free port, so a machine that has
// every port above the default taken gets an error rather than a scan.
const maxPortTries = 100

var (
	projectPath *string
	addr        *string
)

// Register wires the top-level `web` command into start. Like `lint` and
// `render`, `web` carries no sub-commands and is registered as a leaf
// (SUBCOMMANDS.3 does not apply when there is nothing to dispatch to).
func Register(projectFlag *string) {
	projectPath = projectFlag
	addr = flag.String("addr", defaultAddr,
		"Address the web UI listens on; without it, the first free port from 8199 up")

	start.Add(&start.Command{
		Name:  "web",
		Short: "Serve the web UI for sorting, editing, and rendering pages",
		Long: "Serve a local web app that shows the project's page tree, " +
			"reorders pages by drag and drop, edits their content, and " +
			"renders the project's book folders. Usage: qm web [<path>]. " +
			"Without a path, the tree at --project is opened.",
		Flags: []string{"project", "addr"},
		Cmd:   cli.Guard(cmd),
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
	// Only the default address is searched from: an address the user named
	// is the one they want, and serving elsewhere would be a surprise.
	return Serve(*addr, !flag.CommandLine.Changed("addr"), docPath)
}

// Serve runs the web UI on listenAddr with the project at root open. With
// searchPort, a port that is already in use makes it try the next one up
// until one is free. It blocks until the server stops.
func Serve(listenAddr string, searchPort bool, root string) error {
	srv, err := newServer(defaultConfigFile())
	if err != nil {
		return fmt.Errorf("web: %w", err)
	}
	if root != "" {
		if err := srv.setRoot(root); err != nil {
			return fmt.Errorf("web: cannot open %s: %w", root, err)
		}
	}
	ln, err := listen(listenAddr, searchPort)
	if err != nil {
		return fmt.Errorf("web: %w", err)
	}
	fmt.Fprintf(os.Stderr, "qm web running on http://%s\n", servedAddr(listenAddr, ln.Addr()))
	if err := http.Serve(ln, srv); err != nil {
		return fmt.Errorf("web: %w", err)
	}
	return nil
}

// listen opens addr, or with searchPort the first port from addr's up that
// is not in use. Any other failure -- a host that does not resolve, a port
// the user may not bind -- is returned at once: counting up would not fix
// it.
func listen(addr string, searchPort bool) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil || !searchPort || !addrInUse(err) {
		return ln, err
	}
	host, portStr, splitErr := net.SplitHostPort(addr)
	port, atoiErr := strconv.Atoi(portStr)
	if splitErr != nil || atoiErr != nil || port == 0 {
		return nil, err
	}
	first := err
	for i := 1; i < maxPortTries && port+i <= 65535; i++ {
		ln, err = net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port+i)))
		if err == nil {
			return ln, nil
		}
		if !addrInUse(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no free port among the %d from %d: %w", maxPortTries, port, first)
}

// wsaeAddrInUse is Windows' "address already in use", which its sockets
// report instead of the EADDRINUSE the syscall package names.
const wsaeAddrInUse = 10048

// addrInUse reports whether a listen failed because the port is taken.
func addrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == wsaeAddrInUse
}

// servedAddr is the address to tell the user: the host as they gave it --
// "localhost" reads better than the 127.0.0.1 it resolved to -- with the
// port actually listened on, which a search may have moved.
func servedAddr(requested string, got net.Addr) string {
	host, _, err := net.SplitHostPort(requested)
	tcp, ok := got.(*net.TCPAddr)
	if err != nil || host == "" || !ok {
		return got.String()
	}
	return net.JoinHostPort(host, strconv.Itoa(tcp.Port))
}
