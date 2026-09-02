package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
)

// completeEntry is one candidate the editor's path completion offers: a
// name to show and to insert, and whether it is a directory the user can
// still descend into.
type completeEntry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

// complete answers the editor's path completion popup: the entries of a
// single directory inside the open project, filtered to what a Markdown
// link of the given kind can point at.
//
// dir is project-relative, "." for the root; kind is "image" or "page".
// Directories are always included, so the user can descend into them
// regardless of what the link ends up pointing at.
func (s *server) complete(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "."
	}
	abs, err := s.resolvePath(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "image" && kind != "page" {
		http.Error(w, "kind must be image or page", http.StatusBadRequest)
		return
	}

	items, err := os.ReadDir(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var entries []completeEntry
	for _, it := range items {
		name := it.Name()
		// The same rule the search index and the page tree use: a
		// directory or file whose name starts with "_" or "." is a draft
		// or hidden, and does not belong in a link.
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		if it.IsDir() {
			entries = append(entries, completeEntry{Name: name, Dir: true})
			continue
		}
		ext := strings.ToLower(path.Ext(name))
		switch kind {
		case "image":
			if !mediaExts[ext] {
				continue
			}
		case "page":
			if ext != ".qmd" {
				continue
			}
		}
		entries = append(entries, completeEntry{Name: name})
	}
	slices.SortFunc(entries, func(a, b completeEntry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Entries []completeEntry `json:"entries"`
	}{entries})
}
