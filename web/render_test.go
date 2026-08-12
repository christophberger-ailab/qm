package web

import (
	"net/http"
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
// file and notes whether the input file it was pointed at exists, which is
// how the tests check that the flat document is present while Quarto runs
// and gone afterwards.
func fakeQuarto(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub quarto is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	stub := filepath.Join(dir, "quarto")
	script := "#!/bin/sh\n" +
		"echo \"args: $*\" >> " + log + "\n" +
		"if [ -f \"$2\" ]; then echo \"present: $2\" >> " + log + "; fi\n" +
		"if [ -f \"$2\" ]; then cat \"$2\" >> " + log + ".input; fi\n" +
		"echo \"pandoc output for $2\"\n"
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

// renderForm is the panel's form for rendering chapter2 as PDF.
func renderForm() url.Values {
	return url.Values{
		"book":             {"chapter2"},
		"profile.chapter2": {"chapter2"},
		"format":           {"pdf"},
	}
}

func TestRenderFlattensAndRunsQuarto(t *testing.T) {
	log := fakeQuarto(t)
	srv, root := testServer(t)

	rec := post(t, srv, "/render", renderForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("render: status %d: %s", rec.Code, rec.Body)
	}
	st := waitForRender(t, srv)
	if st.Failed {
		t.Fatalf("render failed:\n%s", strings.Join(st.Lines, "\n"))
	}

	got := calls(t, log)
	if !strings.Contains(got, "args: render _book-build-chapter2.qmd --to pdf --no-clean --profile chapter2") {
		t.Errorf("unexpected quarto invocation:\n%s", got)
	}
	// The flat document has to be a real file while Quarto reads it.
	if !strings.Contains(got, "present: _book-build-chapter2.qmd") {
		t.Errorf("flat document missing while quarto ran:\n%s", got)
	}
	// And it is the flattened book, not one of the source pages.
	input, err := os.ReadFile(log + ".input")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Chapter 2", "Second", "Third"} {
		if !strings.Contains(string(input), want) {
			t.Errorf("flat document missing %q:\n%s", want, input)
		}
	}

	// It is removed once the render is done.
	if _, err := os.Stat(filepath.Join(root, "_book-build-chapter2.qmd")); !os.IsNotExist(err) {
		t.Error("_book-build-chapter2.qmd not cleaned up")
	}
}

// Every checked profile gets a render run of its own: a book's profiles
// select alternative variants of it rather than combining into one document.
func TestRenderRunsOncePerProfileAndFormat(t *testing.T) {
	log := fakeQuarto(t)
	srv, root := testServer(t)
	cfg := "book:\n  chapters:\n    - index.qmd\n"
	if err := os.WriteFile(filepath.Join(root, "_quarto-chapter2-pol.yml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"book":             {"chapter2"},
		"profile.chapter2": {"chapter2", "chapter2-pol"},
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
		"--to pdf --no-clean --profile chapter2\n",
		"--to docx --no-clean --profile chapter2\n",
		"--to pdf --no-clean --profile chapter2-pol",
		"--to docx --no-clean --profile chapter2-pol",
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
	for _, flag := range []string{"--output", "--output-dir"} {
		if strings.Contains(got, flag) {
			t.Errorf("%s overrides the profile's output setting:\n%s", flag, got)
		}
	}
}

// A book with no profile checked still renders once, without --profile.
func TestRenderWithoutProfile(t *testing.T) {
	log := fakeQuarto(t)
	srv, _ := testServer(t)

	form := url.Values{"book": {"chapter2"}, "format": {"pdf"}}
	if rec := post(t, srv, "/render", form); rec.Code != http.StatusOK {
		t.Fatalf("render: status %d: %s", rec.Code, rec.Body)
	}
	if st := waitForRender(t, srv); st.Failed {
		t.Fatalf("render failed:\n%s", strings.Join(st.Lines, "\n"))
	}

	got := calls(t, log)
	if !strings.Contains(got, "args: render _book-build-chapter2.qmd --to pdf --no-clean\n") {
		t.Errorf("unexpected quarto invocation:\n%s", got)
	}
	if strings.Contains(got, "--profile") {
		t.Errorf("unchecked profile still passed:\n%s", got)
	}
}

// A failing quarto run is reported and does not leave the flat document
// behind.
func TestRenderReportsFailure(t *testing.T) {
	fakeQuarto(t)
	srv, root := testServer(t)
	if err := os.WriteFile(bookrender.QuartoCommand, []byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755); err != nil {
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
	if _, err := os.Stat(filepath.Join(root, "_book-build-chapter2.qmd")); !os.IsNotExist(err) {
		t.Error("flat document left behind after a failed render")
	}
}

// Rendering nothing is a mistake worth naming rather than a no-op.
func TestRenderNeedsBookAndFormat(t *testing.T) {
	fakeQuarto(t)
	srv, _ := testServer(t)

	rec := post(t, srv, "/render", url.Values{"format": {"pdf"}})
	if !strings.Contains(rec.Body.String(), "select at least one book") {
		t.Errorf("no book selected: %s", rec.Body)
	}
	rec = post(t, srv, "/render", url.Values{"book": {"chapter2"}})
	if !strings.Contains(rec.Body.String(), "select at least one output format") {
		t.Errorf("no format selected: %s", rec.Body)
	}
	if srv.job.state().Running {
		t.Error("an incomplete selection started a render")
	}
}

// The render panel lists the project's book folders and book profiles.
func TestRenderPanelListsBooks(t *testing.T) {
	srv, _ := testServer(t)
	body := get(t, srv, "/").Body.String()
	for _, want := range []string{
		`name="book" value="chapter2"`,
		`name="profile.chapter2" value="chapter2"`,
		`name="format" value="pdf"`,
		`name="format" value="docx"`,
		`name="slides"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("render panel missing %q:\n%s", want, body)
		}
	}
	// _quarto-web.yml has no book key, so it is not a book profile.
	if strings.Contains(body, `value="web"`) {
		t.Errorf("profile without a book key offered:\n%s", body)
	}
	// Formats are checked by default, books are not: Render must not kick
	// off a long run before the user has chosen anything.
	if !strings.Contains(body, `name="format" value="pdf" checked`) {
		t.Errorf("pdf not selected by default:\n%s", body)
	}
	if strings.Contains(body, `name="book" value="chapter2" checked`) {
		t.Errorf("book selected by default:\n%s", body)
	}
}

func TestRenderSelectionPersistsPerProject(t *testing.T) {
	root := fixture(t)
	prefs := filepath.Join(t.TempDir(), "render.json")

	srv, err := newServer(prefs)
	if err != nil {
		t.Fatal(err)
	}
	post(t, srv, "/open", url.Values{"path": {root}})
	form := url.Values{"book": {"chapter2"}, "format": {"docx"}, "slides": {"1"}}
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
		`name="slides" value="1" checked`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("selection not restored, missing %q:\n%s", want, body)
		}
	}
	// The profile was deliberately left unchecked, so the name-matched
	// default must not come back.
	if strings.Contains(body, `name="profile.chapter2" value="chapter2" checked`) {
		t.Errorf("deselected profile restored as checked:\n%s", body)
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

// A saved selection may name a book or profile that has since been deleted;
// it must neither show up nor break the restore.
func TestSavedSelectionIgnoresRemovedEntries(t *testing.T) {
	root := fixture(t)
	prefs := filepath.Join(t.TempDir(), "render.json")
	saved := `{"` + root + `":{"books":["chapter2","gone"],` +
		`"profiles":{"chapter2":["chapter2","gone"]},"formats":["pdf"]}}`
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
		t.Errorf("deleted entry rendered:\n%s", body)
	}
	if !strings.Contains(body, `name="profile.chapter2" value="chapter2" checked`) {
		t.Errorf("surviving profile not restored:\n%s", body)
	}
}

// The slide deck is built from the pages' ::: slide blocks and rendered to
// revealjs, not to the book's formats.
func TestRenderSlides(t *testing.T) {
	log := fakeQuarto(t)
	srv, root := testServer(t)
	page := "---\ntitle: Second\norder: 1\n---\n# Second\n\n::: slide\n## A slide\n:::\n"
	if err := os.WriteFile(filepath.Join(root, "chapter2", "second.qmd"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"book": {"chapter2"}, "format": {"pdf"}, "slides": {"1"}}
	if rec := post(t, srv, "/render", form); rec.Code != http.StatusOK {
		t.Fatalf("render: status %d: %s", rec.Code, rec.Body)
	}
	if st := waitForRender(t, srv); st.Failed {
		t.Fatalf("render failed:\n%s", strings.Join(st.Lines, "\n"))
	}

	got := calls(t, log)
	if !strings.Contains(got, "args: render _slides-build-chapter2.qmd --to revealjs --no-clean") {
		t.Errorf("slides not rendered:\n%s", got)
	}
	// The slide content leaves the book.
	input, _ := os.ReadFile(log + ".input")
	if strings.Count(string(input), "A slide") != 1 {
		t.Errorf("slide content should appear in the deck only:\n%s", input)
	}
	if _, err := os.Stat(filepath.Join(root, "_slides-build-chapter2.qmd")); !os.IsNotExist(err) {
		t.Error("_slides-build-chapter2.qmd not cleaned up")
	}
}

// A book without a single ::: slide block gets no deck, and says so.
func TestRenderSlidesWithoutSlideBlocks(t *testing.T) {
	log := fakeQuarto(t)
	srv, _ := testServer(t)

	form := url.Values{"book": {"chapter2"}, "format": {"pdf"}, "slides": {"1"}}
	post(t, srv, "/render", form)
	st := waitForRender(t, srv)
	if st.Failed {
		t.Fatalf("render failed:\n%s", strings.Join(st.Lines, "\n"))
	}
	if strings.Contains(calls(t, log), "slides-build") {
		t.Error("a deck was rendered although there are no slide blocks")
	}
	if !strings.Contains(strings.Join(st.Lines, "\n"), "no ::: slide blocks") {
		t.Errorf("missing note about the absent deck:\n%s", strings.Join(st.Lines, "\n"))
	}
}
