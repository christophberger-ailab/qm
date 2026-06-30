package qmcore

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReadOrder opens a .qmd/.md file and extracts the `order` field from
// its YAML frontmatter.
func ReadOrder(path string) (*int, error) {
	fm, err := ReadFrontmatter(path)
	if err != nil {
		return nil, err
	}
	return fm.Order, nil
}

// ReadFrontmatter reads the YAML frontmatter from a .qmd/.md file. If the
// file has no frontmatter, a zero-value Frontmatter is returned with no error.
func ReadFrontmatter(path string) (Frontmatter, error) {
	body, err := extractFrontmatter(path)
	if err != nil {
		return Frontmatter{}, err
	}
	if body == "" {
		return Frontmatter{}, nil
	}

	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(body), &fm); err != nil {
		return Frontmatter{}, err
	}
	return fm, nil
}

// extractFrontmatter returns the text between the leading `---` and the next
// `---` or `...` terminator. Returns an empty string when no frontmatter is
// found.
func extractFrontmatter(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return "", nil
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return "", nil
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
	return sb.String(), scanner.Err()
}

// ScanFiles recursively finds all .qmd/.md files in baseFolderPath, reads
// their frontmatter, applies variant filtering, and drops excluded files.
func ScanFiles(docRoot, baseFolderPath, variant string, excludePattern *regexp.Regexp) (map[string]*FileEntry, error) {
	entries := make(map[string]*FileEntry)

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

		if ShouldExcludeChapter(relPath, excludePattern) {
			return nil
		}

		order, err := ReadOrder(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not read frontmatter from %s: %v\n", relPath, err)
		}

		entries[relPath] = &FileEntry{RelPath: relPath, Order: order}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if variant != "" {
		entries = ApplyVariantFilter(entries, variant)
	}

	return entries, nil
}

// PathFolderVariant inspects the folder segments of relPath and returns the
// variant suffix found on any of them ("fw", "pol", or ""). Deepest wins.
func PathFolderVariant(relPath string) string {
	segments := strings.Split(filepath.ToSlash(relPath), "/")
	if len(segments) < 2 {
		return ""
	}
	variant := ""
	for _, seg := range segments[:len(segments)-1] {
		switch {
		case strings.HasSuffix(seg, "_FW"):
			variant = "fw"
		case strings.HasSuffix(seg, "_POL"):
			variant = "pol"
		}
	}
	return variant
}

// ApplyVariantFilter implements BOOKS.4: for profiles with -fw or -pol suffix,
// plain files are included as-is, and variant files are filtered to the
// matching variant. Folder name suffixes are treated identically (BOOKS.3-1).
func ApplyVariantFilter(entries map[string]*FileEntry, profileVariant string) map[string]*FileEntry {
	filtered := make(map[string]*FileEntry, len(entries))
	for relPath, entry := range entries {
		folderVariant := PathFolderVariant(relPath)
		if folderVariant != "" && folderVariant != profileVariant {
			continue
		}
		filtered[relPath] = entry
	}
	entries = filtered

	variantSuffix := ""
	switch profileVariant {
	case "fw":
		variantSuffix = "_FW"
	case "pol":
		variantSuffix = "_POL"
	}
	if variantSuffix != "" {
		folders := make(map[string]bool)
		for relPath := range entries {
			parts := strings.Split(relPath, "/")
			for i := 0; i < len(parts)-1; i++ {
				folders[strings.Join(parts[:i+1], "/")] = true
			}
		}
		superseded := make(map[string]bool)
		for folder := range folders {
			base := filepath.Base(folder)
			if strings.HasSuffix(base, "_FW") || strings.HasSuffix(base, "_POL") {
				continue
			}
			variantFolder := folder + variantSuffix
			if folders[variantFolder] {
				superseded[folder] = true
				fmt.Fprintf(os.Stderr, "warning: folder %s superseded by %s\n", folder, variantFolder)
			}
		}
		if len(superseded) > 0 {
			next := make(map[string]*FileEntry, len(entries))
			for relPath, entry := range entries {
				skip := false
				parts := strings.Split(relPath, "/")
				for i := 0; i < len(parts)-1; i++ {
					if superseded[strings.Join(parts[:i+1], "/")] {
						skip = true
						break
					}
				}
				if !skip {
					next[relPath] = entry
				}
			}
			entries = next
		}
	}

	type group struct {
		plain *FileEntry
		fw    *FileEntry
		pol   *FileEntry
	}
	groups := make(map[string]*group)

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

	result := make(map[string]*FileEntry)
	for _, g := range groups {
		if g.fw != nil || g.pol != nil {
			var chosen *FileEntry
			switch profileVariant {
			case "fw":
				chosen = g.fw
			case "pol":
				chosen = g.pol
			}
			if chosen != nil {
				result[chosen.RelPath] = chosen
			}
			if g.plain != nil {
				fmt.Fprintf(os.Stderr, "warning: file %s superseded by variant counterpart\n", g.plain.RelPath)
			}
		} else if g.plain != nil {
			result[g.plain.RelPath] = g.plain
		}
	}

	return result
}

// rewriteFrontmatterRegex matches an existing frontmatter block at the start
// of a file. Group 1 is the block content between the delimiters.
var rewriteFrontmatterRegex = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n(?:---|\.\.\.)\r?\n`)

// UpdateFrontmatter rewrites a file's frontmatter. It parses the existing
// frontmatter block as YAML, calls mutate to modify it, then writes the file
// back with the new frontmatter. The body content is preserved verbatim.
//
// If the file has no frontmatter, a new one is created and prepended.
func UpdateFrontmatter(path string, mutate func(*yaml.Node) error) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var fmYAML []byte
	var body []byte
	if m := rewriteFrontmatterRegex.FindSubmatchIndex(data); m != nil {
		fmYAML = data[m[2]:m[3]]
		body = data[m[1]:]
	} else {
		fmYAML = []byte{}
		body = data
	}

	var node yaml.Node
	if len(bytes.TrimSpace(fmYAML)) > 0 {
		if err := yaml.Unmarshal(fmYAML, &node); err != nil {
			return fmt.Errorf("parse frontmatter: %w", err)
		}
	}
	if node.Kind == 0 {
		node = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
			{Kind: yaml.MappingNode},
		}}
	}
	if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		return fmt.Errorf("unexpected frontmatter structure in %s", path)
	}

	if err := mutate(node.Content[0]); err != nil {
		return err
	}

	var out bytes.Buffer
	out.WriteString("---\n")
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(node.Content[0]); err != nil {
		return err
	}
	enc.Close()
	out.WriteString("---\n")
	out.Write(body)

	return os.WriteFile(path, out.Bytes(), 0644)
}
