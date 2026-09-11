package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBranch(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		branch   string
		upstream string
		ahead    int
		behind   int
	}{
		{"plain", "main", "main", "", 0, 0},
		{"tracking", "main...origin/main", "main", "origin/main", 0, 0},
		{"ahead", "main...origin/main [ahead 2]", "main", "origin/main", 2, 0},
		{"both", "main...origin/main [ahead 2, behind 3]", "main", "origin/main", 2, 3},
		{"behind", "main...origin/main [behind 1]", "main", "origin/main", 0, 1},
		{"no commits yet", "No commits yet on main", "main", "", 0, 0},
		{"detached", "HEAD (no branch)", "HEAD (no branch)", "", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			branch, upstream, ahead, behind := parseBranch(tc.header)
			if branch != tc.branch || upstream != tc.upstream || ahead != tc.ahead || behind != tc.behind {
				t.Errorf("parseBranch(%q) = %q, %q, %d, %d; want %q, %q, %d, %d",
					tc.header, branch, upstream, ahead, behind, tc.branch, tc.upstream, tc.ahead, tc.behind)
			}
		})
	}
}

// rec builds the NUL-terminated records git writes with -z.
func rec(entries ...string) string {
	return strings.Join(entries, "\x00") + "\x00"
}

func TestParseStatus(t *testing.T) {
	out := rec(
		"## main...origin/main [ahead 1]",
		"M  staged.qmd",
		" M dirty.qmd",
		"MM both.qmd",
		"?? new.qmd",
		"A  added.qmd",
		" D gone.qmd",
		"R  now.qmd", "then.qmd",
	)
	st := parseStatus(out)

	if st.Branch != "main" || st.Upstream != "origin/main" || st.Ahead != 1 {
		t.Errorf("branch = %q, upstream = %q, ahead = %d", st.Branch, st.Upstream, st.Ahead)
	}

	staged := paths(st.Staged)
	want := []string{"staged.qmd", "both.qmd", "added.qmd", "now.qmd"}
	if !equal(staged, want) {
		t.Errorf("staged = %v, want %v", staged, want)
	}
	unstaged := paths(st.Unstaged)
	want = []string{"dirty.qmd", "both.qmd", "new.qmd", "gone.qmd"}
	if !equal(unstaged, want) {
		t.Errorf("unstaged = %v, want %v", unstaged, want)
	}

	// A rename keeps where it came from, so the panel can say so.
	if st.Staged[3].From != "then.qmd" {
		t.Errorf("rename origin = %q, want then.qmd", st.Staged[3].From)
	}
	// An untracked file is not in the index at all, so it cannot be one of
	// the staged entries.
	for _, f := range st.Staged {
		if f.Path == "new.qmd" {
			t.Error("untracked file listed as staged")
		}
	}
	// The description tells the two sides of a file changed in both apart.
	if got := st.Staged[1].Description; got != "modified in the index" {
		t.Errorf("staged description = %q", got)
	}
	if got := st.Unstaged[1].Description; got != "modified in the working tree" {
		t.Errorf("unstaged description = %q", got)
	}
}

// A path with a space survives the -z format that a line-based read would
// have to guess at.
func TestParseStatusSpacedPath(t *testing.T) {
	st := parseStatus(rec("## main", " M a folder/a page.qmd"))
	if len(st.Unstaged) != 1 || st.Unstaged[0].Path != "a folder/a page.qmd" {
		t.Errorf("unstaged = %+v", st.Unstaged)
	}
}

func paths(files []File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// testRepo makes a repository with one commit and a configured identity,
// so committing does not depend on the machine's git config. Tests using
// it are skipped where git is not installed.
func testRepo(t *testing.T) string {
	t.Helper()
	if !Available() {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "qm@example.com"},
		{"config", "user.name", "QM Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write(t, dir, "index.qmd", "---\ntitle: Home\n---\n")
	if _, err := Stage(dir, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(dir, "first"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsRepo(t *testing.T) {
	dir := testRepo(t)
	if !IsRepo(dir) {
		t.Error("IsRepo = false for a repository")
	}
	if IsRepo(t.TempDir()) {
		t.Error("IsRepo = true for a plain directory")
	}
}

func TestStageUnstageCommit(t *testing.T) {
	dir := testRepo(t)
	write(t, dir, "new.qmd", "---\ntitle: New\n---\n")
	write(t, dir, "index.qmd", "---\ntitle: Changed\n---\n")

	st, err := GetStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 0 {
		t.Errorf("staged before staging = %v", paths(st.Staged))
	}
	if got := paths(st.Unstaged); len(got) != 2 {
		t.Errorf("unstaged = %v, want two entries", got)
	}

	if _, err := Stage(dir, "new.qmd"); err != nil {
		t.Fatal(err)
	}
	st, _ = GetStatus(dir)
	if got := paths(st.Staged); !equal(got, []string{"new.qmd"}) {
		t.Errorf("staged = %v, want [new.qmd]", got)
	}

	if _, err := Unstage(dir, "new.qmd"); err != nil {
		t.Fatal(err)
	}
	st, _ = GetStatus(dir)
	if len(st.Staged) != 0 {
		t.Errorf("staged after unstaging = %v", paths(st.Staged))
	}

	if _, err := Stage(dir, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(dir, "second"); err != nil {
		t.Fatal(err)
	}
	st, _ = GetStatus(dir)
	if len(st.Staged) != 0 || len(st.Unstaged) != 0 {
		t.Errorf("tree not clean after commit: %+v", st)
	}
	if st.Branch != "main" {
		t.Errorf("branch = %q, want main", st.Branch)
	}
}

// A commit without a message is refused before git is even asked, because
// git would open an editor there is no terminal for.
func TestCommitNeedsMessage(t *testing.T) {
	if _, err := Commit(t.TempDir(), "  "); err == nil {
		t.Error("Commit with an empty message succeeded")
	}
}

// A commit with nothing staged fails, and git's own words come back, since
// that is what the panel shows.
func TestCommitNothingStaged(t *testing.T) {
	dir := testRepo(t)
	_, err := Commit(dir, "empty")
	if err == nil {
		t.Fatal("Commit with nothing staged succeeded")
	}
	if !strings.Contains(err.Error(), "nothing") {
		t.Errorf("error = %q, want git's own message", err)
	}
}

// The three diffs the panel asks for: what the working tree changed, what
// the index holds, and the whole content of a file git has never seen.
func TestDiff(t *testing.T) {
	dir := testRepo(t)
	write(t, dir, "index.qmd", "---\ntitle: Changed\n---\n")
	write(t, dir, "new.qmd", "---\ntitle: New\n---\n")

	out, err := Diff(dir, "index.qmd", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "-title: Home") || !strings.Contains(out, "+title: Changed") {
		t.Errorf("working tree diff misses the change:\n%s", out)
	}

	// Nothing is staged yet, so the diff against HEAD is empty rather than
	// an error: the file simply differs on the other side.
	if out, err := Diff(dir, "index.qmd", true); err != nil || strings.TrimSpace(out) != "" {
		t.Errorf("staged diff = %q, %v; want empty", out, err)
	}

	// An untracked file is in neither HEAD nor the index, so its diff is
	// the whole file, added.
	out, err = Diff(dir, "new.qmd", false)
	if err != nil {
		t.Fatalf("untracked diff: %v", err)
	}
	if !strings.Contains(out, "+title: New") {
		t.Errorf("untracked diff misses the content:\n%s", out)
	}

	if _, err := Stage(dir, "index.qmd"); err != nil {
		t.Fatal(err)
	}
	out, err = Diff(dir, "index.qmd", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+title: Changed") {
		t.Errorf("staged diff misses the change:\n%s", out)
	}
}

// A diff of nothing would be a diff of everything, which is not what a
// click on one file asks for.
func TestDiffNeedsAPath(t *testing.T) {
	if _, err := Diff(t.TempDir(), "  ", false); err == nil {
		t.Error("Diff without a path succeeded")
	}
}

// A path with a space is one argument to git, not two, and is not read as
// a pathspec pattern either.
func TestDiffSpacedPath(t *testing.T) {
	dir := testRepo(t)
	write(t, dir, "a page.qmd", "---\ntitle: Spaced\n---\n")
	out, err := Diff(dir, "a page.qmd", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+title: Spaced") {
		t.Errorf("diff of a spaced path:\n%s", out)
	}
}

// subdirRepo is a repository whose Quarto project sits well inside it, the
// way a book kept in a documentation monorepo does. It returns the
// repository root and the project directory qm would be pointed at.
func subdirRepo(t *testing.T) (root, proj string) {
	t.Helper()
	root = testRepo(t)
	proj = filepath.Join(root, "doc", "Training", "book")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, proj, "index.qmd", "---\ntitle: Book\n---\n# Book\n")
	if _, err := Stage(proj, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(proj, "book"); err != nil {
		t.Fatal(err)
	}
	return root, proj
}

// The paths a status reports are relative to the top of the working tree,
// not to the directory git ran in, so every operation has to run at the
// top: from the project, `git add -- doc/Training/book/index.qmd` would
// look inside doc/Training/book for a doc/Training/book of its own.
func TestOperationsOnAProjectInsideARepository(t *testing.T) {
	_, proj := subdirRepo(t)
	write(t, proj, "index.qmd", "---\ntitle: Book\n---\n# Edited\n")
	write(t, proj, "new.qmd", "---\ntitle: New\n---\n")

	st, err := GetStatus(proj)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"doc/Training/book/index.qmd", "doc/Training/book/new.qmd"}
	if got := paths(st.Unstaged); !equal(got, want) {
		t.Fatalf("unstaged = %v, want %v", got, want)
	}

	// Each of the paths the status just reported has to be one the other
	// operations accept.
	for _, path := range want {
		out, err := Diff(proj, path, false)
		if err != nil {
			t.Errorf("Diff(%q): %v", path, err)
		}
		if !strings.Contains(out, "+") {
			t.Errorf("Diff(%q) shows no change:\n%s", path, out)
		}
		if _, err := Stage(proj, path); err != nil {
			t.Errorf("Stage(%q): %v", path, err)
		}
	}
	st, _ = GetStatus(proj)
	if got := paths(st.Staged); !equal(got, want) {
		t.Errorf("staged = %v, want %v", got, want)
	}
	if _, err := Unstage(proj, want[0]); err != nil {
		t.Errorf("Unstage(%q): %v", want[0], err)
	}

	out, err := Diff(proj, want[0], true)
	if err != nil || strings.TrimSpace(out) != "" {
		t.Errorf("staged diff after unstaging = %q, %v; want empty", out, err)
	}
}

// Root is the top of the working tree, whichever directory inside it is
// asked.
func TestRoot(t *testing.T) {
	root, proj := subdirRepo(t)
	got, err := Root(proj)
	if err != nil {
		t.Fatal(err)
	}
	// The temporary directory may be reached through a symlink (/tmp on
	// macOS), and git answers with the resolved path.
	want, _ := filepath.EvalSymlinks(root)
	if got != want {
		t.Errorf("Root(%q) = %q, want %q", proj, got, want)
	}
	if _, err := Root(t.TempDir()); err == nil {
		t.Error("Root outside a repository succeeded")
	}
}
