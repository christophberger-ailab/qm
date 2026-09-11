package web

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/christophberger-ailab/qm/internal/gitrepo"
)

// gitView is what the Git panel shows: where the branch stands, the files
// changed since the last commit split by whether they are staged, and the
// outcome of whatever the user last asked for.
type gitView struct {
	// Repo says the panel has something to act on: git is installed and
	// the project is inside a working tree. When it is false, Error says
	// which of the two is missing and the panel shows nothing else.
	Repo     bool
	Branch   string
	Upstream string
	Ahead    int
	Behind   int
	Staged   []gitrepo.File
	Unstaged []gitrepo.File
	// Message reports what an operation did, Error why it did not, and
	// Output carries git's own words for either — a commit's summary, a
	// push's transcript, the reason a hook refused.
	Message string
	Error   string
	Output  string
}

// gitRoot returns the open project, or "" when there is none. It is read
// under the lock and used without it: a git command talks to a remote and
// can take as long as a render, and the tree handlers must stay answerable
// while it does.
func (s *server) gitRoot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.root
}

// gitStatus builds the panel from the working tree, carrying over what the
// operation that led here reported.
func gitStatus(root string, v gitView) gitView {
	switch {
	case root == "":
		v.Error = "no project open"
		return v
	case !gitrepo.Available():
		v.Error = "git is not installed, so the panel has nothing to work with"
		return v
	case !gitrepo.IsRepo(root):
		v.Error = "this project is not inside a Git working tree"
		return v
	}
	st, err := gitrepo.GetStatus(root)
	if err != nil {
		v.Error = err.Error()
		return v
	}
	v.Repo = true
	v.Branch, v.Upstream = st.Branch, st.Upstream
	v.Ahead, v.Behind = st.Ahead, st.Behind
	v.Staged, v.Unstaged = st.Staged, st.Unstaged
	return v
}

// renderGit answers with the panel body: every git route ends here, so the
// panel always shows the working tree as it stands after what was asked,
// not as it stood before.
func (s *server) renderGit(w http.ResponseWriter, v gitView) {
	s.render(w, "git-status", gitStatus(s.gitRoot(), v))
}

// gitStatusHandler is the panel opening, and the Refresh button.
func (s *server) gitStatusHandler(w http.ResponseWriter, r *http.Request) {
	s.renderGit(w, gitView{})
}

// gitStageHandler adds a path — or, with all=1, everything — to the index.
func (s *server) gitStageHandler(w http.ResponseWriter, r *http.Request) {
	s.gitDo(w, r, "Staged", func(root, path string) (string, error) {
		return gitrepo.Stage(root, path)
	})
}

// gitUnstageHandler takes a path — or everything — back out of the index.
func (s *server) gitUnstageHandler(w http.ResponseWriter, r *http.Request) {
	s.gitDo(w, r, "Unstaged", func(root, path string) (string, error) {
		return gitrepo.Unstage(root, path)
	})
}

// gitDo runs a per-path operation and reports it. The two callers differ
// only in the verb they report and the function they run, and both take
// "all" as the empty path, which is what git itself calls the whole tree.
func (s *server) gitDo(w http.ResponseWriter, r *http.Request, verb string, op func(root, path string) (string, error)) {
	root := s.gitRoot()
	if root == "" {
		s.renderGit(w, gitView{})
		return
	}
	r.ParseForm()
	path := r.FormValue("path")
	if r.FormValue("all") != "" {
		path = ""
	} else if path == "" {
		s.renderGit(w, gitView{Error: "no file named"})
		return
	}
	what := path
	if what == "" {
		what = "everything"
	}
	out, err := op(root, path)
	v := gitView{Output: strings.TrimSpace(out), Message: verb + " " + what + "."}
	if err != nil {
		v.Message, v.Error = "", err.Error()
	}
	s.renderGit(w, v)
}

// gitCommitHandler records the staged changes.
func (s *server) gitCommitHandler(w http.ResponseWriter, r *http.Request) {
	root := s.gitRoot()
	if root == "" {
		s.renderGit(w, gitView{})
		return
	}
	r.ParseForm()
	out, err := gitrepo.Commit(root, r.FormValue("message"))
	v := gitView{Output: strings.TrimSpace(out), Message: "Committed."}
	if err != nil {
		v.Message, v.Error = "", err.Error()
	}
	s.renderGit(w, v)
}

// gitPushHandler sends the branch to its remote.
func (s *server) gitPushHandler(w http.ResponseWriter, r *http.Request) {
	root := s.gitRoot()
	if root == "" {
		s.renderGit(w, gitView{})
		return
	}
	out, err := gitrepo.Push(root)
	v := gitView{Output: strings.TrimSpace(out), Message: "Pushed."}
	if err != nil {
		v.Message, v.Error = "", err.Error()
	}
	s.renderGit(w, v)
}

// diffView is the diff pane inside the Git panel: the change to the one
// file the user clicked, split into the lines the pane colours.
type diffView struct {
	Path string
	// Staged says which of a file's two diffs is shown — what the next
	// commit would carry, or what it would leave behind — because a file
	// changed in both places has one of each and they differ.
	Staged bool
	// Added and Removed count the lines of the diff, so the header can
	// say how big the change is without the user reading it.
	Added   int
	Removed int
	// Editable says whether the pane offers to open the file in the
	// editor: a file that is gone from the working tree, one git calls
	// binary, and one that lies outside the open project have nothing the
	// editor could show. EditPath is what the editor opens it by, which
	// is Path seen from the project rather than from the repository.
	Editable bool
	EditPath string
	Lines    []diffLine
	Error    string
}

// diffLine is one line of a unified diff and what it is: an added line, a
// removed one, the "@@" that starts a hunk, one of git's notes about the
// file itself, or a line of unchanged context.
type diffLine struct {
	Kind string
	Text string
}

// gitDiffHandler answers with the diff of one path. It is a GET because
// nothing changes: the pane is a view of the working tree, and asking for
// it again is what the file lists do after every operation.
func (s *server) gitDiffHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v := diffView{Path: q.Get("path"), Staged: q.Get("staged") != ""}
	if err := s.readDiff(&v); err != nil {
		v.Error = err.Error()
	}
	s.render(w, "git-diff", v)
}

// readDiff fills the pane from the working tree. What it cannot do it
// reports, and the pane shows that instead of a diff.
func (s *server) readDiff(v *diffView) error {
	p, err := s.gitPath(v.Path)
	if err != nil {
		return err
	}
	out, err := gitrepo.Diff(p.root, v.Path, v.Staged)
	if err != nil {
		return err
	}
	lines, added, removed, binary := diffLines(out)
	v.Lines, v.Added, v.Removed = lines, added, removed
	// The file has to be there to be opened, to be text to be worth
	// opening, and to be inside the project for the editor to have a path
	// to it at all. Git is what says it is not the second.
	if !binary && p.edit != "" {
		st, err := os.Stat(p.abs)
		v.Editable = err == nil && st.Mode().IsRegular()
		v.EditPath = p.edit
	}
	return nil
}

// gitFile is where a path the panel sent actually is.
type gitFile struct {
	// root is the open project, which is what a git operation is asked
	// for; git itself goes on from there to the top of the working tree.
	root string
	// abs is the file on disk, and edit is the path the editor opens it
	// by — the file seen from the project. It is empty for a file that
	// lies elsewhere in the repository, which the editor cannot address
	// and so is not offered.
	abs  string
	edit string
}

// gitPath locates a path the panel sent. The paths git reports are
// relative to the top of the working tree, which is the project itself
// only until someone keeps their Quarto book in a subdirectory of a larger
// repository; so they are resolved against the top, and what the editor
// needs is worked back out from there.
//
// They arrive as a query parameter, so the rule that keeps the editor
// inside the project applies here too, against the repository: without it,
// a path of git's `--no-index` form would read any file on the machine.
func (s *server) gitPath(rel string) (gitFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == "" {
		return gitFile{}, errors.New("no project open")
	}
	if strings.TrimSpace(rel) == "" {
		return gitFile{}, errors.New("no file named")
	}
	if !safeRel(rel) {
		return gitFile{}, errors.New("invalid path")
	}
	top, err := gitrepo.Root(s.root)
	if err != nil {
		return gitFile{}, err
	}
	f := gitFile{root: s.root, abs: filepath.Join(top, filepath.FromSlash(rel))}
	if in, err := filepath.Rel(s.root, f.abs); err == nil && safeRel(filepath.ToSlash(in)) {
		f.edit = filepath.ToSlash(in)
	}
	return f, nil
}

// diffHeads are the lines git writes above a file's first hunk that only
// repeat what the pane's header already says. They are dropped, so what
// is left is the change itself; everything else git says up there — a new
// or deleted file, a rename, a mode change, "Binary files ... differ" —
// is kept, because it is the whole story for a file that has no hunks.
var diffHeads = []string{"diff --git ", "diff --no-index ", "index ", "--- ", "+++ "}

// diffLines classifies the lines of a unified diff and counts what the
// change adds and removes.
//
// A line is read as one of git's own notes only above the first "@@": a
// removed line reading "-- a note" arrives as "--- a note" and is a
// deletion, not the "---" header, and once the hunks start, only the first
// character of a line says what it is.
func diffLines(out string) (lines []diffLine, added, removed int, binary bool) {
	inHunk := false
	for _, text := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		switch {
		case text == "" && !inHunk:
			continue
		case strings.HasPrefix(text, "@@"):
			inHunk = true
			lines = append(lines, diffLine{Kind: "hunk", Text: text})
		case !inHunk:
			if strings.HasPrefix(text, "Binary files ") || text == "GIT binary patch" {
				binary = true
			}
			if slices.ContainsFunc(diffHeads, func(h string) bool { return strings.HasPrefix(text, h) }) {
				continue
			}
			lines = append(lines, diffLine{Kind: "meta", Text: text})
		case strings.HasPrefix(text, "+"):
			added++
			lines = append(lines, diffLine{Kind: "add", Text: text})
		case strings.HasPrefix(text, "-"):
			removed++
			lines = append(lines, diffLine{Kind: "del", Text: text})
		case strings.HasPrefix(text, `\`):
			// "\ No newline at end of file" is a note about the line
			// above it, not a line of the file.
			lines = append(lines, diffLine{Kind: "meta", Text: text})
		default:
			lines = append(lines, diffLine{Kind: "ctx", Text: text})
		}
	}
	return lines, added, removed, binary
}
