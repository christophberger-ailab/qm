package web

import (
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"github.com/christophberger-ailab/qm/internal/gitrepo"
)

// The Git button sits in the top bar beside Render, and its body is
// fetched when the panel opens rather than with the page.
func TestGitPanelIsInTheTopBar(t *testing.T) {
	srv, _ := testServer(t)
	body := get(t, srv, "/").Body.String()
	i, j := strings.Index(body, `id="render-panel"`), strings.Index(body, `id="git-panel"`)
	if i < 0 || j < 0 {
		t.Fatalf("panels missing: render at %d, git at %d", i, j)
	}
	if j < i {
		t.Error("the Git panel does not sit to the right of the render panel")
	}
	for _, want := range []string{`<summary title="Show the changes`, `id="git-body"`, `hx-get="/git"`} {
		if !strings.Contains(body, want) {
			t.Errorf("git panel missing %q:\n%s", want, body)
		}
	}
}

// With no project open there is nothing to show a working tree of, so the
// button is not there either.
func TestGitPanelNeedsAProject(t *testing.T) {
	srv := bareServer(t)
	if body := get(t, srv, "/").Body.String(); strings.Contains(body, `<details class="git"`) {
		t.Errorf("git panel shown without a project:\n%s", body)
	}
}

// /open refreshes the panel along with the panes: it belongs to the
// project, and the top bar is out of the #main swap's reach.
func TestOpenRefreshesTheGitPanel(t *testing.T) {
	srv, _ := testServer(t)
	body := post(t, srv, "/open", url.Values{"path": {fixture(t)}}).Body.String()
	if !strings.Contains(body, `hx-swap-oob="innerHTML:#git-panel"`) {
		t.Errorf("open does not refresh the git panel:\n%s", body)
	}
}

// A project that is not a working tree gets an explanation instead of a
// list of changes; the same route is what says so when git is missing.
func TestGitOutsideARepository(t *testing.T) {
	srv, _ := testServer(t)
	rec := get(t, srv, "/git")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="error"`) {
		t.Errorf("no explanation given:\n%s", body)
	}
	if strings.Contains(body, "Commit") {
		t.Errorf("commit offered outside a repository:\n%s", body)
	}
}

// The panel's operations need a project; without one they say so rather
// than run git somewhere unexpected.
func TestGitOperationsNeedAProject(t *testing.T) {
	srv := bareServer(t)
	for _, target := range []string{"/git/stage", "/git/unstage", "/git/commit", "/git/push"} {
		rec := post(t, srv, target, url.Values{"path": {"index.qmd"}, "message": {"m"}})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", target, rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "no project open") {
			t.Errorf("%s does not report the missing project:\n%s", target, rec.Body)
		}
	}
}

// gitProject turns the test fixture into a repository with one commit, so
// the panel has a working tree to report on. It skips where git is not
// installed.
func gitProject(t *testing.T) (*server, string) {
	t.Helper()
	if !gitrepo.Available() {
		t.Skip("git is not installed")
	}
	srv, root := testServer(t)
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "qm@example.com"},
		{"config", "user.name", "QM Test"},
		{"add", "--all"},
		{"commit", "-m", "first"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return srv, root
}

// The panel lists the changes since the last commit, and staging moves a
// file from one list to the other.
func TestGitStageAndUnstage(t *testing.T) {
	srv, root := gitProject(t)
	writeFile(t, root, "chapter2/second.qmd", "---\ntitle: Second\norder: 1\n---\n# Edited\n")

	body := get(t, srv, "/git").Body.String()
	if !strings.Contains(body, "chapter2/second.qmd") {
		t.Fatalf("change not listed:\n%s", body)
	}
	if !strings.Contains(body, "Not staged") {
		t.Errorf("change not reported as unstaged:\n%s", body)
	}

	body = post(t, srv, "/git/stage", url.Values{"path": {"chapter2/second.qmd"}}).Body.String()
	if !strings.Contains(body, "Staged") {
		t.Errorf("file not staged:\n%s", body)
	}
	body = post(t, srv, "/git/unstage", url.Values{"path": {"chapter2/second.qmd"}}).Body.String()
	if strings.Contains(body, "<legend>Staged") {
		t.Errorf("file still staged:\n%s", body)
	}
}

// A commit takes the staged changes; the panel then reports a clean tree.
func TestGitCommit(t *testing.T) {
	srv, root := gitProject(t)
	writeFile(t, root, "chapter2/second.qmd", "---\ntitle: Second\norder: 1\n---\n# Edited\n")
	post(t, srv, "/git/stage", url.Values{"all": {"1"}})

	body := post(t, srv, "/git/commit", url.Values{"message": {"edit second"}}).Body.String()
	if !strings.Contains(body, "No changes since the last commit.") {
		t.Errorf("tree not clean after the commit:\n%s", body)
	}
}

// A commit without a message is refused, and the panel says why instead of
// leaving the user with an unexplained non-event.
func TestGitCommitWithoutMessage(t *testing.T) {
	srv, root := gitProject(t)
	writeFile(t, root, "chapter2/second.qmd", "---\ntitle: Second\norder: 1\n---\n# Edited\n")
	post(t, srv, "/git/stage", url.Values{"all": {"1"}})

	body := post(t, srv, "/git/commit", url.Values{"message": {"  "}}).Body.String()
	if !strings.Contains(body, "a commit needs a message") {
		t.Errorf("empty message not reported:\n%s", body)
	}
	if !strings.Contains(body, "<legend>Staged") {
		t.Errorf("the staged change was lost:\n%s", body)
	}
}

// A click on a path in either list opens that file's diff in the pane, and
// the pane offers to open the file in the editor.
func TestGitDiff(t *testing.T) {
	srv, root := gitProject(t)
	writeFile(t, root, "chapter2/second.qmd", "---\ntitle: Second\norder: 1\n---\n# Edited\n")

	// The list itself is what makes the diff reachable: the path is the
	// button that asks for it.
	body := get(t, srv, "/git").Body.String()
	if !strings.Contains(body, `hx-get="/git/diff?path=chapter2%2Fsecond.qmd"`) {
		t.Errorf("the path does not ask for a diff:\n%s", body)
	}
	if !strings.Contains(body, `id="git-diff"`) {
		t.Errorf("no pane for the diff to land in:\n%s", body)
	}

	body = get(t, srv, "/git/diff?path=chapter2/second.qmd").Body.String()
	if !strings.Contains(body, `class="git-dl git-dl-add"`) || !strings.Contains(body, "# Edited") {
		t.Errorf("the added line is missing:\n%s", body)
	}
	if !strings.Contains(body, `class="git-dl git-dl-del"`) {
		t.Errorf("the removed line is missing:\n%s", body)
	}
	if !strings.Contains(body, `class="git-dl git-dl-hunk"`) {
		t.Errorf("the hunk header is missing:\n%s", body)
	}
	// The pane reads only; the way to change the file is the editor.
	if !strings.Contains(body, `hx-get="/content?path=chapter2%2Fsecond.qmd"`) {
		t.Errorf("no way to open the file in the editor:\n%s", body)
	}
	if strings.Contains(body, "/git/stage") {
		t.Errorf("the diff pane offers to change the index:\n%s", body)
	}
}

// The two sides of a file changed in both places are two diffs, and the
// pane shows the one the list the click came from stands for.
func TestGitDiffStagedAndUnstaged(t *testing.T) {
	srv, root := gitProject(t)
	writeFile(t, root, "chapter2/second.qmd", "---\ntitle: Second\norder: 1\n---\n# Staged\n")
	post(t, srv, "/git/stage", url.Values{"path": {"chapter2/second.qmd"}})
	writeFile(t, root, "chapter2/second.qmd", "---\ntitle: Second\norder: 1\n---\n# Working tree\n")

	staged := get(t, srv, "/git/diff?path=chapter2/second.qmd&staged=1").Body.String()
	if !strings.Contains(staged, "# Staged") || strings.Contains(staged, "# Working tree") {
		t.Errorf("the staged diff is not what the index holds:\n%s", staged)
	}
	unstaged := get(t, srv, "/git/diff?path=chapter2/second.qmd").Body.String()
	if !strings.Contains(unstaged, "# Working tree") {
		t.Errorf("the unstaged diff is not what the working tree holds:\n%s", unstaged)
	}
}

// A file git has never seen has no other side to be diffed against, so its
// diff is its whole content, added.
func TestGitDiffUntracked(t *testing.T) {
	srv, root := gitProject(t)
	writeFile(t, root, "chapter2/third.qmd", "---\ntitle: Third\n---\n# Brand new\n")

	body := get(t, srv, "/git/diff?path=chapter2/third.qmd").Body.String()
	if !strings.Contains(body, "# Brand new") {
		t.Errorf("the new file's content is missing:\n%s", body)
	}
	if !strings.Contains(body, `class="git-dl git-dl-add"`) {
		t.Errorf("the content is not shown as added:\n%s", body)
	}
}

// The diff is a view of the open project and nothing else: a path that
// leads out of it is refused rather than read, which `git diff --no-index`
// would otherwise happily do for any file on the machine.
func TestGitDiffStaysInTheProject(t *testing.T) {
	srv, _ := gitProject(t)
	for _, path := range []string{"../outside.qmd", "/etc/passwd", ""} {
		rec := get(t, srv, "/git/diff?path="+url.QueryEscape(path))
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: status %d: %s", path, rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), `class="error"`) {
			t.Errorf("%q was not refused:\n%s", path, rec.Body)
		}
	}
}

// Without a project there is no working tree to diff against, and the pane
// says so rather than running git somewhere unexpected.
func TestGitDiffNeedsAProject(t *testing.T) {
	srv := bareServer(t)
	body := get(t, srv, "/git/diff?path=index.qmd").Body.String()
	if !strings.Contains(body, "no project open") {
		t.Errorf("the missing project is not reported:\n%s", body)
	}
}

// The lines of a diff are classified by what they are, and the file
// headers above the first hunk are dropped: the pane's own header already
// names the file. A removed line that begins with dashes is a deletion,
// not one of those headers.
func TestDiffLines(t *testing.T) {
	out := strings.Join([]string{
		"diff --git a/a.qmd b/a.qmd",
		"index 1234567..89abcde 100644",
		"--- a/a.qmd",
		"+++ b/a.qmd",
		"@@ -1,4 +1,4 @@",
		" title: A",
		"-- an old note",
		"+new line",
		`\ No newline at end of file`,
	}, "\n")
	lines, added, removed, binary := diffLines(out)
	var kinds []string
	for _, l := range lines {
		kinds = append(kinds, l.Kind)
	}
	want := []string{"hunk", "ctx", "del", "add", "meta"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}
	if added != 1 || removed != 1 {
		t.Errorf("added = %d, removed = %d; want 1, 1", added, removed)
	}
	if binary {
		t.Error("a text diff read as binary")
	}
}

// A binary file has no lines to show and nothing the editor could open, so
// the pane says what git says and offers no more.
func TestDiffLinesBinary(t *testing.T) {
	_, _, _, binary := diffLines("diff --git a/i.png b/i.png\nBinary files a/i.png and b/i.png differ")
	if !binary {
		t.Error("a binary diff not recognized")
	}
}
