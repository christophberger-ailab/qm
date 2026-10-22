package web

import (
	"errors"
	"slices"
	"testing"
)

// stubWSL makes wslCommand see the programs named, and translate a path by
// prefixing it, so that no test needs WSL.
func stubWSL(t *testing.T, programs ...string) {
	t.Helper()
	prevLook, prevPath := lookPath, windowsPath
	lookPath = func(name string) (string, error) {
		if slices.Contains(programs, name) {
			return "/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	windowsPath = func(p string) (string, error) { return `\\wsl` + p, nil }
	t.Cleanup(func() { lookPath, windowsPath = prevLook, prevPath })
}

func TestWSLCommandFindsWindowsProgramsAndTranslatesTheirPaths(t *testing.T) {
	stubWSL(t, "mspaint.exe", "gimp", "code")
	for _, tc := range []struct {
		name     string
		args     []string
		wantName string
		wantArgs []string
	}{
		// The Windows shells' short name finds the .exe, and the image is
		// handed over in its Windows form.
		{"mspaint", []string{"/home/u/p/pic.png"}, "mspaint.exe", []string{`\\wsl/home/u/p/pic.png`}},
		{"mspaint.exe", []string{"/home/u/p/pic.png"}, "mspaint.exe", []string{`\\wsl/home/u/p/pic.png`}},
		// A Linux program keeps its Linux paths.
		{"gimp", []string{"/home/u/p/pic.png"}, "gimp", []string{"/home/u/p/pic.png"}},
		{"code", []string{"/home/u/p", "--goto", "/home/u/p/a.qmd"}, "code", []string{"/home/u/p", "--goto", "/home/u/p/a.qmd"}},
		// Only paths are translated, not options.
		{"mspaint", []string{"--opt", "/x.png"}, "mspaint.exe", []string{"--opt", `\\wsl/x.png`}},
		// A program found nowhere is left for exec to report.
		{"krita", []string{"/x.png"}, "krita", []string{"/x.png"}},
	} {
		name, args := wslCommand(tc.name, tc.args)
		if name != tc.wantName || !slices.Equal(args, tc.wantArgs) {
			t.Errorf("wslCommand(%q, %q) = %q %q, want %q %q", tc.name, tc.args, name, args, tc.wantName, tc.wantArgs)
		}
	}
}
