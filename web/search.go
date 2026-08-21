package web

import (
	"fmt"
	iofs "io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// searchIndex is the word index the search field is answered from. Reading
// every page of a book-sized project takes long enough to be felt between
// two keystrokes, so the index is built in the background: a query is
// answered from whatever the index already holds, and the client asks again
// while a build is running.
//
// It has its own lock, like the render job: a build must not block the tree
// handlers, and it must not hold the server's lock while it reads the disk.
type searchIndex struct {
	mu sync.Mutex

	// want is the project state the index should describe; root and fp are
	// the one it does describe. They differ while a build is pending.
	want     indexState
	root     string
	fp       string
	ready    bool
	building bool

	// terms maps each word to the pages it appears on and how often.
	terms map[string]map[string]int
}

// indexState identifies what an index describes: a project, in the shape it
// had at a given fingerprint.
type indexState struct {
	root string
	fp   string
}

// searchHit is one page a query matched, with the number of matches on it.
type searchHit struct {
	Path  string
	Count int
}

// rebuild schedules a build for the given project state and returns at
// once. A build already running is not interrupted: it picks the new state
// up when it finishes, so a burst of saves costs one extra pass, not one
// per save.
func (x *searchIndex) rebuild(root, fp string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	want := indexState{root, fp}
	if x.ready && x.root == want.root && x.fp == want.fp {
		return
	}
	x.want = want
	if x.building {
		return
	}
	x.building = true
	go x.run()
}

// run reads the project and installs the result, repeating while the
// project has moved on in the meantime.
func (x *searchIndex) run() {
	for {
		x.mu.Lock()
		want := x.want
		x.mu.Unlock()

		terms := indexPages(want.root)

		x.mu.Lock()
		if x.want != want {
			x.mu.Unlock()
			continue // the project changed again while it was being read
		}
		x.root, x.fp, x.terms = want.root, want.fp, terms
		x.ready, x.building = true, false
		x.mu.Unlock()
		return
	}
}

// expire marks the index as describing a project state that is gone, so
// that the next rebuild is carried out even when the fingerprint did not
// move. The server calls it when it writes a page itself: an edit that
// keeps a page's size can land in the same fingerprint as the text it
// replaced, and a search that misses what the user just typed is worse than
// an extra pass over the project.
func (x *searchIndex) expire() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.fp = ""
}

// search returns the pages of root matching q, and whether the answer
// describes the project as it is now. An index that has not been built yet,
// or that describes a different project, has nothing to say at all; one
// with a build pending still answers -- its hits are a moment old, which
// beats clearing the tree's highlighting on every autosave -- but says so,
// and the client comes back for the new answer.
func (x *searchIndex) search(root, q string) ([]searchHit, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.ready || x.root != root {
		return nil, false
	}
	current := x.want == indexState{x.root, x.fp}
	return matchPages(x.terms, q), current
}

// indexPages counts the words of every page of the project. It covers the
// files the page tree covers -- .qmd files outside the "_" and "." folders,
// and not themselves prefixed with "_" -- so that every hit names a page
// the user can open from the tree. Unreadable files drop out of the index
// the way they drop out of the fingerprint.
func indexPages(root string) map[string]map[string]int {
	terms := map[string]map[string]int{}
	if root == "" {
		return terms
	}
	filepath.WalkDir(root, func(p string, d iofs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".qmd") || strings.HasPrefix(name, "_") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		page := filepath.ToSlash(rel)
		for _, term := range indexWords(string(src)) {
			pages := terms[term]
			if pages == nil {
				pages = map[string]int{}
				terms[term] = pages
			}
			pages[page]++
		}
		return nil
	})
	return terms
}

// indexWords splits text into the lowercased words the index is keyed by:
// runs of letters and digits, everything else a separator. Markdown and
// YAML punctuation therefore never becomes part of a word, and a page is
// searched as the text it reads as.
func indexWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// minTerm is the shortest query term that is searched for. A single letter
// starts almost every page and would highlight the whole tree after the
// first keystroke.
const minTerm = 2

// queryWords are the terms of a query worth searching for.
func queryWords(q string) []string {
	words := indexWords(q)
	return slices.DeleteFunc(words, func(w string) bool {
		return len([]rune(w)) < minTerm
	})
}

// matchPages finds the pages matching every term of q. A term matches the
// words that start with it, so a query narrows down while it is typed
// rather than finding nothing until its last letter.
func matchPages(terms map[string]map[string]int, q string) []searchHit {
	words := queryWords(q)
	if len(words) == 0 {
		return nil
	}
	var found map[string]int
	for _, w := range words {
		pages := map[string]int{}
		for term, counts := range terms {
			if !strings.HasPrefix(term, w) {
				continue
			}
			for page, n := range counts {
				pages[page] += n
			}
		}
		if found == nil {
			found = pages
			continue
		}
		// Every term has to match, so the terms after the first one only
		// ever remove pages -- and add their own hits to the survivors.
		for page, n := range found {
			if extra, ok := pages[page]; ok {
				found[page] = n + extra
			} else {
				delete(found, page)
			}
		}
	}
	hits := make([]searchHit, 0, len(found))
	for page, n := range found {
		hits = append(hits, searchHit{Path: page, Count: n})
	}
	slices.SortFunc(hits, func(a, b searchHit) int { return strings.Compare(a.Path, b.Path) })
	return hits
}

// searchView is what the search field shows: the summary next to it, and
// the hits the page tree highlights.
type searchView struct {
	Query    string
	Hits     []searchHit
	Summary  string
	Indexing bool
}

// search answers the search field. The hits are handed to the client as a
// list of page paths rather than as a result panel: the tree already shows
// every page, so it is the tree that highlights them.
func (s *server) search(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := r.URL.Query().Get("q")
	view := searchView{Query: q}
	if s.root != "" {
		hits, current := s.index.search(s.root, q)
		// An empty query needs no index: reporting it as still indexing
		// would put the client into a poll that nothing ends.
		view.Hits, view.Indexing = hits, !current && len(queryWords(q)) > 0
		if !current {
			// The project may have been opened without a page ever being
			// served -- `qm web <path>` does that -- so no build has been
			// asked for yet. Ask now; the client comes back for the answer.
			s.index.rebuild(s.root, s.fp)
		}
	}
	view.Summary = searchSummary(view)
	s.render(w, "search-results", view)
}

// searchSummary is the line beside the field. It says how much was found,
// because the hits themselves are shown in the tree, which may be scrolled
// away from all of them. A pending rebuild is only worth saying when there
// is nothing to show yet: hits a moment old still describe the project
// better than the word "indexing" does.
func searchSummary(v searchView) string {
	switch {
	case v.Indexing && len(v.Hits) == 0:
		return "Indexing…"
	case len(queryWords(v.Query)) == 0:
		return ""
	case len(v.Hits) == 0:
		return "no hits"
	}
	total := 0
	for _, h := range v.Hits {
		total += h.Count
	}
	return fmt.Sprintf("%s in %s", plural(total, "hit"), plural(len(v.Hits), "page"))
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
