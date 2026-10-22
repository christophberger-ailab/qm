package web

// wsl.go — starting Windows programs from qm running under WSL.
//
// Under WSL, qm is a Linux program, but the user's editors may well be
// Windows ones. WSL runs those, with two catches: a Windows program is only
// found by its full file name, mspaint.exe rather than the mspaint that
// Windows' own shells accept, and it cannot open a Linux path. So a command
// whose program is not on the PATH gets a second try with ".exe", and a
// Windows program is handed the Windows form of the paths it is to open.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// inWSL reports whether qm runs under the Windows Subsystem for Linux.
// WSL's kernel says so in its version string.
var inWSL = sync.OnceValue(func() bool {
	v, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(v)), "microsoft")
})

// lookPath and windowsPath are how wslCommand finds a program and
// translates a path; tests replace them.
var lookPath = exec.LookPath

var windowsPath = func(p string) (string, error) {
	out, err := exec.Command("wslpath", "-w", p).Output()
	return strings.TrimSpace(string(out)), err
}

// wslCommand adapts a command line for WSL: a program not found by its name
// is looked for as a Windows program, and when the program is a Windows one,
// each argument that is an absolute path is turned into its Windows form.
// Anything it cannot make sense of is left as it was, for exec to report.
func wslCommand(name string, args []string) (string, []string) {
	if _, err := lookPath(name); err != nil && filepath.Ext(name) == "" {
		if _, err := lookPath(name + ".exe"); err == nil {
			name += ".exe"
		}
	}
	if !strings.EqualFold(filepath.Ext(name), ".exe") {
		return name, args
	}
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a
		if strings.HasPrefix(a, "/") {
			if w, err := windowsPath(a); err == nil && w != "" {
				out[i] = w
			}
		}
	}
	return name, out
}
