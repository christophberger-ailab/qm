// Package qmcore contains the shared logic used by the qm subcommands.
// It is deliberately *not* named after the user-facing object ("chapters");
// per the spec, object names are not subcommand or package names.
package qmcore

import (
	"path/filepath"
	"regexp"
)

// FileEntry holds metadata for a single .qmd/.md file.
type FileEntry struct {
	RelPath string // relative to doc root, forward-slash separated
	Order   *int
}

// Frontmatter is the YAML front matter we read from .qmd/.md files.
type Frontmatter struct {
	Order    *int   `yaml:"order"`
	InsertAt *int   `yaml:"insert-at"`
	Title    string `yaml:"title"`
}

// ChapterItem represents one entry within a folder's flat chapter order.
// A chapter is either a .qmd/.md file in the folder, or a subfolder
// represented by its index.qmd/index.md file.
type ChapterItem struct {
	// RelPath is the path (forward-slash, relative to the doc root) of the
	// file that carries the chapter's frontmatter. For a subfolder chapter
	// this is the subfolder's index file.
	RelPath string
	// Order is the value of the `order:` field in the frontmatter, or nil.
	Order *int
	// Title is the value of the `title:` field, if present.
	Title string
	// IsDir is true when the chapter is represented by a subfolder
	// (and RelPath points to its index file).
	IsDir bool
}

// DefaultExcludePattern matches basenames starting with `_` or `.` (FILES.3).
const DefaultExcludePattern = `^[._]`

// ShouldExcludeChapter returns true when the file's basename matches the
// exclusion regex. A nil pattern disables filtering.
func ShouldExcludeChapter(relPath string, pattern *regexp.Regexp) bool {
	if pattern == nil {
		return false
	}
	base := filepath.Base(filepath.ToSlash(relPath))
	return pattern.MatchString(base)
}
