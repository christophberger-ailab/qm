package web

import (
	"net/http"
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
