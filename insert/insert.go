// Package insert implements the `qm chapters insert` subcommand.
//
// Spec: spec-insert.yaml. The new chapter is a file the user has already
// created in the target folder, with an `insert-at: <n>` frontmatter entry
// (and no `order:` entry). This subcommand:
//
//  1. Reads the existing order list of the target folder.
//  2. Renumbers all existing chapters with order >= <n> by +1.
//  3. Renames `insert-at` to `order` in the new chapter's frontmatter.
//  4. Invokes the update subcommand to keep the chapter list in sync.
package insert

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/cboct/qm/internal/qmcore"
	"github.com/cboct/qm/update"
	"github.com/christophberger/start"
	"gopkg.in/yaml.v3"
)

var projectPath *string

// Register adds the insert subcommand to the given parent command.
func Register(parent string, projectFlag *string) {
	projectPath = projectFlag
	start.Add(&start.Command{
		Name:   "insert",
		Parent: parent,
		Short:  "Insert a new chapter into a quarto book folder",
		Long: "Insert a new chapter into a folder. The chapter must already exist as " +
			"a file (or subfolder/index.qmd) in the folder, with an `insert-at: <n>` " +
			"frontmatter entry. Existing chapters at order >= n are renumbered.",
		Flags: []string{"project"},
		Cmd:   cmd,
	})
}

func cmd(c *start.Command) error {
	if len(c.Args) < 1 {
		return fmt.Errorf("usage: qm chapters insert <folder>")
	}
	if projectPath == nil {
		return fmt.Errorf("insert: --project flag not initialised")
	}
	docPath, err := filepath.Abs(*projectPath)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}

	folderArg := c.Args[0]
	folderPath := folderArg
	if !filepath.IsAbs(folderPath) {
		folderPath = filepath.Join(docPath, folderArg)
	}
	if info, err := os.Stat(folderPath); err != nil || !info.IsDir() {
		return fmt.Errorf("folder %q does not exist", folderPath)
	}

	return Run(docPath, folderPath)
}

// Run performs the insert operation. folderPath must be the absolute path
// of the target folder; docPath is the absolute project root.
func Run(docPath, folderPath string) error {
	items, err := qmcore.ScanFolderChapters(docPath, folderPath)
	if err != nil {
		return fmt.Errorf("scanning folder: %w", err)
	}

	newItem, insertAt, err := findInsertCandidate(docPath, items)
	if err != nil {
		return err
	}

	if err := renumberForInsert(docPath, items, newItem.RelPath, insertAt); err != nil {
		return err
	}

	if err := convertInsertAtToOrder(filepath.Join(docPath, filepath.FromSlash(newItem.RelPath)), insertAt); err != nil {
		return fmt.Errorf("rewriting new chapter frontmatter: %w", err)
	}

	fmt.Printf("Inserted %s at order %d\n", newItem.RelPath, insertAt)

	folderName, err := update.FolderName(docPath, folderPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot determine top-level folder for %s: %v\n", folderPath, err)
		return nil
	}
	pattern, err := update.CompileExclude()
	if err != nil {
		return err
	}
	return update.UpdateFolderProfiles(docPath, folderName, pattern)
}

// findInsertCandidate returns the chapter item that has insert-at:<n> set
// and no order set. Errors if there is not exactly one such candidate.
func findInsertCandidate(docPath string, items []qmcore.ChapterItem) (qmcore.ChapterItem, int, error) {
	type match struct {
		item     qmcore.ChapterItem
		insertAt int
	}
	var matches []match
	for _, it := range items {
		full := filepath.Join(docPath, filepath.FromSlash(it.RelPath))
		fm, err := qmcore.ReadFrontmatter(full)
		if err != nil {
			return qmcore.ChapterItem{}, 0, fmt.Errorf("reading %s: %w", it.RelPath, err)
		}
		if fm.InsertAt == nil {
			continue
		}
		if fm.Order != nil {
			return qmcore.ChapterItem{}, 0, fmt.Errorf("%s has both `order` and `insert-at`; remove `order` first", it.RelPath)
		}
		matches = append(matches, match{item: it, insertAt: *fm.InsertAt})
	}
	switch len(matches) {
	case 0:
		return qmcore.ChapterItem{}, 0, fmt.Errorf("no chapter with `insert-at:` frontmatter found in folder")
	case 1:
		return matches[0].item, matches[0].insertAt, nil
	default:
		return qmcore.ChapterItem{}, 0, fmt.Errorf("multiple chapters with `insert-at:` found; resolve before inserting")
	}
}

// renumberForInsert shifts every existing chapter with order >= insertAt by
// +1. The new chapter is skipped (its order is set separately).
func renumberForInsert(docPath string, items []qmcore.ChapterItem, newRel string, insertAt int) error {
	for _, it := range items {
		if it.RelPath == newRel {
			continue
		}
		if it.Order == nil {
			continue
		}
		if *it.Order < insertAt {
			continue
		}
		newOrder := *it.Order + 1
		full := filepath.Join(docPath, filepath.FromSlash(it.RelPath))
		if err := setOrder(full, newOrder); err != nil {
			return fmt.Errorf("renumbering %s: %w", it.RelPath, err)
		}
	}
	return nil
}

func setOrder(path string, order int) error {
	return qmcore.UpdateFrontmatter(path, func(n *yaml.Node) error {
		return qmcore.SetMappingScalar(n, "order", strconv.Itoa(order))
	})
}

// convertInsertAtToOrder rewrites the new chapter's frontmatter, removing
// `insert-at` and writing `order: <insertAt>`.
func convertInsertAtToOrder(path string, insertAt int) error {
	return qmcore.UpdateFrontmatter(path, func(n *yaml.Node) error {
		qmcore.RemoveMappingKey(n, "insert-at")
		return qmcore.SetMappingScalar(n, "order", strconv.Itoa(insertAt))
	})
}
