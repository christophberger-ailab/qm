package bookrender

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/christophberger-ailab/qm/internal/qmcore"
)

// project builds a minimal Quarto project with one content folder whose
// single page carries a slide block, and the three profiles one render
// selection needs.
func project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.qmd":                   "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"book/index.qmd":              "---\ntitle: Book\norder: 1\n---\n# Book\n",
		"book/page.qmd":               "---\ntitle: Page\norder: 2\n---\n# Page\n\n::: slide\n## Deck\n:::\n",
		"assets/logo.png":             "",
		"_quarto.yml":                 "project:\n  type: book\nbook:\n  chapters:\n    - index.qmd\n",
		"_quarto-topic-book.yml":      "book:\n  title: Book\n_quarto-vars:\n  topic: book\n",
		"_quarto-topic-none.yml":      "_quarto-vars:\n  topic: \"\"\n",
		"_quarto-format-handout.yml":  "project:\n  type: book\n  output-dir: _output/handout\n",
		"_quarto-audience-std.yml":    "make:\n  perle: true\n_quarto-vars:\n  audience: \"\"\n",
		"_quarto-audience-pol.yml":    "make:\n  pol: true\n_quarto-vars:\n  audience: \"-pol\"\n",
		"_quarto-format-website.yml":  "project:\n  type: website\n  output-dir: _output/site\n",
		"_output/handout/stale.docx":  "",
		"_output/site/stale/page.htm": "",
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

func sel(topic, format, audience string) qmcore.Selection {
	return qmcore.Selection{Topic: topic, Format: format, Audience: audience}
}

func TestContentFoldersListsTopLevelContent(t *testing.T) {
	root := project(t)
	if got := ContentFolders(root); !slices.Equal(got, []string{"book"}) {
		t.Errorf("ContentFolders = %v, want [book]", got)
	}
}

// The build documents are written under fixed names in _build/: that is
// what lets index.qmd hold one topic-independent include.
func TestWriteBuildWritesFixedNames(t *testing.T) {
	root := project(t)
	if err := WriteBuild(root, "book", "Book Polizei", nil); err != nil {
		t.Fatalf("WriteBuild: %v", err)
	}
	book, err := os.ReadFile(BookFile(root))
	if err != nil {
		t.Fatalf("book file: %v", err)
	}
	if !strings.Contains(string(book), "Book") {
		t.Errorf("book document does not hold the flattened content:\n%s", book)
	}
	slides, err := os.ReadFile(SlidesFile(root))
	if err != nil {
		t.Fatalf("slides file: %v", err)
	}
	// The deck is a document of its own and needs a title of its own; the
	// divs it is built from carry none.
	if !strings.Contains(string(slides), "title: 'Book Polizei'") {
		t.Errorf("deck has no title:\n%s", slides)
	}
	if !strings.Contains(string(slides), "## Deck") {
		t.Errorf("deck content missing:\n%s", slides)
	}
}

// A selection with no topic — a website render — still has to leave both
// build documents in place, and they must contribute nothing.
func TestWriteBuildWithoutTopicEmptiesTheDocuments(t *testing.T) {
	root := project(t)
	if err := WriteBuild(root, "book", "Book", nil); err != nil {
		t.Fatalf("WriteBuild: %v", err)
	}
	if err := WriteBuild(root, "", "", nil); err != nil {
		t.Fatalf("WriteBuild: %v", err)
	}
	for _, p := range []string{BookFile(root), SlidesFile(root)} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if strings.Contains(string(b), "#") {
			t.Errorf("%s still holds headings, which would leak into the "+
				"chapter title:\n%s", p, b)
		}
	}
}

func TestRunRendersOncePerSelection(t *testing.T) {
	log := stubQuarto(t, "echo rendering\n")
	root := project(t)

	var lines []string
	err := Run(Options{
		Root: root,
		Selections: []qmcore.Selection{
			sel("book", "handout", "pol"),
			sel("book", "handout", "std"),
		},
	}, func(format string, args ...any) {
		lines = append(lines, strings.TrimSpace(fmt.Sprintf(format, args...)))
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := readLog(t, log)
	for _, want := range []string{
		// No input file: the project renders, and its index.qmd includes
		// the flattened topic. No --to: the format profile declares the
		// formats and Quarto renders them all in one pass.
		"args: render --profile topic-book,format-handout,audience-pol --no-clean\n",
		"args: render --profile topic-book,format-handout,audience-std --no-clean\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing invocation %q, got:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "args:"); n != 2 {
		t.Errorf("%d quarto runs, want 2:\n%s", n, got)
	}
	if !slices.Contains(lines, "rendering") {
		t.Errorf("quarto output missing from the log: %v", lines)
	}
	if !slices.Contains(lines, "done: 2 render(s)") {
		t.Errorf("no completion line: %v", lines)
	}
}

// Every run passes --no-clean, because several topics share one output
// directory. Clean therefore has to empty those directories itself, once.
func TestRunCleanEmptiesEachOutputDirOnce(t *testing.T) {
	stubQuarto(t, "")
	root := project(t)

	err := Run(Options{
		Root: root,
		Selections: []qmcore.Selection{
			sel("book", "handout", "pol"),
			sel("book", "handout", "std"),
			sel("none", "website", "std"),
		},
		Clean: true,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, stale := range []string{"_output/handout/stale.docx", "_output/site/stale/page.htm"} {
		if _, err := os.Stat(filepath.Join(root, stale)); !os.IsNotExist(err) {
			t.Errorf("%s survived --clean", stale)
		}
	}
}

func TestRunDryRunInvokesNothing(t *testing.T) {
	log := stubQuarto(t, "")
	root := project(t)

	err := Run(Options{
		Root:       root,
		Selections: []qmcore.Selection{sel("book", "handout", "pol")},
		DryRun:     true,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := readLog(t, log); got != "" {
		t.Errorf("quarto was invoked despite --dry-run:\n%s", got)
	}
}

func TestRunReportsFailingSelections(t *testing.T) {
	stubQuarto(t, "echo boom >&2\nexit 1\n")
	root := project(t)

	var lines []string
	err := Run(Options{
		Root: root,
		Selections: []qmcore.Selection{
			sel("book", "handout", "pol"),
			sel("book", "handout", "std"),
		},
	}, func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	})
	if err == nil {
		t.Fatal("expected an error when a render fails")
	}
	if !strings.Contains(err.Error(), "2 of 2") {
		t.Errorf("error = %v, want it to count both failures", err)
	}
	// A broken render does not hide what went wrong with it.
	if !strings.Contains(strings.Join(lines, "\n"), "boom") {
		t.Errorf("quarto's stderr missing from the log: %v", lines)
	}
}
