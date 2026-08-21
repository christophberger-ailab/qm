package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cboct/qm/internal/bookrender"
)

// fakeQuarto installs a stub `quarto` that appends its arguments to a log
// file. Building the input is no longer the render's job — the project's
// pre-render hook does that — so the stub only has to record how it was
// called.
func fakeQuarto(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub quarto is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	stub := filepath.Join(dir, "quarto")
	script := "#!/bin/sh\necho \"args: $*\" >> " + log + "\necho \"pandoc output\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := bookrender.QuartoCommand
	bookrender.QuartoCommand = stub
	t.Cleanup(func() { bookrender.QuartoCommand = old })
	return log
}

// calls returns the stub's recorded lines.
func calls(t *testing.T, log string) string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(b)
}

// waitForRender blocks until the background render finishes.
func waitForRender(t *testing.T, s *server) jobState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := s.job.state(); !st.Running {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("render did not finish")
	return jobState{}
}

// renderForm is the panel's form for rendering chapter2 as pdf.
func renderForm() url.Values {
	return url.Values{
		"book":             {"chapter2"},
		"profile.chapter2": {"std"},
		"format":           {"pdf"},
	}
}

// bookFieldset returns the render panel fieldset for one topic.
func bookFieldset(t *testing.T, body, book string) string {
	t.Helper()
	marker := `name="book" value="` + book + `"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("render panel has no topic %q:\n%s", book, body)
	}
	start := strings.LastIndex(body[:i], `<fieldset class="book">`)
	if start < 0 {
		t.Fatalf("topic %q has no fieldset:\n%s", book, body)
	}
	end := strings.Index(body[i:], `</fieldset>`)
	if end < 0 {
		t.Fatalf("topic %q fieldset is not closed:\n%s", book, body)
	}
	return body[start : i+end+len(`</fieldset>`)]
}

// writeProfile adds a profile to the fixture project.
func writeProfile(t *testing.T, root, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "_quarto-"+name+".yml"),
		[]byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// One `quarto render --profile topic-<t>,format-<f>,audience-<a>` per
// selection: no input file, no --to, no --output-dir. Everything the render
// needs beyond that is the project's own pre- and post-render hooks.
func TestRenderRunsQuartoPerSelection(t *testing.T) {
	log := fakeQuarto(t)
	srv, _ := testServer(t)

	rec := post(t, srv, "/render", renderForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("render: status %d: %s", rec.Code, rec.Body)
	}
	st := waitForRender(t, srv)
	if st.Failed {
		t.Fatalf("render failed:\n%s", strings.Join(st.Lines, "\n"))
	}

	got := calls(t, log)
	want := "args: render --profile topic-chapter2,format-pdf,audience-std --no-clean\n"
	if !strings.Contains(got, want) {
		t.Errorf("unexpected quarto invocation:\n%s", got)
	}
	if n := strings.Count(got, "args: render"); n != 1 {
		t.Errorf("got %d render runs, want 1:\n%s", n, got)
	}
}

// Every checked format and audience is a combination of its own.
func TestRenderExpandsTheSelectedMatrix(t *testing.T) {
	log := fakeQuarto(t)
	srv, root := testServer(t)
	writeProfile(t, root, "audience-pol", "_quarto-vars:\n  audience: \"-pol\"\n")

	form := url.Values{
		"book":             {"chapter2"},
		"profile.chapter2": {"std", "pol"},
		"format":           {"pdf", "docx"},
	}
	if rec := post(t, srv, "/render", form); rec.Code != http.StatusOK {
		t.Fatalf("render: status %d: %s", rec.Code, rec.Body)
	}
	if st := waitForRender(t, srv); st.Failed {
		t.Fatalf("render failed:\n%s", strings.Join(st.Lines, "\n"))
	}

	got := calls(t, log)
	for _, want := range []string{
		"--profile topic-chapter2,format-pdf,audience-pol",
		"--profile topic-chapter2,format-pdf,audience-std",
		"--profile topic-chapter2,format-docx,audience-pol",
		"--profile topic-chapter2,format-docx,audience-std",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing render run %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "args: render"); n != 4 {
		t.Errorf("got %d render runs, want 4:\n%s", n, got)
	}
}

// Renders go into the same output directory one after another, so Quarto
// must not clean it between runs, and it must be left to the profiles to say
// where the output goes and what it is called.
func TestRenderKeepsProfileOutput(t *testing.T) {
	log := fakeQuarto(t)
	srv, _ := testServer(t)

	if rec := post(t, srv, "/render", renderForm()); rec.Code != http.StatusOK {
		t.Fatalf("render: status %d: %s", rec.Code, rec.Body)
	}
	if st := waitForRender(t, srv); st.Failed {
		t.Fatalf("render failed:\n%s", strings.Join(st.Lines, "\n"))
	}

	got := calls(t, log)
	if !strings.Contains(got, "--no-clean") {
		t.Errorf("quarto called without --no-clean:\n%s", got)
	}
	for _, flag := range []string{"--output", "--output-dir", "--to"} {
		if strings.Contains(got, flag) {
			t.Errorf("%s overrides the profile's own setting:\n%s", flag, got)
		}
	}
}

func TestRenderReportsFailure(t *testing.T) {
	fakeQuarto(t)
	srv, _ := testServer(t)
	if err := os.WriteFile(bookrender.QuartoCommand,
		[]byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if rec := post(t, srv, "/render", renderForm()); rec.Code != http.StatusOK {
		t.Fatalf("render: status %d", rec.Code)
	}
	st := waitForRender(t, srv)
	if !st.Failed {
		t.Errorf("failing render not reported as failed:\n%s", strings.Join(st.Lines, "\n"))
	}
	if out := strings.Join(st.Lines, "\n"); !strings.Contains(out, "boom") {
		t.Errorf("quarto stderr missing from the log:\n%s", out)
	}
}

// The log panel is replaced by every poll of /render/status, so the fresh
// <pre> would start at the top and the client pins it to the newest line
// instead. That hook reaches for the log through `#render-log
// .render-output`; the markup has to keep offering it.
func TestRenderLogSwapsItselfInPlace(t *testing.T) {
	srv, _ := testServer(t)

	if body := get(t, srv, "/").Body.String(); !strings.Contains(body, `<div id="render-log">`) {
		t.Errorf("the panel has no log container:\n%s", body)
	}

	rec := httptest.NewRecorder()
	srv.renderLog(rec, jobState{Lines: []string{"first", "last"}, Running: true})
	log := rec.Body.String()
	for _, want := range []string{
		`hx-get="/render/status"`,
		`hx-target="#render-log"`,
		`hx-swap="innerHTML"`,
		`class="render-output"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("running log missing %q:\n%s", want, log)
		}
	}
}

// Rendering nothing is a mistake worth naming rather than a no-op.
func TestRenderNeedsTopicAndFormat(t *testing.T) {
	fakeQuarto(t)
	srv, _ := testServer(t)

	rec := post(t, srv, "/render", url.Values{"format": {"pdf"}})
	if !strings.Contains(rec.Body.String(), "select at least one topic") {
		t.Errorf("no topic selected: %s", rec.Body)
	}
	rec = post(t, srv, "/render", url.Values{"book": {"chapter2"}})
	if !strings.Contains(rec.Body.String(), "select at least one output format") {
		t.Errorf("no format selected: %s", rec.Body)
	}
	if srv.job.state().Running {
		t.Error("an incomplete selection started a render")
	}
}

// The render panel lists the project's topics, their audiences, and the
// project's formats — the three axes a render is addressed along.
func TestRenderPanelListsTheAxes(t *testing.T) {
	srv, _ := testServer(t)
	body := get(t, srv, "/").Body.String()
	for _, want := range []string{
		`name="book" value="chapter2"`,
		`name="profile.chapter2" value="std"`,
		`name="format" value="pdf"`,
		`name="format" value="docx"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("render panel missing %q:\n%s", want, body)
		}
	}
	// A `_quarto-*.yml` that is not on one of the three axes is not a
	// render selection and is not offered.
	if strings.Contains(body, `value="not-an-axis-web"`) {
		t.Errorf("a non-axis profile was offered:\n%s", body)
	}
	// Nothing is selected by default: Render must not kick off a long run
	// before the user has chosen anything.
	if strings.Contains(body, `name="book" value="chapter2" checked`) {
		t.Errorf("topic selected by default:\n%s", body)
	}
}

// A topic offers only the audiences it declares.
func TestRenderPanelShowsTheTopicsOwnAudiences(t *testing.T) {
	srv, root := testServer(t)
	writeProfile(t, root, "audience-pol", "_quarto-vars:\n  audience: \"-pol\"\n")
	writeProfile(t, root, "audience-fw", "_quarto-vars:\n  audience: \"-fw\"\n")
	writeProfile(t, root, "topic-agency",
		"book:\n  title: Agency\nqm:\n  audiences: [pol, fw]\n")
	writeProfile(t, root, "topic-common",
		"book:\n  title: Common\nqm:\n  audiences: [std]\n")

	body := get(t, srv, "/").Body.String()
	section := bookFieldset(t, body, "agency")
	// The audiences sit in the open, right under their topic: no switch to
	// flip before the user can see or change what a topic renders for.
	if !strings.Contains(section, `class="variants"`) {
		t.Errorf("audience group missing:\n%s", section)
	}
	for _, unwanted := range []string{`<details`, `<summary`} {
		if strings.Contains(section, unwanted) {
			t.Errorf("audiences are still behind %q:\n%s", unwanted, section)
		}
	}
	for _, want := range []string{
		`name="profile.agency" value="fw" checked`,
		`name="profile.agency" value="pol" checked`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("audience missing %q:\n%s", want, section)
		}
	}
	if strings.Contains(section, `value="std"`) {
		t.Errorf("an undeclared audience was offered:\n%s", section)
	}

	// One audience is no choice; it is submitted as a hidden field.
	common := bookFieldset(t, body, "common")
	if strings.Contains(common, `class="variants"`) {
		t.Errorf("single-audience topic offers a choice:\n%s", common)
	}
	if !strings.Contains(common, `<input type="hidden" name="profile.common" value="std">`) {
		t.Errorf("single-audience topic does not submit its audience:\n%s", common)
	}
}

// An unchecked topic renders nothing, so its audiences are shown dimmed.
// The dimming is the stylesheet's, read off the topic's own checkbox, so it
// follows every click of it without a round trip; the test pins the rule and
// the markup the rule reads.
func TestRenderPanelDimsTheAudiencesOfAnUncheckedTopic(t *testing.T) {
	srv, root := testServer(t)
	writeProfile(t, root, "audience-pol", "_quarto-vars:\n  audience: \"-pol\"\n")
	writeProfile(t, root, "audience-fw", "_quarto-vars:\n  audience: \"-fw\"\n")
	writeProfile(t, root, "topic-agency",
		"book:\n  title: Agency\nqm:\n  audiences: [pol, fw]\n")

	// The selector reaches from the topic's checkbox to its audiences, so
	// both have to sit in the fieldset the way it expects them to.
	section := bookFieldset(t, get(t, srv, "/").Body.String(), "agency")
	want := `<legend><label><input type="checkbox" name="book"`
	if !strings.Contains(section, want) {
		t.Errorf("topic checkbox is not %s...:\n%s", want, section)
	}
	if !strings.Contains(section, `<div class="variants">`) {
		t.Errorf("audiences are not in a .variants div:\n%s", section)
	}

	css := get(t, srv, "/static/app.css").Body.String()
	rule := `.render-body .book:not(:has(> legend input[name="book"]:checked)) .variants`
	if !strings.Contains(css, rule) {
		t.Errorf("the stylesheet does not dim the audiences of an unchecked topic: %s", rule)
	}
}

func TestRenderSelectionPersistsPerProject(t *testing.T) {
	root := fixture(t)
	writeProfile(t, root, "audience-pol", "_quarto-vars:\n  audience: \"-pol\"\n")
	prefs := filepath.Join(t.TempDir(), "render.json")

	srv, err := newServer(prefs)
	if err != nil {
		t.Fatal(err)
	}
	post(t, srv, "/open", url.Values{"path": {root}})
	form := url.Values{"book": {"chapter2"}, "format": {"docx"}}
	if rec := post(t, srv, "/render/select", form); rec.Code != http.StatusNoContent {
		t.Fatalf("select: status %d: %s", rec.Code, rec.Body)
	}

	// A new server reading the same prefs file (a restart) restores it.
	srv2, err := newServer(prefs)
	if err != nil {
		t.Fatal(err)
	}
	post(t, srv2, "/open", url.Values{"path": {root}})
	body := get(t, srv2, "/").Body.String()
	for _, want := range []string{
		`name="book" value="chapter2" checked`,
		`name="format" value="docx" checked`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("selection not restored, missing %q:\n%s", want, body)
		}
	}
	// The audiences were deliberately left unchecked, so the default must
	// not come back.
	for _, bad := range []string{
		`name="profile.chapter2" value="std" checked`,
		`name="profile.chapter2" value="pol" checked`,
	} {
		if strings.Contains(body, bad) {
			t.Errorf("deselected audience restored as checked:\n%s", body)
		}
	}
	if strings.Contains(body, `name="format" value="pdf" checked`) {
		t.Errorf("deselected format restored as checked:\n%s", body)
	}

	// A different project keeps its own selection. /open swaps #main, which
	// does not reach the header, so the panel comes along out of band.
	other := fixture(t)
	rec := post(t, srv2, "/open", url.Values{"path": {other}})
	if !strings.Contains(rec.Body.String(), `hx-swap-oob="innerHTML:#render-panel"`) {
		t.Errorf("open did not refresh the render panel:\n%s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), `name="book" value="chapter2" checked`) {
		t.Errorf("selection leaked into another project:\n%s", rec.Body)
	}
}

// A saved selection may name a topic or audience that has since been
// deleted; it must neither show up nor break the restore.
func TestSavedSelectionIgnoresRemovedEntries(t *testing.T) {
	root := fixture(t)
	prefs := filepath.Join(t.TempDir(), "render.json")
	saved := `{"` + root + `":{"topics":["chapter2","gone"],` +
		`"audiences":{"chapter2":["std","gone"]},"formats":["pdf"]}}`
	if err := os.WriteFile(prefs, []byte(saved), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := newServer(prefs)
	if err != nil {
		t.Fatal(err)
	}
	post(t, srv, "/open", url.Values{"path": {root}})
	body := get(t, srv, "/").Body.String()
	if strings.Contains(body, `value="gone"`) {
		t.Errorf("removed entry rendered:\n%s", body)
	}
	if !strings.Contains(body, `<input type="hidden" name="profile.chapter2" value="std">`) {
		t.Errorf("surviving audience not restored:\n%s", body)
	}
}
