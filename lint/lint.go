// Package lint implements the `qm lint <profile>` subcommand.
//
// It runs lint checks over the .qmd source files that belong to the
// given profile. The first check verifies that every opening Quarto div
// fence (three-or-more colons followed by a block descriptor) has a
// matching closing fence (three-or-more colons on an otherwise empty
// line).
package lint

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/cboct/qm/internal/qmcore"
	"github.com/christophberger/start"
)

var projectPath *string

// Register wires the top-level `lint` command into start. Per the spec's
// SUBCOMMANDS.3 constraint, start has no notion of an <object>; because
// `lint` carries no sub-commands, we model it directly as a leaf command.
func Register(projectFlag *string) {
	projectPath = projectFlag

	start.Add(&start.Command{
		Name:  "lint",
		Short: "Run lint checks on the files of a profile",
		Long: "Run lint checks against every .qmd file that belongs to " +
			"the given profile. Usage: qm lint <profile>.",
		Flags: []string{"project"},
		Cmd:   cmd,
	})
}

func cmd(c *start.Command) error {
	if projectPath == nil {
		return fmt.Errorf("lint: --project flag not initialised")
	}
	docPath, err := filepath.Abs(*projectPath)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}
	profile := ""
	if len(c.Args) >= 1 {
		profile = c.Args[0]
	}
	return Run(docPath, profile)
}

// Run performs all lint checks on the files belonging to profileArg,
// resolved against docPath. When profileArg is empty, every .qmd file
// under docPath is linted (except those matching the default exclude
// pattern). It prints one line per finding to stderr and returns a
// non-nil error when at least one finding was reported.
func Run(docPath, profileArg string) error {
	var (
		files []string
		err   error
	)
	if profileArg == "" {
		files, err = allDocFiles(docPath)
	} else {
		files, err = profileFiles(docPath, profileArg)
	}
	if err != nil {
		return err
	}

	var findings []string
	for _, rel := range files {
		abs := filepath.Join(docPath, filepath.FromSlash(rel))
		fs, err := CheckFences(abs)
		if err != nil {
			return fmt.Errorf("lint %s: %w", rel, err)
		}
		for _, f := range fs {
			findings = append(findings, fmt.Sprintf("%s:%d: %s", rel, f.Line, f.Message))
		}
	}

	for _, f := range findings {
		fmt.Fprintln(os.Stderr, f)
	}
	if len(findings) > 0 {
		return fmt.Errorf("lint: %d finding(s)", len(findings))
	}
	fmt.Printf("lint: %d file(s) checked, no findings\n", len(files))
	return nil
}

// profileFiles returns the sorted list of doc-root-relative file paths
// that belong to the given profile.
func profileFiles(docPath, profileArg string) ([]string, error) {
	profileArg = qmcore.NormalizeProfileArg(profileArg)
	profilePath := qmcore.ResolveProfilePath(docPath, profileArg)
	profileName := qmcore.StripYamlExt(filepath.Base(profilePath))
	baseFolder, variant := qmcore.ParseProfileName(profileName)

	baseFolderPath := filepath.Join(docPath, baseFolder)
	if _, err := os.Stat(baseFolderPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("base folder %q not found in document tree %q", baseFolder, docPath)
	}

	excludePattern, err := regexp.Compile(qmcore.DefaultExcludePattern)
	if err != nil {
		return nil, err
	}
	entries, err := qmcore.ScanFiles(docPath, baseFolderPath, variant, excludePattern)
	if err != nil {
		return nil, fmt.Errorf("scanning files: %w", err)
	}
	out := make([]string, 0, len(entries))
	for rel := range entries {
		if filepath.Ext(rel) != ".qmd" {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out, nil
}

// allDocFiles returns every .qmd file under docPath (recursively),
// relative to docPath, in sorted order. Files whose basename matches the
// default exclude pattern are skipped.
func allDocFiles(docPath string) ([]string, error) {
	excludePattern, err := regexp.Compile(qmcore.DefaultExcludePattern)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(docPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(d.Name()) != ".qmd" {
			return nil
		}
		rel, err := filepath.Rel(docPath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if qmcore.ShouldExcludeChapter(rel, excludePattern) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning %s: %w", docPath, err)
	}
	sort.Strings(out)
	return out, nil
}

// Finding is a single lint result.
type Finding struct {
	Line    int
	Message string
}

var (
	// openFenceRE matches a Quarto div opening fence: three or more colons
	// at the start of a line, followed by a non-empty block descriptor.
	openFenceRE = regexp.MustCompile(`^:{3,}\s*\S.*$`)
	// closeFenceRE matches a closing fence: three or more colons on an
	// otherwise empty line (trailing whitespace tolerated).
	closeFenceRE = regexp.MustCompile(`^:{3,}\s*$`)
)

// CheckFences scans path and returns findings for every opening Quarto
// div fence that lacks a matching closing fence. Nested fences are
// tracked with a stack; each surplus opener is reported on its own line.
func CheckFences(path string) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var findings []Finding
	var openLines []int // stack of line numbers for still-open fences

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		switch {
		case closeFenceRE.MatchString(line):
			if len(openLines) > 0 {
				openLines = openLines[:len(openLines)-1]
			}
			// A closing fence without a matching opener is silently
			// ignored here; it is not the concern of this check.
		case openFenceRE.MatchString(line):
			openLines = append(openLines, lineNo)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for _, ln := range openLines {
		findings = append(findings, Finding{
			Line:    ln,
			Message: "unclosed Quarto block fence (no matching `:::` close)",
		})
	}
	return findings, nil
}
