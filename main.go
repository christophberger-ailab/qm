package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// fileEntry holds metadata for a single .qmd/.md file.
type fileEntry struct {
	relPath string // relative to doc root, forward-slash separated
	order   *int
}

// frontmatter is the minimal YAML front matter we care about.
type frontmatter struct {
	Order *int `yaml:"order"`
}

func main() {
	var docPath string
	flag.StringVar(&docPath, "path", ".", "Path to the Quarto document tree (default: current directory)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: quarto-order-to-chapter-list [--path <doc-tree>] <profile-yaml-name>")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	profileArg := args[0]

	// Ensure the _quarto- prefix is present (BOOKS.2)
	if !strings.HasPrefix(profileArg, "_quarto-") {
		profileArg = "_quarto-" + profileArg
	}

	absDocPath, err := filepath.Abs(docPath)
	if err != nil {
		fatalf("cannot resolve doc path: %v", err)
	}

	profilePath := resolveProfilePath(absDocPath, profileArg)

	profileName := stripYamlExt(filepath.Base(profilePath))
	baseFolder, variant := parseProfileName(profileName)

	baseFolderPath := filepath.Join(absDocPath, baseFolder)
	if _, err := os.Stat(baseFolderPath); os.IsNotExist(err) {
		fatalf("base folder %q not found in document tree %q", baseFolder, absDocPath)
	}

	entries, err := scanFiles(absDocPath, baseFolderPath, variant)
	if err != nil {
		fatalf("error scanning files: %v", err)
	}

	for _, e := range entries {
		if e.order == nil {
			fmt.Fprintf(os.Stderr, "warning: no order in frontmatter: %s\n", e.relPath)
		}
	}

	chapters := buildFolderChapters(absDocPath, baseFolderPath, baseFolder, entries)

	// Quarto requires the cover page to be "index.qmd" without a folder prefix.
	if len(chapters) > 0 {
		base := filepath.Base(chapters[0])
		if base != "index.qmd" && base != "index.md" {
			fatalf("Cover page: expected index.(q)md, got %s", base)
		}
		chapters[0] = base
	}

	if err := updateProfileYaml(profilePath, chapters); err != nil {
		fatalf("error updating profile yaml: %v", err)
	}

	fmt.Printf("Updated %s with %d chapters\n", profilePath, len(chapters))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func stripYamlExt(name string) string {
	if strings.HasSuffix(name, ".yaml") {
		return strings.TrimSuffix(name, ".yaml")
	}
	return strings.TrimSuffix(name, ".yml")
}

// parseProfileName derives the base folder and optional variant (fw/pol) from a profile name.
// Profile names follow the pattern `_quarto-<folder>`, `_quarto-<folder>-fw`, or `_quarto-<folder>-pol`.
func parseProfileName(name string) (baseFolder, variant string) {
	const prefix = "_quarto-"
	if !strings.HasPrefix(name, prefix) {
		fatalf("profile name %q does not start with %q", name, prefix)
	}
	name = strings.TrimPrefix(name, prefix)

	if strings.HasSuffix(name, "-fw") {
		return strings.TrimSuffix(name, "-fw"), "fw"
	}
	if strings.HasSuffix(name, "-pol") {
		return strings.TrimSuffix(name, "-pol"), "pol"
	}
	return name, ""
}

// resolveProfilePath finds the profile yaml file in the doc root, trying .yaml/.yml extensions.
func resolveProfilePath(docRoot, arg string) string {
	if strings.HasSuffix(arg, ".yaml") || strings.HasSuffix(arg, ".yml") {
		return filepath.Join(docRoot, arg)
	}
	for _, ext := range []string{".yaml", ".yml"} {
		p := filepath.Join(docRoot, arg+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(docRoot, arg+".yaml")
}

// scanFiles recursively finds all .qmd/.md files in baseFolderPath, reads their
// order frontmatter, and applies variant filtering if a variant is specified.
func scanFiles(docRoot, baseFolderPath, variant string) (map[string]*fileEntry, error) {
	entries := make(map[string]*fileEntry)

	err := filepath.WalkDir(baseFolderPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".qmd" && ext != ".md" {
			return nil
		}

		relPath, err := filepath.Rel(docRoot, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		order, err := readOrder(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not read frontmatter from %s: %v\n", relPath, err)
		}

		entries[relPath] = &fileEntry{relPath: relPath, order: order}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if variant != "" {
		entries = applyVariantFilter(entries, variant)
	}

	return entries, nil
}

// applyVariantFilter implements BOOKS.4: for profiles with -fw or -pol suffix,
// plain files are included as-is, and when both _FW and _POL variants of the
// same file exist, only the matching variant is kept.
func applyVariantFilter(entries map[string]*fileEntry, profileVariant string) map[string]*fileEntry {
	type group struct {
		plain *fileEntry
		fw    *fileEntry
		pol   *fileEntry
	}
	groups := make(map[string]*group) // keyed by normalized (no-suffix) relPath

	for relPath, entry := range entries {
		dir := filepath.ToSlash(filepath.Dir(relPath))
		name := filepath.Base(relPath)
		ext := filepath.Ext(name)
		nameNoExt := strings.TrimSuffix(name, ext)

		baseKey := relPath
		fileVariant := ""

		if strings.HasSuffix(nameNoExt, "_FW") {
			baseName := strings.TrimSuffix(nameNoExt, "_FW") + ext
			baseKey = dir + "/" + baseName
			fileVariant = "fw"
		} else if strings.HasSuffix(nameNoExt, "_POL") {
			baseName := strings.TrimSuffix(nameNoExt, "_POL") + ext
			baseKey = dir + "/" + baseName
			fileVariant = "pol"
		}

		if groups[baseKey] == nil {
			groups[baseKey] = &group{}
		}
		switch fileVariant {
		case "fw":
			groups[baseKey].fw = entry
		case "pol":
			groups[baseKey].pol = entry
		default:
			groups[baseKey].plain = entry
		}
	}

	result := make(map[string]*fileEntry)
	for _, g := range groups {
		if g.fw != nil || g.pol != nil {
			// BOOKS.4-2: when variant files exist, pick the matching one.
			var chosen *fileEntry
			switch profileVariant {
			case "fw":
				chosen = g.fw
			case "pol":
				chosen = g.pol
			}
			if chosen != nil {
				result[chosen.relPath] = chosen
			}
			// A plain file alongside variants is superseded — omit it.
		} else if g.plain != nil {
			// BOOKS.4-1: plain files with no variants are always included.
			result[g.plain.relPath] = g.plain
		}
	}

	return result
}

// readOrder opens a .qmd/.md file and extracts the `order` field from its YAML frontmatter.
func readOrder(path string) (*int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	if !scanner.Scan() {
		return nil, nil
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return nil, nil // no frontmatter
	}

	var sb strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" || trimmed == "..." {
			break
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(sb.String()), &fm); err != nil {
		return nil, err
	}
	return fm.Order, nil
}

// buildFolderChapters recursively produces the flat ordered chapter list for a folder.
//
// Ordering rules (ORDERS):
//   - index.qmd is always listed first within its folder.
//   - Other files and subfolders are interleaved by their order value.
//   - A subfolder's position is determined by its own index.qmd's order value.
//   - Files/folders without an order value are appended at the end, unordered.
func buildFolderChapters(docRoot, folderPath, relFolder string, entries map[string]*fileEntry) []string {
	var result []string

	// index.qmd is always first.
	indexRelPath := relFolder + "/index.qmd"
	if e, ok := entries[indexRelPath]; ok {
		result = append(result, e.relPath)
	} else {
		// Also try index.md
		indexMdRelPath := relFolder + "/index.md"
		if e, ok := entries[indexMdRelPath]; ok {
			result = append(result, e.relPath)
		}
	}

	type item struct {
		order   int
		ordered bool
		isDir   bool
		relPath string // for files: full relPath; for dirs: rel dir path
	}

	dirEntries, err := os.ReadDir(folderPath)
	if err != nil {
		return result
	}

	var items []item
	for _, de := range dirEntries {
		name := de.Name()

		if de.IsDir() {
			subRelFolder := relFolder + "/" + name
			subIndexRelPath := subRelFolder + "/index.qmd"
			e, ok := entries[subIndexRelPath]
			if !ok {
				// Try index.md
				e, ok = entries[subRelFolder+"/index.md"]
			}
			if !ok {
				// No index file found for this subfolder; skip it.
				continue
			}
			it := item{isDir: true, relPath: subRelFolder}
			if e.order != nil {
				it.order = *e.order
				it.ordered = true
			}
			items = append(items, it)
		} else {
			// Skip index files (already handled above).
			nameNoExt := strings.TrimSuffix(name, filepath.Ext(name))
			if nameNoExt == "index" {
				continue
			}
			ext := strings.ToLower(filepath.Ext(name))
			if ext != ".qmd" && ext != ".md" {
				continue
			}
			relPath := relFolder + "/" + name
			e, ok := entries[relPath]
			if !ok {
				continue // filtered out by variant logic
			}
			it := item{isDir: false, relPath: relPath}
			if e.order != nil {
				it.order = *e.order
				it.ordered = true
			}
			items = append(items, it)
		}
	}

	// Sort: ordered items by order value first, then unordered items at the end.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ordered && items[j].ordered {
			return items[i].order < items[j].order
		}
		if items[i].ordered {
			return true
		}
		return false
	})

	for _, it := range items {
		if it.isDir {
			subFolderPath := filepath.Join(docRoot, filepath.FromSlash(it.relPath))
			result = append(result, buildFolderChapters(docRoot, subFolderPath, it.relPath, entries)...)
		} else {
			result = append(result, it.relPath)
		}
	}

	return result
}

// updateProfileYaml reads the profile yaml, sets book.chapters to the given list, and writes it back.
func updateProfileYaml(profilePath string, chapters []string) error {
	var root yaml.Node

	data, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot read %q: %w", profilePath, err)
	}

	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("cannot parse %q: %w", profilePath, err)
		}
	}

	// Ensure we have a document node with a mapping root.
	if root.Kind == 0 {
		root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
			{Kind: yaml.MappingNode},
		}}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("unexpected yaml structure in %q", profilePath)
	}
	mappingRoot := root.Content[0]

	// Find or create the `book` mapping node.
	bookNode := findMappingValue(mappingRoot, "book")
	if bookNode == nil {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "book"}
		bookNode = &yaml.Node{Kind: yaml.MappingNode}
		mappingRoot.Content = append(mappingRoot.Content, keyNode, bookNode)
	}

	// Build the new chapters sequence node.
	chaptersSeq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, ch := range chapters {
		chaptersSeq.Content = append(chaptersSeq.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: ch,
		})
	}

	// Replace or add the `chapters` key inside `book`.
	if !replaceMappingValue(bookNode, "chapters", chaptersSeq) {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "chapters"}
		bookNode.Content = append(bookNode.Content, keyNode, chaptersSeq)
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("cannot marshal yaml: %w", err)
	}

	return os.WriteFile(profilePath, out, 0644)
}

// findMappingValue returns the value node for the given key in a YAML mapping node.
func findMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// replaceMappingValue replaces the value for key in a YAML mapping node.
// Returns true if the key was found and replaced.
func replaceMappingValue(parent *yaml.Node, key string, newValue *yaml.Node) bool {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1] = newValue
			return true
		}
	}
	return false
}
