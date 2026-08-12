package render

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/cboct/qm/internal/bookrender"
)

// fixture builds a small project with two book folders and a profile for
// each: `chapter2` carries `_quarto-chapter2*.yml`, `extra` has none.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.qmd":               "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"chapter2/index.qmd":      "---\ntitle: Chapter 2\norder: 2\n---\n# Two\n",
		"chapter2/second.qmd":     "---\ntitle: Second\norder: 1\n---\n# Second\n",
		"extra/index.qmd":         "---\ntitle: Extra\norder: 3\n---\n# Extra\n",
		"_quarto.yml":             "project:\n  type: book\nbook:\n  chapters:\n    - index.qmd\n",
		"_quarto-chapter2.yml":    "book:\n  chapters:\n    - index.qmd\n",
		"_quarto-chapter2-fw.yml": "book:\n  chapters:\n    - index.qmd\n",
		"_quarto-web.yml":         "format:\n  html: default\n",
	}
	for name, content := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// fakeQuarto installs a stub `quarto` that records its arguments, and
// returns the path of the log it writes.
func fakeQuarto(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub quarto is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	stub := filepath.Join(dir, "quarto")
	script := "#!/bin/sh\necho \"args: $*\" >> " + log + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := bookrender.QuartoCommand
	bookrender.QuartoCommand = stub
	t.Cleanup(func() { bookrender.QuartoCommand = old })
	return log
}

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

func TestBuildOptionsRendersEveryBookByDefault(t *testing.T) {
	root := fixture(t)
	opts, err := BuildOptions(root, nil, nil, DefaultFormats, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"chapter2", "extra"}
	if !slices.Equal(opts.Books, want) {
		t.Errorf("Books = %v, want %v", opts.Books, want)
	}
	if !slices.Equal(opts.Formats, []string{"pdf", "docx"}) {
		t.Errorf("Formats = %v, want pdf docx", opts.Formats)
	}
}

// Without --profile a book is rendered with the profiles named after it.
func TestBuildOptionsDefaultsProfilesToTheBookName(t *testing.T) {
	root := fixture(t)
	opts, err := BuildOptions(root, nil, nil, DefaultFormats, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"chapter2", "chapter2-fw"}
	if !slices.Equal(opts.Profiles["chapter2"], want) {
		t.Errorf("profiles of chapter2 = %v, want %v", opts.Profiles["chapter2"], want)
	}
	// `_quarto-web.yml` declares no book, and nothing is named after
	// `extra`, so that book renders with the project's default config.
	if len(opts.Profiles["extra"]) != 0 {
		t.Errorf("profiles of extra = %v, want none", opts.Profiles["extra"])
	}
}

func TestBuildOptionsProfileFlagAppliesToEveryBook(t *testing.T) {
	root := fixture(t)
	opts, err := BuildOptions(root, []string{"chapter2"}, []string{"chapter2-fw"},
		[]string{"pdf"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(opts.Profiles["chapter2"], []string{"chapter2-fw"}) {
		t.Errorf("profiles = %v, want chapter2-fw", opts.Profiles["chapter2"])
	}
}

func TestBuildOptionsRejectsUnknownBook(t *testing.T) {
	root := fixture(t)
	if _, err := BuildOptions(root, []string{"nope"}, nil, DefaultFormats, false); err == nil {
		t.Fatal("expected an error for a folder that is not a book")
	}
}

func TestBuildOptionsNeedsAnOutput(t *testing.T) {
	root := fixture(t)
	if _, err := BuildOptions(root, nil, nil, nil, false); err == nil {
		t.Fatal("expected an error when neither formats nor slides are selected")
	}
	// --slides alone is a complete request.
	if _, err := BuildOptions(root, nil, nil, nil, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildOptionsNeedsBookFolders(t *testing.T) {
	if _, err := BuildOptions(t.TempDir(), nil, nil, DefaultFormats, false); err == nil {
		t.Fatal("expected an error for a project without book folders")
	}
}

// Run flattens the book and calls quarto once per profile and format.
func TestRunInvokesQuartoPerProfileAndFormat(t *testing.T) {
	log := fakeQuarto(t)
	root := fixture(t)

	if err := Run(root, []string{"chapter2"}, nil, []string{"pdf"}, false); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	got := calls(t, log)
	for _, want := range []string{
		"args: render _book-build-chapter2.qmd --to pdf --no-clean --profile chapter2\n",
		"args: render _book-build-chapter2.qmd --to pdf --no-clean --profile chapter2-fw\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing invocation %q, got:\n%s", want, got)
		}
	}
	// The flat document is temporary and gone once the render is done.
	if _, err := os.Stat(filepath.Join(root, "_book-build-chapter2.qmd")); !os.IsNotExist(err) {
		t.Error("_book-build-chapter2.qmd not cleaned up")
	}
}

func TestRunReportsFailure(t *testing.T) {
	fakeQuarto(t)
	root := fixture(t)
	if err := os.WriteFile(bookrender.QuartoCommand,
		[]byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Run(root, []string{"chapter2"}, nil, []string{"pdf"}, false); err == nil {
		t.Fatal("expected an error when quarto fails")
	}
}

func TestSplitList(t *testing.T) {
	if got := splitList(" pdf , ,docx,"); !slices.Equal(got, []string{"pdf", "docx"}) {
		t.Errorf("splitList = %v, want pdf docx", got)
	}
	if got := splitList("  "); got != nil {
		t.Errorf("splitList = %v, want nil", got)
	}
}
