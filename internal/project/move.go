package project

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Move makes the page at src the child at position pos of the page at
// parent ("" means the project root). It moves files when the parent
// changes, shifts Markdown headings by the depth difference, renumbers the
// destination siblings sequentially, and closes the order gap in the
// source group. The receiver is stale afterwards; reload the tree.
func (t *Tree) Move(src, parent string, pos int) error {
	sp := t.Find(src)
	if sp == nil {
		return fmt.Errorf("page %s not found", src)
	}
	destDir, destSiblings, destDepth, err := t.resolveParent(parent)
	if err != nil {
		return err
	}

	newPath := src
	if srcDir := groupDir(src); destDir != srcDir {
		if newPath, err = t.reparent(sp, destDir, destDepth); err != nil {
			return err
		}
		// Close the order gap left in the source group.
		n := 0
		for _, p := range t.group(srcDir) {
			if p != sp && p.Order != nil {
				n++
				if err := t.setOrder(p.Path, n); err != nil {
					return err
				}
			}
		}
	}

	// Renumber the destination group in its new display order.
	list := make([]*Page, 0, len(destSiblings)+1)
	for _, p := range destSiblings {
		if p != sp {
			list = append(list, p)
		}
	}
	pos = min(max(pos, 0), len(list))
	list = append(list[:pos], append([]*Page{sp}, list[pos:]...)...)
	for i, p := range list {
		file := p.Path
		if p == sp {
			file = newPath
		}
		if err := t.setOrder(file, i+1); err != nil {
			return err
		}
	}
	return nil
}

// reparent moves the page's files under destDir and shifts headings by the
// depth difference. It returns the page's new path.
func (t *Tree) reparent(sp *Page, destDir string, destDepth int) (string, error) {
	src := sp.Path
	// The unit to move: an index.qmd section moves its directory; anything
	// else moves the file, plus its resource/children directory if present.
	var renames [][2]string // relative old, new
	var newPath string
	if base := path.Base(src); base == "index.qmd" {
		dir := path.Dir(src)
		newDir := join(destDir, path.Base(dir))
		renames = append(renames, [2]string{dir, newDir})
		newPath = newDir + "/index.qmd"
	} else {
		newPath = join(destDir, base)
		renames = append(renames, [2]string{src, newPath})
		if fi, err := os.Stat(t.abs(sp.Dir)); err == nil && fi.IsDir() {
			renames = append(renames, [2]string{sp.Dir, join(destDir, path.Base(sp.Dir))})
		}
	}
	for _, r := range renames {
		if destDir == r[0] || strings.HasPrefix(destDir+"/", r[0]+"/") {
			return "", fmt.Errorf("cannot move %s into its own subtree", src)
		}
		if _, err := os.Stat(t.abs(r[1])); err == nil {
			return "", fmt.Errorf("%s already exists", r[1])
		}
	}
	if destDir != "." {
		if err := os.MkdirAll(t.abs(destDir), 0o755); err != nil {
			return "", err
		}
	}
	for _, r := range renames {
		if err := os.Rename(t.abs(r[0]), t.abs(r[1])); err != nil {
			return "", err
		}
		os.Remove(t.abs(path.Dir(r[0]))) // drop the source dir if now empty
	}

	if delta := destDepth - t.depthOf(sp); delta != 0 {
		for _, r := range renames {
			if err := shiftHeadingsBelow(t.abs(r[1]), delta); err != nil {
				return "", err
			}
		}
	}
	return newPath, nil
}

// shiftHeadingsBelow shifts headings in the .qmd file at abs, or in all
// .qmd files below it if abs is a directory.
func shiftHeadingsBelow(abs string, delta int) error {
	return filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".qmd") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(p, ShiftHeadings(src, delta), 0o644)
	})
}

// CreatePage creates a new page as name/index.qmd under parent ("" for
// root) with the given title, ordered after the last ordered sibling. It
// returns the new page's path.
func (t *Tree) CreatePage(parent, name, title string) (string, error) {
	destDir, siblings, _, err := t.resolveParent(parent)
	if err != nil {
		return "", err
	}
	order := 0
	for _, p := range siblings {
		if p.Order != nil && *p.Order > order {
			order = *p.Order
		}
	}
	return t.newPageFile(destDir, name, title, order+1)
}

// CreatePageAfter creates a new page as name/index.qmd in the sibling
// group of the page at after, immediately following it. Like Move, it
// renumbers the whole group sequentially, so previously unordered
// siblings become ordered. It returns the new page's path.
func (t *Tree) CreatePageAfter(after, name, title string) (string, error) {
	ap := t.Find(after)
	if ap == nil {
		return "", fmt.Errorf("page %s not found", after)
	}
	dir := groupDir(after)
	rel, err := t.newPageFile(dir, name, title, 0)
	if err != nil {
		return "", err
	}
	n := 0
	for _, p := range t.group(dir) {
		n++
		if err := t.setOrder(p.Path, n); err != nil {
			return "", err
		}
		if p == ap {
			n++
			if err := t.setOrder(rel, n); err != nil {
				return "", err
			}
		}
	}
	return rel, nil
}

// newPageFile writes destDir/name/index.qmd with title and order
// frontmatter and returns its path.
func (t *Tree) newPageFile(destDir, name, title string, order int) (string, error) {
	name = strings.TrimSuffix(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"), ".qmd")
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("invalid page name %q", name)
	}
	dir := join(destDir, name)
	for _, taken := range []string{dir, dir + ".qmd"} {
		if _, err := os.Stat(t.abs(taken)); err == nil {
			return "", fmt.Errorf("%s already exists", taken)
		}
	}
	rel := dir + "/index.qmd"
	content := fmt.Sprintf("---\ntitle: %s\norder: %d\n---\n\n", strconv.Quote(title), order)
	if err := os.MkdirAll(filepath.Dir(t.abs(rel)), 0o755); err != nil {
		return "", err
	}
	return rel, os.WriteFile(t.abs(rel), []byte(content), 0o644)
}

// DeletePage moves the page — and, for a section, its whole directory —
// to the system trash, falling back to a _trash directory in the project.
func (t *Tree) DeletePage(p string) error {
	sp := t.Find(p)
	if sp == nil {
		return fmt.Errorf("page %s not found", p)
	}
	var victims []string
	if path.Base(p) == "index.qmd" && path.Dir(p) != "." {
		victims = []string{t.abs(path.Dir(p))}
	} else {
		victims = []string{t.abs(p)}
		if fi, err := os.Stat(t.abs(sp.Dir)); err == nil && fi.IsDir() {
			victims = append(victims, t.abs(sp.Dir))
		}
	}
	return trash(t.Root, victims)
}

// TrashCommands are tried in order to reach the system trash. Set the
// list to nil to force the in-project _trash fallback (tests do).
var TrashCommands = [][]string{{"gio", "trash"}, {"trash-put"}, {"trash"}}

// trash moves the given absolute paths to the system trash if a trash
// command is available, else into <root>/_trash.
func trash(root string, victims []string) error {
	for _, cmd := range TrashCommands {
		if _, err := exec.LookPath(cmd[0]); err != nil {
			continue
		}
		if exec.Command(cmd[0], append(cmd[1:], victims...)...).Run() == nil {
			return nil
		}
	}
	dir := filepath.Join(root, "_trash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, v := range victims {
		dst := filepath.Join(dir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), filepath.Base(v)))
		if err := os.Rename(v, dst); err != nil {
			return err
		}
	}
	return nil
}

// resolveParent resolves a parent page path ("" for root) to the directory
// that holds its children, the current child pages, and the children's depth.
func (t *Tree) resolveParent(parent string) (string, []*Page, int, error) {
	if parent == "" {
		return ".", t.Pages, 0, nil
	}
	pp := t.Find(parent)
	if pp == nil {
		return "", nil, 0, fmt.Errorf("parent %s not found", parent)
	}
	if pp.Dir == "" {
		return "", nil, 0, fmt.Errorf("%s cannot contain child pages", parent)
	}
	return pp.Dir, pp.Children, t.depthOf(pp) + 1, nil
}

// groupDir returns the directory of the sibling group a page path belongs to.
func groupDir(p string) string {
	d := path.Dir(p)
	if path.Base(p) == "index.qmd" && d != "." {
		return path.Dir(d)
	}
	return d
}

// group returns the pages of the sibling group for the given directory.
func (t *Tree) group(dir string) []*Page {
	if dir == "." {
		return t.Pages
	}
	for _, p := range t.Pages {
		if g := groupIn(p, dir); g != nil {
			return g
		}
	}
	return nil
}

func groupIn(p *Page, dir string) []*Page {
	if p.Dir == dir && len(p.Children) > 0 {
		return p.Children
	}
	for _, c := range p.Children {
		if g := groupIn(c, dir); g != nil {
			return g
		}
	}
	return nil
}

// depthOf returns the tree depth of a page (root pages have depth 0).
func (t *Tree) depthOf(target *Page) int {
	d, _ := depthIn(t.Pages, target, 0)
	return d
}

func depthIn(pages []*Page, target *Page, level int) (int, bool) {
	for _, p := range pages {
		if p == target {
			return level, true
		}
		if d, ok := depthIn(p.Children, target, level+1); ok {
			return d, true
		}
	}
	return 0, false
}

// setOrder writes order into the frontmatter of the page file at rel.
func (t *Tree) setOrder(rel string, order int) error {
	abs := t.abs(rel)
	src, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	out := SetOrder(src, order)
	if string(out) == string(src) {
		return nil
	}
	return os.WriteFile(abs, out, 0o644)
}

func (t *Tree) abs(rel string) string {
	return filepath.Join(t.Root, filepath.FromSlash(rel))
}

// join joins slash paths, treating "." as the root.
func join(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}
