// Package move implements the `qm chapters move` subcommand.
//
// Spec: spec-move.yaml. Moves a chapter from oldOrder to newOrder within
// the same folder, renumbering siblings as needed.
package move

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

// Register adds the move subcommand to the given parent command.
func Register(parent string, projectFlag *string) {
	projectPath = projectFlag
	start.Add(&start.Command{
		Name:   "move",
		Parent: parent,
		Short:  "Move a chapter to a new order position within its folder",
		Long: "Move a chapter from its current order number to a new order " +
			"number within the same folder. Other chapters are renumbered to " +
			"close any gap and to make room at the new position.",
		Flags: []string{"project"},
		Cmd:   cmd,
	})
}

func cmd(c *start.Command) error {
	if len(c.Args) < 3 {
		return fmt.Errorf("usage: qm chapters move <folder> <old-order> <new-order>")
	}
	if projectPath == nil {
		return fmt.Errorf("move: --project flag not initialised")
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

	oldOrder, err := strconv.Atoi(c.Args[1])
	if err != nil {
		return fmt.Errorf("old-order must be an integer: %w", err)
	}
	newOrder, err := strconv.Atoi(c.Args[2])
	if err != nil {
		return fmt.Errorf("new-order must be an integer: %w", err)
	}

	return Run(docPath, folderPath, oldOrder, newOrder)
}

// Run performs the move operation.
func Run(docPath, folderPath string, oldOrder, newOrder int) error {
	if oldOrder == newOrder {
		fmt.Println("Old and new order are equal; nothing to do.")
		return nil
	}

	items, err := qmcore.ScanFolderChapters(docPath, folderPath)
	if err != nil {
		return fmt.Errorf("scanning folder: %w", err)
	}

	// Locate the chapter at oldOrder.
	var moved *qmcore.ChapterItem
	for i := range items {
		if items[i].Order != nil && *items[i].Order == oldOrder {
			moved = &items[i]
			break
		}
	}
	if moved == nil {
		return fmt.Errorf("no chapter at order %d found in %s", oldOrder, folderPath)
	}

	// Compute the new order for every affected chapter.
	updates := make(map[string]int)
	for _, it := range items {
		if it.Order == nil {
			continue
		}
		o := *it.Order
		if it.RelPath == moved.RelPath {
			updates[it.RelPath] = newOrder
			continue
		}
		switch {
		case newOrder < oldOrder:
			// Moving "up" (to a lower number). Shift chapters in
			// [newOrder, oldOrder) by +1. Spec-move PROCESS.2-1.
			if o >= newOrder && o < oldOrder {
				updates[it.RelPath] = o + 1
			}
		case newOrder > oldOrder:
			// Moving "down" (to a higher number). Close the gap
			// behind the moved chapter and make room before it.
			// Spec-move PROCESS.2-2.
			if o > oldOrder && o <= newOrder {
				updates[it.RelPath] = o - 1
			}
		}
	}

	if err := applyOrderUpdates(docPath, updates); err != nil {
		return err
	}

	fmt.Printf("Moved %s from order %d to %d\n", moved.RelPath, oldOrder, newOrder)

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

func applyOrderUpdates(docPath string, updates map[string]int) error {
	for relPath, newOrder := range updates {
		full := filepath.Join(docPath, filepath.FromSlash(relPath))
		err := qmcore.UpdateFrontmatter(full, func(n *yaml.Node) error {
			return qmcore.SetMappingScalar(n, "order", strconv.Itoa(newOrder))
		})
		if err != nil {
			return fmt.Errorf("updating %s: %w", relPath, err)
		}
	}
	return nil
}
