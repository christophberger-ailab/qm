// Package update implements the `qm chapters update` subcommand.
//
// It (re)generates the `book.chapters` list inside one or more Quarto
// profile yaml files based on the `order:` frontmatter values found in
// the corresponding document folder.
package update

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cboct/qm/internal/qmcore"
	"github.com/christophberger/start"
	flag "github.com/spf13/pflag"
)

var (
	projectPath    *string
	excludePattern *string
)

// Register adds the update subcommand to the given parent command.
// projectFlag must be the *string pointer returned by the qm-wide
// --project flag declaration.
func Register(parent string, projectFlag *string) {
	projectPath = projectFlag
	excludePattern = flag.StringP("exclude", "x", qmcore.DefaultExcludePattern,
		"Regex matched against file basenames to exclude chapters (default: files starting with _ or .)")

	start.Add(&start.Command{
		Name:   "update",
		Parent: parent,
		Short:  "Update the chapters list of one or all profile yaml files",
		Long:   "Update the `book.chapters` list of one or all `_quarto-*.yml` profiles, based on the per-file `order:` frontmatter.",
		Flags:  []string{"project", "exclude"},
		Cmd:    cmd,
	})
}

func cmd(c *start.Command) error {
	args := c.Args
	if projectPath == nil {
		return fmt.Errorf("update: --project flag not initialised")
	}
	docPath, err := filepath.Abs(*projectPath)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}

	var profiles []string
	if len(args) >= 1 && args[0] != "" {
		profiles = []string{args[0]}
	} else {
		discovered, err := qmcore.DiscoverProfiles(docPath)
		if err != nil {
			return fmt.Errorf("cannot list profiles in %s: %w", docPath, err)
		}
		profiles = discovered
	}
	if len(profiles) == 0 {
		return fmt.Errorf("no profile yaml files found in %s", docPath)
	}

	pattern, err := CompileExclude()
	if err != nil {
		return err
	}

	for _, p := range profiles {
		if err := Run(docPath, p, pattern); err != nil {
			return err
		}
	}
	return nil
}

// CompileExclude returns the compiled excludePattern regex, or nil when
// the pattern is empty. Returns an error for an invalid regex.
func CompileExclude() (*regexp.Regexp, error) {
	if excludePattern == nil || *excludePattern == "" {
		return nil, nil
	}
	pat, err := regexp.Compile(*excludePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid --exclude regex %q: %w", *excludePattern, err)
	}
	return pat, nil
}

// Run regenerates the chapter list of a single profile yaml.
//
// profileArg may be a bare profile name ("calltaker"), a name with the
// `_quarto-` prefix, or a file name with extension. The doc tree is
// searched at docPath.
func Run(docPath, profileArg string, excludePattern *regexp.Regexp) error {
	profileArg = qmcore.NormalizeProfileArg(profileArg)
	profilePath := qmcore.ResolveProfilePath(docPath, profileArg)

	profileName := qmcore.StripYamlExt(filepath.Base(profilePath))
	baseFolder, variant := qmcore.ParseProfileName(profileName)

	baseFolderPath := filepath.Join(docPath, baseFolder)
	if _, err := os.Stat(baseFolderPath); os.IsNotExist(err) {
		return fmt.Errorf("base folder %q not found in document tree %q", baseFolder, docPath)
	}

	entries, err := qmcore.ScanFiles(docPath, baseFolderPath, variant, excludePattern)
	if err != nil {
		return fmt.Errorf("scanning files: %w", err)
	}

	for _, e := range entries {
		if e.Order == nil {
			fmt.Fprintf(os.Stderr, "warning: no order in frontmatter: %s\n", e.RelPath)
		}
	}

	chapters := qmcore.BuildFolderChapters(docPath, baseFolderPath, baseFolder, entries)

	// Quarto requires the cover page to be "index.qmd" without a folder prefix.
	if len(chapters) > 0 {
		base := filepath.Base(chapters[0])
		if base != "index.qmd" && base != "index.md" {
			return fmt.Errorf("cover page: expected index.(q)md, got %s", base)
		}
		chapters[0] = base
	}

	if err := qmcore.UpdateProfileYaml(profilePath, chapters); err != nil {
		return fmt.Errorf("updating profile yaml: %w", err)
	}

	fmt.Printf("Updated %s with %d chapters\n", profilePath, len(chapters))
	return nil
}

// UpdateFolderProfiles regenerates every profile yaml whose base folder
// matches the given folder name. Used after insert/move/remove to keep
// chapter lists in sync.
func UpdateFolderProfiles(docPath, folderName string, pattern *regexp.Regexp) error {
	profiles, err := qmcore.DiscoverProfiles(docPath)
	if err != nil {
		return err
	}
	any := false
	for _, p := range profiles {
		name := qmcore.StripYamlExt(p)
		base, _ := qmcore.ParseProfileName(name)
		if base != folderName {
			continue
		}
		if err := Run(docPath, p, pattern); err != nil {
			return err
		}
		any = true
	}
	if !any {
		fmt.Fprintf(os.Stderr, "note: no profile yaml found for folder %q; skipped chapter list update\n", folderName)
	}
	return nil
}

// FolderName returns the top-level folder name (relative to docPath) that
// contains the given target path. The first path segment is returned.
// E.g. docPath=/x, target=/x/sysadmin/intro -> "sysadmin".
func FolderName(docPath, targetPath string) (string, error) {
	absDoc, err := filepath.Abs(docPath)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDoc, absTarget)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") || rel == ".." {
		return "", fmt.Errorf("%s is outside the project root %s", targetPath, docPath)
	}
	parts := strings.SplitN(rel, "/", 2)
	if parts[0] == "." || parts[0] == "" {
		return "", fmt.Errorf("target %s is the project root itself", targetPath)
	}
	return parts[0], nil
}

// ProjectPath returns the resolved absolute path of the qm-wide --project flag.
func ProjectPath() (string, error) {
	if projectPath == nil {
		return "", fmt.Errorf("--project flag is not initialised")
	}
	return filepath.Abs(*projectPath)
}
