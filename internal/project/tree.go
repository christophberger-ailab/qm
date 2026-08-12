package project

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Page is a node in the project tree. A page without an Order field is
// "unordered" and sorts below its ordered siblings. Dir is the directory
// that holds (or would hold) the page's children; it is empty when the
// page cannot have children (the root index.qmd).
type Page struct {
	Path      string // slash-separated path relative to the project root
	Dir       string
	Title     string
	Order     *int
	BadFences bool   // the file has unmatched Quarto block fences
	Marker    string // emoji marker from a _FW/_POL suffix (own or inherited)
	Children  []*Page
}

// Emoji markers for the _FW and _POL name suffixes.
const (
	markerFW  = "\U0001F692" // 🚒
	markerPOL = "\U0001F693" // 🚔
)

// Tree is the page tree of a Quarto project.
type Tree struct {
	Root  string // absolute path of the project
	Pages []*Page
}

// Load scans root for .qmd files and builds the page tree.
//
// index.qmd inside a directory represents that directory's section and is
// ordered among the pages one level up, as is a name.qmd file next to a
// name/ directory. Directories starting with "_" or "." are ignored.
func Load(root string) (*Tree, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".qmd") && !strings.HasPrefix(name, "_") {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Directories that (transitively) contain .qmd files.
	hasFiles := map[string]bool{}
	for _, f := range files {
		for d := path.Dir(f); d != "."; d = path.Dir(d) {
			hasFiles[d] = true
		}
	}

	pages := make(map[string]*Page, len(files))
	for _, f := range files {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			return nil, err
		}
		fm := ParseFrontmatter(src)
		p := &Page{Path: f, Title: fm.Title, Order: fm.Order, BadFences: !BalancedFences(src)}
		if p.Title == "" {
			if h := FirstHeading(src); h != "" {
				p.Title = h
			} else {
				p.Title = fallbackTitle(f)
			}
		}
		pages[f] = p
	}

	// A section page owns a directory: name/index.qmd wins over name.qmd.
	sections := map[string]*Page{}
	for f, p := range pages {
		if path.Base(f) == "index.qmd" && path.Dir(f) != "." {
			sections[path.Dir(f)] = p
		}
	}
	for f, p := range pages {
		d := strings.TrimSuffix(f, ".qmd")
		if path.Base(f) != "index.qmd" && hasFiles[d] && sections[d] == nil {
			sections[d] = p
		}
	}

	// Group pages by the directory whose section they belong to.
	groups := map[string][]*Page{}
	for f, p := range pages {
		dir := path.Dir(f)
		if base := path.Base(f); base == "index.qmd" {
			if dir == "." {
				p.Dir = "" // root index.qmd takes no children
			} else {
				p.Dir = dir
				dir = path.Dir(dir) // section sorts among its parent's pages
			}
		} else {
			p.Dir = strings.TrimSuffix(f, ".qmd")
			if sections[p.Dir] == p {
				dir = path.Dir(p.Dir)
			}
		}
		groups[dir] = append(groups[dir], p)
	}

	// Directories with pages but no section page get a synthetic node.
	for d := range groups {
		for ; d != "."; d = path.Dir(d) {
			if sections[d] == nil {
				s := &Page{Dir: d, Title: d + "/"}
				sections[d] = s
				groups[path.Dir(d)] = append(groups[path.Dir(d)], s)
			}
		}
	}

	for d, s := range sections {
		s.Children = groups[d]
		sortPages(s.Children)
	}
	t := &Tree{Root: root, Pages: groups["."]}
	sortPages(t.Pages)
	markPages(t.Pages, "")
	return t, nil
}

// ownMarker returns the emoji marker implied by a page's own name suffix, or
// "" if the name has neither the _FW nor the _POL suffix. Section and
// directory nodes match on their directory name; plain files on the base
// name before the .qmd extension.
func ownMarker(p *Page) string {
	var name string
	if p.Path == "" || path.Base(p.Path) == "index.qmd" {
		name = path.Base(p.Dir)
	} else {
		name = strings.TrimSuffix(path.Base(p.Path), ".qmd")
	}
	switch {
	case strings.HasSuffix(name, "_FW"):
		return markerFW
	case strings.HasSuffix(name, "_POL"):
		return markerPOL
	}
	return ""
}

// markPages assigns each page its emoji marker. A page's own _FW/_POL suffix
// wins; otherwise it inherits the marker of its nearest marked ancestor.
func markPages(pages []*Page, inherited string) {
	for _, p := range pages {
		m := ownMarker(p)
		if m == "" {
			m = inherited
		}
		p.Marker = m
		markPages(p.Children, m)
	}
}

func fallbackTitle(f string) string {
	if path.Base(f) == "index.qmd" {
		if d := path.Dir(f); d != "." {
			return path.Base(d)
		}
	}
	return strings.TrimSuffix(path.Base(f), ".qmd")
}

// sortPages orders siblings: pages with an order field first (by order,
// then path), unordered pages after them (by path).
func sortPages(ps []*Page) {
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		switch {
		case a.Order != nil && b.Order != nil:
			if *a.Order != *b.Order {
				return *a.Order < *b.Order
			}
			return a.Path < b.Path
		case a.Order != nil:
			return true
		case b.Order != nil:
			return false
		default:
			return a.Path < b.Path
		}
	})
}

// Find returns the page with the given path, or nil.
func (t *Tree) Find(p string) *Page {
	return find(t.Pages, p)
}

func find(pages []*Page, p string) *Page {
	for _, pg := range pages {
		if pg.Path == p && p != "" {
			return pg
		}
		if hit := find(pg.Children, p); hit != nil {
			return hit
		}
	}
	return nil
}

// Profiles returns the profile names of the _quarto-<name>.yml files in
// root that configure a book (contain a top-level book key).
func Profiles(root string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(root, "_quarto-*.yml"))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, m := range matches {
		src, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		if !hasBook(src) {
			continue
		}
		names = append(names, strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m), "_quarto-"), ".yml"))
	}
	sort.Strings(names)
	return names, nil
}
