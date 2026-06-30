package qmcore

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BuildFolderChapters recursively produces the flat ordered chapter list
// for a folder.
//
// Ordering rules (ORDERS):
//   - index.qmd is always listed first within its folder.
//   - Other files and subfolders are interleaved by their order value.
//   - A subfolder's position is determined by its index.qmd's order value.
//   - Files/folders without an order value are appended at the end.
func BuildFolderChapters(docRoot, folderPath, relFolder string, entries map[string]*FileEntry) []string {
	var result []string

	// index file is always first.
	indexRelPath := relFolder + "/index.qmd"
	if e, ok := entries[indexRelPath]; ok {
		result = append(result, e.RelPath)
	} else {
		indexMdRelPath := relFolder + "/index.md"
		if e, ok := entries[indexMdRelPath]; ok {
			result = append(result, e.RelPath)
		}
	}

	type item struct {
		order   int
		ordered bool
		isDir   bool
		relPath string
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
				e, ok = entries[subRelFolder+"/index.md"]
			}
			if !ok {
				continue
			}
			it := item{isDir: true, relPath: subRelFolder}
			if e.Order != nil {
				it.order = *e.Order
				it.ordered = true
			}
			items = append(items, it)
		} else {
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
				continue
			}
			it := item{isDir: false, relPath: relPath}
			if e.Order != nil {
				it.order = *e.Order
				it.ordered = true
			}
			items = append(items, it)
		}
	}

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
			result = append(result, BuildFolderChapters(docRoot, subFolderPath, it.relPath, entries)...)
		} else {
			result = append(result, it.relPath)
		}
	}

	return result
}

// ScanFolderChapters lists the direct chapter items in a single folder
// (non-recursive). Used by the insert/move/remove subcommands which need to
// reason about a single folder's order list.
//
// Each result entry is either:
//   - a .qmd/.md file directly in folderPath (excluding index.qmd/index.md), or
//   - a subfolder of folderPath that contains an index.qmd/index.md file.
//
// The returned list is sorted by order value; unordered entries are appended
// last (deterministic, by RelPath).
func ScanFolderChapters(docRoot, folderPath string) ([]ChapterItem, error) {
	absDocRoot, err := filepath.Abs(docRoot)
	if err != nil {
		return nil, err
	}
	absFolder, err := filepath.Abs(folderPath)
	if err != nil {
		return nil, err
	}
	relFolder, err := filepath.Rel(absDocRoot, absFolder)
	if err != nil {
		return nil, err
	}
	relFolder = filepath.ToSlash(relFolder)

	dirEntries, err := os.ReadDir(absFolder)
	if err != nil {
		return nil, err
	}

	var items []ChapterItem
	for _, de := range dirEntries {
		name := de.Name()
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}

		if de.IsDir() {
			indexPath := ""
			for _, candidate := range []string{"index.qmd", "index.md"} {
				p := filepath.Join(absFolder, name, candidate)
				if _, err := os.Stat(p); err == nil {
					indexPath = p
					break
				}
			}
			if indexPath == "" {
				continue
			}
			fm, err := ReadFrontmatter(indexPath)
			if err != nil {
				return nil, err
			}
			rel, _ := filepath.Rel(absDocRoot, indexPath)
			items = append(items, ChapterItem{
				RelPath: filepath.ToSlash(rel),
				Order:   fm.Order,
				Title:   fm.Title,
				IsDir:   true,
			})
			continue
		}

		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".qmd" && ext != ".md" {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if base == "index" {
			continue
		}

		filePath := filepath.Join(absFolder, name)
		fm, err := ReadFrontmatter(filePath)
		if err != nil {
			return nil, err
		}
		rel := relFolder
		if rel == "." || rel == "" {
			rel = name
		} else {
			rel = rel + "/" + name
		}
		items = append(items, ChapterItem{
			RelPath: rel,
			Order:   fm.Order,
			Title:   fm.Title,
			IsDir:   false,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		switch {
		case items[i].Order != nil && items[j].Order != nil:
			if *items[i].Order != *items[j].Order {
				return *items[i].Order < *items[j].Order
			}
			return items[i].RelPath < items[j].RelPath
		case items[i].Order != nil:
			return true
		case items[j].Order != nil:
			return false
		default:
			return items[i].RelPath < items[j].RelPath
		}
	})

	return items, nil
}

// ChapterFilePath returns the on-disk file path (relative to docRoot) that
// carries the frontmatter for the given chapter item. For a file chapter,
// this is the file itself; for a directory chapter, it is the subfolder's
// index file.
func ChapterFilePath(item ChapterItem) string {
	return item.RelPath
}
