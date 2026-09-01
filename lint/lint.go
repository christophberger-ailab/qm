// Package lint implements the `qm lint [<topic>]` subcommand.
//
// It runs lint checks over the .qmd source files that belong to the
// given topic. The first check verifies that every opening Quarto div
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

	"github.com/cboct/qm/internal/cli"
	"github.com/cboct/qm/internal/qmcore"
	"github.com/christophberger/start"
	flag "github.com/spf13/pflag"
)

var (
	projectPath  *string
	audienceFlag *string
)

// Register wires the top-level `lint` command into start. Per the spec's
// SUBCOMMANDS.3 constraint, start has no notion of an <object>; because
// `lint` carries no sub-commands, we model it directly as a leaf command.
func Register(projectFlag *string) {
	projectPath = projectFlag
	audienceFlag = flag.String("lint-audience", "",
		"Only lint the files of this audience's _POL/_FW variants")

	start.Add(&start.Command{
		Name:  "lint",
		Short: "Run lint checks on the files of a topic",
		Long: "Run lint checks against every .qmd file that belongs to " +
			"the given topic, or against the whole project when no topic is " +
			"named or the topic is `all` or `none`. Usage: qm lint [<topic>].",
		Flags: []string{"project", "lint-audience"},
		Cmd:   cli.Guard(cmd),
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
	topic := ""
	if len(c.Args) >= 1 {
		topic = c.Args[0]
	}
	audience := ""
	if audienceFlag != nil {
		audience = *audienceFlag
	}
	return Run(docPath, topic, audience)
}

// Run performs all lint checks on the files belonging to topicArg,
// resolved against docPath. When topicArg is empty or names a whole-project
// topic (`all`, `none`), every .qmd file under docPath is linted (except
// those matching the default exclude pattern). It prints one line per
// finding to stderr and returns a non-nil error when at least one finding
// was reported.
func Run(docPath, topicArg, audience string) error {
	var (
		files []string
		err   error
	)
	if topicArg == "" {
		files, err = allDocFiles(docPath)
	} else {
		files, err = topicFiles(docPath, topicArg, audience)
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

// topicFiles returns the sorted list of doc-root-relative file paths that
// belong to the given topic.
//
// The argument is a topic — `calltaker` or `topic-calltaker` — not a whole
// profile selection: linting looks at the source files, and those are the
// same for every format and audience. The audience only decides which of a
// folder's `_POL`/`_FW` variant files are in play, so it is an optional
// narrowing, not part of the address.
func topicFiles(docPath, topicArg, audience string) ([]string, error) {
	topic := topicArg
	if a, value, ok := qmcore.SplitProfileName(topicArg); ok {
		if a != qmcore.AxisTopic {
			return nil, fmt.Errorf("%q is a %s profile; qm lint takes a topic", topicArg, a)
		}
		topic = value
	}
	// `all` and `none` name no folder: they are the whole project, which is
	// what linting without a topic already covers.
	if qmcore.IsWholeProject(topic) {
		return allDocFiles(docPath)
	}
	folder := topic
	if p, err := qmcore.LoadProfile(docPath, qmcore.AxisTopic.ProfileName(topic)); err == nil {
		if p.QM.Folder != "" {
			folder = p.QM.Folder
		}
	}

	baseFolderPath := filepath.Join(docPath, folder)
	if _, err := os.Stat(baseFolderPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("content folder %q not found in document tree %q", folder, docPath)
	}

	excludePattern, err := regexp.Compile(qmcore.DefaultExcludePattern)
	if err != nil {
		return nil, err
	}
	entries, err := qmcore.ScanFiles(docPath, baseFolderPath, audience, excludePattern)
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
