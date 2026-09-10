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
