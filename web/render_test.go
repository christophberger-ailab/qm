package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

// writeConfig replaces the fixture project's qm.yml.
func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "qm.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A render from the panel writes what the selection names.
func TestRenderWritesTheSelection(t *testing.T) {
	srv, root := testServer(t)

	rec := post(t, srv, "/render", renderForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("render: status %d: %s", rec.Code, rec.Body)
	}
	st := waitForRender(t, srv)
	if st.Failed {
		t.Fatalf("render failed:\n%s", strings.Join(st.Lines, "\n"))
	}
	if _, err := os.Stat(filepath.Join(root, "_output", "pdf", "chapter2.pdf")); err != nil {
		t.Errorf("no PDF: %v\n%s", err, strings.Join(st.Lines, "\n"))
	}
}

// Every checked format and audience is a combination of its own; a website
// covers every checked topic at once.
func TestRenderExpandsTheSelectedMatrix(t *testing.T) {
	srv, root := testServer(t)
	writeConfig(t, root, "formats:\n  pdf: {type: pdf}\n  web: {type: html}\naudiences: [std, pol]\ntopics:\n  chapter2:\n")

	form := url.Values{
		"book":             {"chapter2"},
		"profile.chapter2": {"std", "pol"},
		"format":           {"pdf", "web"},
	}
	if rec := post(t, srv, "/render", form); rec.Code != http.StatusOK {
		t.Fatalf("render: status %d: %s", rec.Code, rec.Body)
	}
	st := waitForRender(t, srv)
	if st.Failed {
		t.Fatalf("render failed:\n%s", strings.Join(st.Lines, "\n"))
	}
	log := strings.Join(st.Lines, "\n")
	for _, want := range []string{
		"web · std (site)", "web · pol (site)",
		"chapter2 · pdf · std", "chapter2 · pdf · pol",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("missing render %q:\n%s", want, log)
		}
	}
	if n := strings.Count(log, "] "); n != 4 {
		t.Errorf("got %d renders, want 4:\n%s", n, log)
	}
	for _, p := range []string{"_output/web-pol/index.html", "_output/pdf/chapter2-pol.pdf"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestRenderReportsFailure(t *testing.T) {
	srv, root := testServer(t)
	writeConfig(t, root, "formats:\n  pdf: {type: pdf}\naudiences: [std]\ntopics:\n  chapter2: {cover: missing.png}\n")

	if rec := post(t, srv, "/render", renderForm()); rec.Code != http.StatusOK {
		t.Fatalf("render: status %d", rec.Code)
	}
	st := waitForRender(t, srv)
	if !st.Failed {
		t.Errorf("failing render not reported as failed:\n%s", strings.Join(st.Lines, "\n"))
	}
	if out := strings.Join(st.Lines, "\n"); !strings.Contains(out, "missing.png") {
		t.Errorf("the cause is missing from the log:\n%s", out)
	}
}

// A broken qm.yml is shown in the panel instead of hiding it.
func TestRenderPanelShowsAConfigError(t *testing.T) {
	srv, root := testServer(t)
	writeConfig(t, root, "formatz: {}\n")
	body := get(t, srv, "/").Body.String()
	if !strings.Contains(body, `class="render-error"`) || !strings.Contains(body, "formatz") {
		t.Errorf("config error not shown:\n%s", body)
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
	// Nothing is selected by default: Render must not kick off a long run
	// before the user has chosen anything.
	if strings.Contains(body, `name="book" value="chapter2" checked`) {
		t.Errorf("topic selected by default:\n%s", body)
	}
}

// A topic offers only the audiences it declares.
func TestRenderPanelShowsTheTopicsOwnAudiences(t *testing.T) {
	srv, root := testServer(t)
	for _, d := range []string{"agency", "common"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
		os.WriteFile(filepath.Join(root, d, "index.qmd"), []byte("# x\n"), 0o644)
	}
	writeConfig(t, root, "formats: [pdf, docx]\naudiences: [std, pol, fw]\n"+
		"topics:\n  chapter2:\n  agency: {audiences: [pol, fw]}\n  common: {audiences: [std]}\n")

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
	os.MkdirAll(filepath.Join(root, "agency"), 0o755)
	os.WriteFile(filepath.Join(root, "agency", "index.qmd"), []byte("# x\n"), 0o644)
	writeConfig(t, root, "formats: [pdf, docx]\naudiences: [std, pol, fw]\n"+
		"topics:\n  chapter2:\n  agency: {audiences: [pol, fw]}\n")

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
	writeConfig(t, root, "formats: [pdf, docx]\naudiences: [std, pol]\ntopics:\n  chapter2:\n")
	prefs := filepath.Join(t.TempDir(), configFileName)

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
	prefs := filepath.Join(t.TempDir(), configFileName)
	saved := defaultConfig()
	saved.Projects[root] = projectPrefs{
		Topics:    []string{"chapter2", "gone"},
		Audiences: map[string][]string{"chapter2": {"std", "gone"}},
		Formats:   []string{"pdf"},
	}
	if err := saveConfigFile(prefs, saved); err != nil {
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
