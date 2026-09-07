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
	"unicode/utf8"
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

	// terms maps each word to the pages it appears on and, on every page,
	// to the positions it stands at -- the word's ordinal in the page,
	// counted from the start of the file. Counting the occurrences alone
	// would answer a query of loose words, but not a quoted phrase:
	// "logging in" asks for two words standing next to each other, and only
	// their positions can tell whether they do.
	terms wordIndex
}

// wordIndex is what a built index holds: word -> page -> positions.
type wordIndex map[string]map[string][]int

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
func indexPages(root string) wordIndex {
	terms := wordIndex{}
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
		for at, term := range indexWords(string(src)) {
			pages := terms[term]
			if pages == nil {
				pages = map[string][]int{}
				terms[term] = pages
			}
			pages[page] = append(pages[page], at)
		}
		return nil
	})
	return terms
}

// isWordRune reports whether r is part of a word. A word is what is left of
// a page when its syntax is taken away, and the syntax of Markdown and YAML
// is written entirely in ASCII: hashes, stars, backticks, brackets, colons,
// dashes. So every ASCII character that is not a letter or a digit
// separates words, and so does whitespace and the punctuation of every
// other script -- quotation marks, the em dash, the Japanese full stop.
//
// Everything else is part of a word. That is the wide half of the rule and
// the point of it: letters and digits of every script, the marks written on
// top of them, the joiners that hold an emoji sequence together, and the
// symbols themselves. A page that says 🚒 can be searched for 🚒.
func isWordRune(r rune) bool {
	if r < utf8.RuneSelf {
		return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9'
	}
	return !unicode.IsSpace(r) && !unicode.IsPunct(r)
}

// isJoiner reports whether r can hold a compound word together. A single
// hyphen between two word characters belongs to the word -- "semi-wide" is
// one word, and searching for "wide" is not searching for it -- while the
// same character anywhere else is Markdown: a bullet, a thematic break, the
// fence of a YAML header, or the em dash that two of them stand for.
func isJoiner(r rune) bool {
	return r == '-' || r == '\u2010' || r == '\u2011'
}

// indexWords splits text into the lowercased words the index is keyed by,
// in the order they are read. Their order is their position: the index
// keeps it so that a phrase can be told apart from the same words scattered
// over a page.
func indexWords(text string) []string {
	var words []string
	var word []rune
	joiner, joiners := rune(0), 0
	flush := func() {
		if len(word) > 0 {
			words = append(words, string(word))
			word = word[:0]
		}
	}
	for _, r := range text {
		switch {
		case isWordRune(r):
			if joiners == 1 && len(word) > 0 {
				word = append(word, joiner) // between two word characters
			} else if joiners > 0 {
				flush() // a run of dashes, or one that starts a word
			}
			word, joiners = append(word, unicode.ToLower(r)), 0
		case isJoiner(r):
			joiner, joiners = r, joiners+1
		default:
			flush()
			joiners = 0
		}
	}
	flush()
	return words
}

// queryTerm is one thing a query asks for: a word, or a phrase of words
// that have to stand next to each other in that order. Quotes make a
// phrase, and a closing quote also makes it exact -- see parseQuery.
type queryTerm struct {
	words []string
	exact bool
}

// quoteEnds pairs every quotation mark that can open a phrase with the one
// that closes it: the two that are typed, and the two that a text editor or
// a phone turns them into. The typographic closers are deliberately not
// openers, so that the apostrophe of "don’t" cannot start a phrase.
var quoteEnds = map[rune]rune{'"': '"', '\'': '\'', '“': '”', '‘': '’'}

// parseQuery reads a query into the terms it asks for. Words between a pair
// of quotes are one term that matches only where they stand together;
// everything outside is one term per word, as before.
//
// A quote only counts as one where a phrase can begin or end -- with a
// separator, or the edge of the query, on its outer side. That is what
// keeps the apostrophe in "don't stop" from opening a phrase that swallows
// the rest of the line.
//
// The closing quote does one more thing: it says the phrase is finished. An
// unfinished phrase still matches its last word by prefix, so that a
// quoted query narrows down while it is typed like any other; the closing
// quote turns that last word exact, which is the only way to ask this
// search for a whole word.
func parseQuery(q string) []queryTerm {
	var terms []queryTerm
	rs := []rune(q)
	loose := 0 // start of the unquoted run passed over so far
	for i := 0; i < len(rs); i++ {
		closer, ok := quoteEnds[rs[i]]
		if !ok || (i > 0 && isWordRune(rs[i-1])) {
			continue
		}
		end := len(rs) // an unclosed quote quotes the rest of the query
		closed := false
		for j := i + 1; j < len(rs); j++ {
			if rs[j] == closer && (j+1 == len(rs) || !isWordRune(rs[j+1])) {
				end, closed = j, true
				break
			}
		}
		terms = append(terms, looseTerms(string(rs[loose:i]))...)
		terms = append(terms, queryTerm{indexWords(string(rs[i+1 : end])), closed})
		i, loose = end, min(end+1, len(rs))
	}
	terms = append(terms, looseTerms(string(rs[loose:]))...)
	return slices.DeleteFunc(terms, queryTerm.skip)
}

// looseTerms are the unquoted words of a query: one term each, every one of
// them matching by prefix.
func looseTerms(text string) []queryTerm {
	var terms []queryTerm
	for _, w := range indexWords(text) {
		terms = append(terms, queryTerm{[]string{w}, false})
	}
	return terms
}

// skip reports whether a term is not worth searching for. A single letter
// or digit starts almost every page and would highlight the whole tree
// after the first keystroke. Any other single character -- an emoji, a
// symbol, a currency sign -- is rare enough to be exactly what the user
// means, and a phrase is specific enough whatever its words are.
func (t queryTerm) skip() bool {
	if len(t.words) == 0 {
		return true // empty quotes
	}
	if len(t.words) > 1 {
		return false
	}
	rs := []rune(t.words[0])
	return len(rs) == 1 && (unicode.IsLetter(rs[0]) || unicode.IsDigit(rs[0]))
}

// matchPages finds the pages matching every term of q, and counts the
// matches. A page matches a term where the term's words are, so a loose
// query counts its words and a phrase counts the places the whole phrase
// stands.
func matchPages(terms wordIndex, q string) []searchHit {
	query := parseQuery(q)
	if len(query) == 0 {
		return nil
	}
	var found map[string]int
	for _, t := range query {
		pages := t.match(terms)
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

// match counts the term on every page that has it.
func (t queryTerm) match(terms wordIndex) map[string]int {
	hits := map[string]int{}
	if len(t.words) == 1 {
		for page, at := range wordPages(terms, t.words[0], t.exact) {
			hits[page] = len(at)
		}
		return hits
	}
	// A phrase stands where its first word is followed by all the others.
	// Start from every place the first word appears and drop the places the
	// next word does not carry on from; what survives the last word are the
	// places the whole phrase stands at.
	starts := map[string][]int{}
	for page, at := range wordPages(terms, t.words[0], true) {
		starts[page] = slices.Clone(at)
	}
	for i := 1; i < len(t.words); i++ {
		// Only the last word of a phrase may still be being typed.
		next := wordPages(terms, t.words[i], t.exact || i < len(t.words)-1)
		for page, from := range starts {
			kept := from[:0]
			for _, p := range from {
				if _, ok := slices.BinarySearch(next[page], p+i); ok {
					kept = append(kept, p)
				}
			}
			if len(kept) == 0 {
				delete(starts, page)
				continue
			}
			starts[page] = kept
		}
	}
	for page, at := range starts {
		hits[page] = len(at)
	}
	return hits
}

// wordPages are the pages a single word of a query is on, with the sorted
// positions it is at. A word matches by prefix while it is being typed --
// a query that only matched whole words would find nothing until its last
// letter -- and whole once a closing quote says it is finished.
//
// The result of an exact lookup is the index's own map and must be read
// only; the prefix lookup builds its own.
func wordPages(terms wordIndex, w string, exact bool) map[string][]int {
	if exact {
		return terms[w]
	}
	pages := map[string][]int{}
	for term, at := range terms {
		if !strings.HasPrefix(term, w) {
			continue
		}
		for page, positions := range at {
			pages[page] = append(pages[page], positions...)
		}
	}
	// The positions of one word come out of the index in order, but those
	// of several words merged into one prefix do not, and the phrase walk
	// searches them.
	for _, positions := range pages {
		slices.Sort(positions)
	}
	return pages
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
		view.Hits, view.Indexing = hits, !current && len(parseQuery(q)) > 0
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
	case len(parseQuery(v.Query)) == 0:
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
