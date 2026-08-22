// Package remove implements the `qm chapters remove` subcommand.
//
// Spec: spec-remove.yaml. Removes a chapter (file or subfolder/index.qmd)
// at a given order number. Confirms with the user before deletion. Tries
// the external `trash` command before falling back to os.Remove /
// os.RemoveAll.
package remove

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/christophberger-ailab/qm/internal/cli"
	"github.com/christophberger-ailab/qm/internal/qmcore"
	"github.com/christophberger/start"
)

var projectPath *string

// Register adds the remove subcommand to the given parent command.
func Register(parent string, projectFlag *string) {
	projectPath = projectFlag
	start.Add(&start.Command{
		Name:   "remove",
		Parent: parent,
		Short:  "Remove a chapter from a folder after confirmation",
		Long: "Remove the chapter at the given order number from a folder. " +
			"Asks for confirmation, then tries to trash the file using the " +
			"`trash` command, falling back to permanent deletion.",
		Flags: []string{"project"},
		Cmd:   cli.Guard(cmd),
	})
}

func cmd(c *start.Command) error {
	if len(c.Args) < 2 {
		return fmt.Errorf("usage: qm chapters remove <folder> <order>")
	}
	if projectPath == nil {
		return fmt.Errorf("remove: --project flag not initialised")
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

	order, err := strconv.Atoi(c.Args[1])
	if err != nil {
		return fmt.Errorf("order must be an integer: %w", err)
	}

	return Run(docPath, folderPath, order, os.Stdin, os.Stdout)
}

// Run performs the remove operation, prompting via in/out for confirmation.
// Separated from cmd so tests can drive it with synthetic input.
func Run(docPath, folderPath string, order int, in io.Reader, out io.Writer) error {
	items, err := qmcore.ScanFolderChapters(docPath, folderPath)
	if err != nil {
		return fmt.Errorf("scanning folder: %w", err)
	}

	var target *qmcore.ChapterItem
	for i := range items {
		if items[i].Order != nil && *items[i].Order == order {
			target = &items[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("no chapter at order %d found in %s", order, folderPath)
	}

	orderStr := "-"
	if target.Order != nil {
		orderStr = strconv.Itoa(*target.Order)
	}
	fmt.Fprintf(out, "About to remove chapter:\n  file:  %s\n  order: %s\n  title: %s\n",
		target.RelPath, orderStr, target.Title)
	fmt.Fprint(out, "Proceed? [y/N]: ")

	confirmed, err := readYesNo(in)
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Fprintln(out, "Aborted.")
		return nil
	}

	if err := deleteChapter(docPath, *target); err != nil {
		return err
	}
	fmt.Fprintf(out, "Removed %s\n", target.RelPath)

	return nil
}

func readYesNo(in io.Reader) (bool, error) {
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

// deleteChapter removes the chapter, deleting the containing subfolder when
// the chapter is a directory (its frontmatter lives in index.qmd inside the
// folder).
func deleteChapter(docPath string, item qmcore.ChapterItem) error {
	target := filepath.Join(docPath, filepath.FromSlash(item.RelPath))
	if item.IsDir {
		target = filepath.Dir(target)
	}
	return trashOrRemove(target)
}

// trashOrRemove attempts to invoke the `trash` CLI; if it is not available
// or returns an error, the path is removed permanently.
func trashOrRemove(path string) error {
	if _, err := exec.LookPath("trash"); err == nil {
		c := exec.Command("trash", path)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err == nil {
			return nil
		}
		fmt.Fprintln(os.Stderr, "trash command failed, falling back to permanent deletion")
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}
