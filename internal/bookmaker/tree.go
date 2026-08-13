package bookmaker

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// audienceSuffix matches the agency markers that Quarto projects use on
// folder and file names, e.g. `betriebszustaende_FW`, `sonderlagen_POL`.
var audienceSuffix = regexp.MustCompile(`(?i)_(fw|pol)$`)

// Page is a single source .qmd file: its front matter and its body with the
// front matter removed.
type Page struct {
	// File is the absolute path of the source file.
	File string
	// Rel is the path relative to the project root, using forward slashes.
	Rel string
	FrontMatter
	// Body is the file content with any front-matter block stripped.
	Body string
}

// Node is one entry in the book tree. It is either a directory (which may
// carry an index.qmd as its own content) or a standalone page.
type Node struct {
	// Dir is the absolute directory path for directory nodes, empty for
	// standalone page nodes.
	Dir string
	// Name is the directory name, or the file name without its extension.
	Name string
	// Rel is the node's path relative to the project root, forward-slashed
	// and without a trailing slash.
	Rel string
	// Level is the heading level this node's content is normalised to.
	Level int
	// Audience is "fw", "pol" or "" and is derived from the name suffix.
	Audience string
	// Anchor is the unique heading identifier this node's page carries in
	// the flattened document. It is filled in by assignAnchors.
	Anchor string
	// Page holds the node's content. It is nil for a directory that has no
	// index.qmd.
	Page *Page
	// Children are the node's sub-entries in render order.
	Children []*Node

	// bookRoot marks the node whose heading is the book's title rather
	// than a chapter of it. Flatten sets it on the tree root.
	bookRoot bool
	// titleOverride replaces the node's title, carrying the --title flag.
	titleOverride string
}

// IsDir reports whether the node is a directory node.
func (n *Node) IsDir() bool { return n.Dir != "" }

// sortKey returns the value used to order this node among its siblings.
// A directory takes the `order:` of its index.qmd, which is exactly Quarto's
// rule that an index page sorts at its parent's level.
func (n *Node) sortKey() int {
	if n.Page == nil {
		return noOrder
	}
	return n.Page.SortKey()
}

// Title returns the node's display title: an explicit override if one was
// given, otherwise the `title:` front-matter field, otherwise a title
// derived from the directory or file name.
func (n *Node) Title() string {
	if n.titleOverride != "" {
		return n.titleOverride
	}
	if n.Page != nil && n.Page.Title != "" {
		return n.Page.Title
	}
	return humanise(n.Name)
}

// Walk visits the node and all of its descendants in render order.
func (n *Node) Walk(fn func(*Node)) {
	fn(n)
	for _, c := range n.Children {
		c.Walk(fn)
	}
}

// LoadTree reads the book folder at dir and returns its root node.
//
// projectRoot is used to compute the Rel paths that in-book links are
// resolved against. Hidden entries and entries whose name starts with "_"
// are skipped, matching Quarto's own rules for what counts as project
// content.
func LoadTree(dir, projectRoot string) (*Node, error) {
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	return prune(loadDir(dir, projectRoot, 0))
}

// prune drops directory nodes whose subtree holds no .qmd at all. Book
// folders commonly contain media directories such as `images/`, which carry
// no content and must not become empty chapters.
func prune(node *Node, err error) (*Node, error) {
	if err != nil {
		return nil, err
	}
	kept := node.Children[:0]
	for _, child := range node.Children {
		c, err := prune(child, nil)
		if err != nil {
			return nil, err
		}
		if c != nil {
			kept = append(kept, c)
		}
	}
	node.Children = kept

	if node.IsDir() && node.Page == nil && len(node.Children) == 0 {
		return nil, nil
	}
	return node, nil
}

// levelForDepth maps a folder depth relative to the book root onto a heading
// level. The book root's own index page and the first level of subfolders
// both become chapters (level 1); every further level of nesting adds one
// heading level. Levels are capped at 6, the deepest heading Markdown has.
func levelForDepth(depth int) int {
	switch {
	case depth < 1:
		return 1
	case depth > 6:
		return 6
	default:
		return depth
	}
}

func loadDir(dir, projectRoot string, depth int) (*Node, error) {
	rel, err := relSlash(projectRoot, dir)
	if err != nil {
		return nil, err
	}
	level := levelForDepth(depth)

	node := &Node{
		Dir:      dir,
		Name:     filepath.Base(dir),
		Rel:      rel,
		Level:    level,
		Audience: audienceOf(filepath.Base(dir)),
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	childLevel := levelForDepth(depth + 1)

	for _, e := range entries {
		name := e.Name()
		if skipEntry(name) {
			continue
		}

		if e.IsDir() {
			child, err := loadDir(filepath.Join(dir, name), projectRoot, depth+1)
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, child)
			continue
		}

		if !strings.EqualFold(filepath.Ext(name), ".qmd") {
			continue
		}

		page, err := loadPage(filepath.Join(dir, name), projectRoot)
		if err != nil {
			return nil, err
		}

		if isIndexName(name) {
			node.Page = page
			continue
		}

		base := strings.TrimSuffix(name, filepath.Ext(name))
		node.Children = append(node.Children, &Node{
			Name:     base,
			Rel:      joinRel(rel, base),
			Level:    childLevel,
			Audience: audienceOf(base),
			Page:     page,
		})
	}

	sortNodes(node.Children)
	return node, nil
}

// sortNodes orders siblings by their `order:` front-matter field, falling
// back to a case-insensitive name comparison for ties and for entries
// without an order.
func sortNodes(nodes []*Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		ki, kj := nodes[i].sortKey(), nodes[j].sortKey()
		if ki != kj {
			return ki < kj
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
}

func loadPage(file, projectRoot string) (*Page, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	fm, body, err := parseFrontMatter(normaliseNewlines(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	rel, err := relSlash(projectRoot, file)
	if err != nil {
		return nil, err
	}
	return &Page{File: file, Rel: rel, FrontMatter: fm, Body: body}, nil
}

// skipEntry reports whether a directory entry is invisible to Quarto.
func skipEntry(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// BookFolders lists the directories directly below dir that hold book
// content, in name order. It applies the same rules as LoadTree: hidden
// entries and entries starting with "_" are invisible to Quarto, and a
// directory without a .qmd anywhere below it is media (`assets/`,
// `images/`), not a book. A directory carrying its own Quarto project
// configuration is a project of its own and is left alone too.
func BookFolders(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var folders []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || skipEntry(name) {
			continue
		}
		sub := filepath.Join(dir, name)
		if HasProjectConfig(sub) || !containsQMD(sub) {
			continue
		}
		folders = append(folders, sub)
	}
	return folders, nil
}

// containsQMD reports whether dir holds a .qmd file anywhere below it,
// ignoring the entries Quarto itself ignores.
func containsQMD(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if skipEntry(name) {
			continue
		}
		if e.IsDir() {
			if containsQMD(filepath.Join(dir, name)) {
				return true
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(name), ".qmd") {
			return true
		}
	}
	return false
}

// isIndexName reports whether a file name is a Quarto index page.
func isIndexName(name string) bool {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return strings.EqualFold(base, "index")
}

// audienceOf extracts the agency marker from a folder or file name.
func audienceOf(name string) string {
	m := audienceSuffix.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return strings.ToLower(m[1])
}

// humanise turns a slug like "einsatz-starten" into "Einsatz starten" so
// that pages without a title still get a readable heading.
func humanise(name string) string {
	name = audienceSuffix.ReplaceAllString(name, "")
	name = strings.NewReplacer("-", " ", "_", " ").Replace(name)
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	runes := []rune(name)
	return strings.ToUpper(string(runes[0])) + string(runes[1:])
}

func normaliseNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func relSlash(base, target string) (string, error) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func joinRel(dir, name string) string {
	if dir == "" || dir == "." {
		return name
	}
	return dir + "/" + name
}

// FindProjectRoot walks up from start looking for the directory that holds
// the Quarto project configuration. It returns an empty string when no
// `_quarto.yml` / `_quarto.yaml` is found.
func FindProjectRoot(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		for _, name := range []string{"_quarto.yml", "_quarto.yaml"} {
			if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// HasProjectConfig reports whether dir itself contains a Quarto project
// configuration file.
func HasProjectConfig(dir string) bool {
	for _, name := range []string{"_quarto.yml", "_quarto.yaml"} {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}
