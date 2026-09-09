package finalize

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// underPreview reports whether this hook runs inside `quarto preview`. It is
// a variable so tests can replace it.
var underPreview = previewing

// previewing detects a `quarto preview` run.
//
// Quarto's preview server renders the project, then reads and stamps every
// file it was told about, under the path it reported in
// QUARTO_PROJECT_OUTPUT_DIR. A post-render hook that renames those outputs
// pulls them out from under the server, which fails with
// "cannot touch ...: No such file or directory" followed by a readfile
// error in ServeRenderManager.fileRenderHash. So finalize keeps its hands
// off during a preview.
//
// Quarto sets no environment variable that marks a preview, so the process
// ancestry is inspected: the hook is a descendant of the `quarto preview`
// process. QM_FINALIZE_PREVIEW forces the answer ("1" or "0") for cases the
// detection cannot reach.
func previewing() bool {
	switch os.Getenv("QM_FINALIZE_PREVIEW") {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	}
	for pid, depth := os.Getppid(), 0; pid > 1 && depth < 12; depth++ {
		ppid, args, err := processInfo(pid)
		if err != nil {
			return false
		}
		if isQuartoPreview(args) {
			return true
		}
		pid = ppid
	}
	return false
}

// processInfo returns the parent PID and the command line of pid.
func processInfo(pid int) (int, string, error) {
	out, err := exec.Command("ps", "-o", "ppid=,args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return 0, "", os.ErrNotExist
	}
	ppid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, "", err
	}
	return ppid, strings.Join(fields[1:], " "), nil
}

// isQuartoPreview reports whether a command line is a Quarto preview server.
// Both the `quarto` wrapper and the deno process running quarto.js carry the
// name and the sub-command.
func isQuartoPreview(args string) bool {
	if !strings.Contains(args, "quarto") {
		return false
	}
	for _, f := range strings.Fields(args) {
		if f == "preview" || f == "serve" {
			return true
		}
	}
	return false
}
