package bookrender

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// project builds a minimal Quarto website with one book folder whose single
// page carries a slide block.
func project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.qmd":       "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"book/index.qmd":  "---\ntitle: Book\norder: 1\n---\n# Book\n",
		"book/page.qmd":   "---\ntitle: Page\norder: 2\n---\n# Page\n\n::: slide\n## Deck\n:::\n",
		"assets/logo.png": "",
		"_quarto.yml":     "project:\n  type: website\n",
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

// stubQuarto replaces the quarto executable with a shell script recording
// its arguments, and returns the path of the log it writes.
func stubQuarto(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub quarto is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	stub := filepath.Join(dir, "quarto")
	body := "#!/bin/sh\necho \"args: $*\" >> " + log + "\n" + script
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	old := QuartoCommand
	QuartoCommand = stub
	t.Cleanup(func() { QuartoCommand = old })
	return log
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func TestBooksListsBookFolders(t *testing.T) {
	root := project(t)
	if got := Books(root); !slices.Equal(got, []string{"book"}) {
		t.Errorf("Books = %v, want [book]", got)
	}
}

func TestDefaultProfiles(t *testing.T) {
	available := []string{"book", "book-fw", "bookish", "other"}
	want := []string{"book", "book-fw"}
	if got := DefaultProfiles("book", available); !slices.Equal(got, want) {
		t.Errorf("DefaultProfiles = %v, want %v", got, want)
	}
	if got := DefaultProfiles("none", available); got != nil {
		t.Errorf("DefaultProfiles = %v, want nil", got)
	}
}

func TestRunRendersBookAndSlides(t *testing.T) {
	log := stubQuarto(t, "echo rendering\n")
	root := project(t)

	var lines []string
	err := Run(Options{
		Root:    root,
		Books:   []string{"book"},
		Formats: []string{"pdf"},
		Slides:  true,
	}, func(format string, args ...any) {
		lines = append(lines, strings.TrimSpace(fmt.Sprintf(format, args...)))
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := readLog(t, log)
	for _, want := range []string{
		// The book is not named on the command line: the project renders,
		// and its index.qmd includes the flat document.
		"args: render --to pdf --no-clean\n",
		"args: render _slides-build-book.qmd --to revealjs --no-clean\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing invocation %q, got:\n%s", want, got)
		}
	}
	// The command's output reaches the log, line by line.
	if !slices.Contains(lines, "rendering") {
		t.Errorf("quarto output missing from the log: %v", lines)
	}
	if !slices.Contains(lines, "done: 1 book(s) rendered") {
		t.Errorf("no completion line: %v", lines)
	}
	// The flat documents exist only while Quarto reads them.
	for _, name := range []string{"_book-build-book.qmd", "_slides-build-book.qmd"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s not cleaned up", name)
		}
	}
}

func TestRunRendersOncePerProfile(t *testing.T) {
	log := stubQuarto(t, "")
	root := project(t)

	err := Run(Options{
		Root:     root,
		Books:    []string{"book"},
		Profiles: map[string][]string{"book": {"book", "book-fw"}},
		Formats:  []string{"pdf", "docx"},
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := strings.Count(readLog(t, log), "args:"); got != 4 {
		t.Errorf("%d quarto runs, want 4 (2 profiles x 2 formats)", got)
	}
}

// With CombineProfiles the profiles are one composed configuration and must
// all reach Quarto, in a single run per format.
func TestRunCombinesProfilesIntoOneRun(t *testing.T) {
	log := stubQuarto(t, "")
	root := project(t)

	err := Run(Options{
		Root:            root,
		Books:           []string{"book"},
		Profiles:        map[string][]string{"book": {"handout", "book-fw"}},
		CombineProfiles: true,
		Formats:         []string{"pdf"},
		Slides:          true,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := readLog(t, log)
	for _, want := range []string{
		"args: render --to pdf --no-clean --profile handout,book-fw\n",
		"args: render _slides-build-book.qmd --to revealjs --no-clean --profile handout,book-fw\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing invocation %q, got:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "args:"); n != 2 {
		t.Errorf("%d quarto runs, want 2 (one book, one deck):\n%s", n, got)
	}
}

func TestRunReportsFailingBooks(t *testing.T) {
	stubQuarto(t, "echo boom >&2\nexit 1\n")
	root := project(t)

	var lines []string
	err := Run(Options{
		Root:    root,
		Books:   []string{"book", "missing"},
		Formats: []string{"pdf"},
	}, func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	})
	if err == nil {
		t.Fatal("expected an error when a book fails")
	}
	if !strings.Contains(err.Error(), "2 of 2") {
		t.Errorf("error = %v, want it to count both failures", err)
	}
	// A broken book does not hide what went wrong with it.
	if !strings.Contains(strings.Join(lines, "\n"), "boom") {
		t.Errorf("quarto's stderr missing from the log: %v", lines)
	}
}
