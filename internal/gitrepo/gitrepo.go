// Package gitrepo is the thin layer between qm and the `git` command: the
// working tree's status, the diff of one file in it, and the four
// operations the web UI offers on it — stage, unstage, commit, push.
//
// It shells out rather than linking a Git implementation in: the projects
// qm manages are ordinary clones the user also works with from a terminal,
// with their own hooks, credential helpers, signing keys, and config, and
// only the real git honours all of those. The cost is that git has to be
// installed, which Available reports so the UI can say so instead of
// failing.
package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Timeout bounds a single git invocation. Status, staging, and committing
// are local and quick; a push talks to a remote and may sit waiting for a
// credential prompt that nobody can answer, because there is no terminal
// attached. The bound is what keeps such a call from occupying the panel
// forever.
var Timeout = 2 * time.Minute

// ErrNoGit is returned when the git command is not installed.
var ErrNoGit = errors.New("git is not installed")

// Available reports whether the git command can be found.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// File is one path the working tree or the index has changed since the
// last commit.
type File struct {
	// Path is the file, relative to the repository root. A rename carries
	// its new path; From then names where it came from.
	Path string
	From string
	// Code is the two-letter porcelain status ("M ", " M", "??", ...) as
	// git writes it, and Description says the same in words.
	Code        string
	Description string
	// Staged says which of the two lists the entry belongs to: what the
	// next commit would contain, or what it would leave behind.
	Staged bool
}

// Status is the working tree as the Git panel shows it.
type Status struct {
	// Branch is the checked-out branch, or "(detached)" when the head is
	// not on one. Upstream is the remote branch it tracks, if any, and
	// Ahead and Behind count the commits between them.
	Branch   string
	Upstream string
	Ahead    int
	Behind   int
	// Staged and Unstaged are the changed files, split by whether the
	// next commit would carry them. A file changed both in the index and
	// in the working tree appears in both.
	Staged   []File
	Unstaged []File
}

// IsRepo reports whether dir is inside a Git working tree.
func IsRepo(dir string) bool {
	out, err := run(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// Root is the top of the working tree dir is in — the directory the paths
// of a status are relative to, which is not the directory qm was pointed
// at whenever the project sits somewhere inside a larger repository.
func Root(dir string) (string, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	// Git answers with forward slashes on every platform.
	return filepath.FromSlash(strings.TrimSpace(out)), nil
}

// top is Root for the callers that have nowhere to report a failure: a
// directory that is not in a working tree has no top, and the command run
// there will say so better than this could.
//
// Every operation below runs at the top rather than at the project, because
// that is the one directory in which the paths git reports are the paths
// git accepts. Run from a project that is a subdirectory of its repository,
// `git add -- doc/book/index.qmd` looks for doc/book/index.qmd *inside*
// doc/book, finds nothing, and fails.
func top(dir string) string {
	root, err := Root(dir)
	if err != nil {
		return dir
	}
	return root
}

// GetStatus reads the changes since the last commit.
//
// The porcelain v1 format is asked for explicitly and read with NUL
// separators, because that is the one output of git that is documented as
// stable across versions and the only one in which a path containing a
// space, a quote, or a newline still arrives in one piece.
func GetStatus(dir string) (Status, error) {
	out, err := run(top(dir), "status", "--porcelain", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return Status{}, err
	}
	return parseStatus(out), nil
}

// parseStatus reads the NUL-separated records of `git status --porcelain
// -z --branch`. The first record is the branch header; every other one is
// "XY <path>", followed by a record of its own with the original path when
// X or Y reports a rename or a copy.
func parseStatus(out string) Status {
	var st Status
	recs := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if rec == "" {
			continue
		}
		if strings.HasPrefix(rec, "## ") {
			st.Branch, st.Upstream, st.Ahead, st.Behind = parseBranch(rec[3:])
			continue
		}
		if len(rec) < 4 { // "XY " plus at least one character of path
			continue
		}
		x, y, path := rec[0], rec[1], rec[3:]
		from := ""
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			if i+1 < len(recs) {
				i++
				from = recs[i]
			}
		}
		// "??" and "!!" are not an index/worktree pair but one state of
		// the whole file: it is not in the index at all, so nothing about
		// it can be staged.
		if x == '?' || x == '!' {
			st.Unstaged = append(st.Unstaged, File{
				Path: path, From: from, Code: rec[:2], Description: describe(x, y), Staged: false,
			})
			continue
		}
		if x != ' ' {
			st.Staged = append(st.Staged, File{
				Path: path, From: from, Code: string([]byte{x, ' '}), Description: describe(x, ' '), Staged: true,
			})
		}
		if y != ' ' {
			st.Unstaged = append(st.Unstaged, File{
				Path: path, From: from, Code: string([]byte{' ', y}), Description: describe(' ', y), Staged: false,
			})
		}
	}
	return st
}

// parseBranch reads the "## <branch>...<upstream> [ahead N, behind M]"
// header. A repository without commits yet says "No commits yet on
// <branch>", and a detached head says "HEAD (no branch)"; both are shown
// as the branch name they name.
func parseBranch(h string) (branch, upstream string, ahead, behind int) {
	if rest, ok := strings.CutPrefix(h, "No commits yet on "); ok {
		h = rest
	}
	if i := strings.Index(h, " ["); i >= 0 {
		track := strings.TrimSuffix(h[i+2:], "]")
		h = h[:i]
		for _, part := range strings.Split(track, ", ") {
			if n, ok := strings.CutPrefix(part, "ahead "); ok {
				ahead, _ = strconv.Atoi(n)
			}
			if n, ok := strings.CutPrefix(part, "behind "); ok {
				behind, _ = strconv.Atoi(n)
			}
		}
	}
	branch, upstream, _ = strings.Cut(h, "...")
	return branch, upstream, ahead, behind
}

// codeWords says what one porcelain status letter means, in the words the
// panel shows on hover.
var codeWords = map[byte]string{
	'M': "modified",
	'T': "type changed",
	'A': "added",
	'D': "deleted",
	'R': "renamed",
	'C': "copied",
	'U': "unmerged",
	'?': "untracked",
	'!': "ignored",
}

// describe turns a status pair into the phrase the panel shows: what
// happened, and where — in the index, which is what a commit would carry,
// or in the working tree, which is what it would leave behind.
func describe(x, y byte) string {
	if x == '?' {
		return "untracked"
	}
	if x == '!' {
		return "ignored"
	}
	if x != ' ' {
		return codeWords[x] + " in the index"
	}
	if word, ok := codeWords[y]; ok {
		return word + " in the working tree"
	}
	return "changed"
}

// Stage adds a path to the index, so the next commit carries it. An empty
// path stages every change in the repository.
func Stage(dir, path string) (string, error) {
	dir = top(dir)
	if path == "" {
		return run(dir, "add", "--all", "--", ".")
	}
	return run(dir, "add", "--", path)
}

// Unstage takes a path back out of the index, leaving the file itself as
// it is. An empty path unstages everything.
//
// `restore --staged` needs a commit to restore the index entry from, so a
// repository whose first commit is still to come is unstaged with `rm
// --cached` instead: there is no HEAD to name.
func Unstage(dir, path string) (string, error) {
	dir = top(dir)
	args := []string{"restore", "--staged", "--"}
	if !hasHead(dir) {
		args = []string{"rm", "--cached", "-r", "--"}
	}
	if path == "" {
		path = "."
	}
	return run(dir, append(args, path)...)
}

// hasHead reports whether the repository has a commit yet.
func hasHead(dir string) bool {
	_, err := run(dir, "rev-parse", "--verify", "HEAD")
	return err == nil
}

// Commit records the staged changes under message.
func Commit(dir, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("a commit needs a message")
	}
	return run(top(dir), "commit", "-m", message)
}

// Push sends the current branch to its remote. A branch that tracks
// nothing yet is pushed with its upstream set, which is what the same
// branch's next push then follows.
func Push(dir string) (string, error) {
	dir = top(dir)
	st, err := GetStatus(dir)
	if err == nil && st.Upstream == "" && st.Branch != "" {
		return run(dir, "push", "--set-upstream", "origin", st.Branch)
	}
	return run(dir, "push")
}

// Diff is the change to one path, as `git diff` writes it.
//
// staged asks for the diff between HEAD and the index — what the next
// commit would carry — rather than the one between the index and the
// working tree. An untracked file is in neither, so it is diffed against
// an empty file instead, which is what makes a file the panel lists as new
// show its content rather than nothing at all.
//
// The external diff drivers and textconv filters a repository may
// configure are turned off: they are meant for a terminal, may open a
// program of their own, and what the panel colours are the lines git
// itself produces.
func Diff(dir, path string, staged bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("no file named")
	}
	dir = top(dir)
	args := []string{"diff", "--no-ext-diff", "--no-color", "--find-renames"}
	switch {
	case staged:
		args = append(args, "--cached")
	case !tracked(dir, path):
		// --no-index reports "they differ" as exit status 1, which here
		// is the answer rather than a failure.
		return runAllowing(dir, 1, "diff", "--no-ext-diff", "--no-color", "--no-index", "--", devNull, path)
	}
	return run(dir, append(args, "--", path)...)
}

// devNull is the empty file git compares an untracked one against. Git
// spells it this way on every platform, Windows included: the name is
// recognised by git itself, not handed to the operating system.
const devNull = "/dev/null"

// tracked reports whether the index knows the path. A file staged for the
// first commit it appears in counts: the index is where a diff against
// HEAD finds it.
func tracked(dir, path string) bool {
	_, err := run(dir, "ls-files", "--error-unmatch", "--", path)
	return err == nil
}

// run executes git in dir and returns its combined output. Git says what
// went wrong on stderr, and that text is the whole point of showing the
// output in the panel, so failures carry it rather than just the exit
// status.
func run(dir string, args ...string) (string, error) {
	return runAllowing(dir, 0, args...)
}

// runAllowing is run with one non-zero exit status treated as success:
// `git diff --no-index` says "the two differ" by exiting 1, and there that
// is the answer, not a failure. An ok of 0 allows nothing.
func runAllowing(dir string, ok int, args ...string) (string, error) {
	if !Available() {
		return "", ErrNoGit
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// No terminal is attached, so a prompt for a password or a passphrase
	// would hang until the timeout. Refusing to ask turns that into an
	// error message the user can act on.
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return string(out), fmt.Errorf("git %s timed out after %s", args[0], Timeout)
		}
		var exit *exec.ExitError
		if ok != 0 && errors.As(err, &exit) && exit.ExitCode() == ok {
			return string(out), nil
		}
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return string(out), errors.New(msg)
		}
		return string(out), err
	}
	return string(out), nil
}
