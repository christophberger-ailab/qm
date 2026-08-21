package finalize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a project with the two format profiles that need a
// post-render fix-up, plus a book format that needs none.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"_quarto.yml":                 "project:\n  type: book\n",
		"_quarto-topic-calltaker.yml": "_quarto-vars:\n  topic: calltaker\n",
		// A book names itself: Quarto resolves book: output-file: and puts
		// the file where it belongs, so nothing is left to do here.
		"_quarto-format-handout.yml": "project:\n  type: book\n  output-dir: _output/handout\n" +
			"book:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n",
		// A deck is not a book: a type: default project mirrors the source
		// layout and has no interpolating key to name the output with.
		"_quarto-format-slides.yml": "project:\n  type: default\n  output-dir: _output/slides\n" +
			"qm:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n",
		// The website's directory depends on the audience, and
		// project: output-dir: does not interpolate.
		"_quarto-format-website.yml": "project:\n  type: website\n  output-dir: _output/site\n" +
			"qm:\n  output-dir-suffix: \"{{< var audience >}}\"\n",
		"_quarto-audience-pol.yml": "_quarto-vars:\n  audience: \"-pol\"\n",
		"_quarto-audience-std.yml": "_quarto-vars:\n  audience: \"\"\n",
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

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

// The deck lands under the name of the source file, in a mirror of the
// source directory. It is moved to the top of the output directory and
// named from the topic and the audience.
func TestRunRenamesTheDeck(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/slides/_build/slides.pptx", "deck")

	err := Run(root, []string{"topic-calltaker", "format-slides", "audience-pol"},
		filepath.Join(root, "_output/slides"),
		[]string{"_output/slides/_build/slides.pptx"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exists(root, "_output/slides/calltaker-pol.pptx") {
		t.Error("the deck was not renamed")
	}
	// The mirrored source directory is not part of the output.
	if exists(root, "_output/slides/_build") {
		t.Error("the mirrored _build directory was left behind")
	}
}

func TestRunRenamesTheOutputDirPerAudience(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/site/index.html", "site")

	err := Run(root, []string{"topic-calltaker", "format-website", "audience-pol"},
		filepath.Join(root, "_output/site"), []string{"_output/site/index.html"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exists(root, "_output/site-pol/index.html") {
		t.Error("the output directory was not renamed")
	}
	if exists(root, "_output/site") {
		t.Error("the original output directory is still there")
	}
}

// An audience whose suffix is empty leaves the directory where it is.
func TestRunLeavesTheDirAloneForAnEmptySuffix(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/site/index.html", "site")

	err := Run(root, []string{"topic-calltaker", "format-website", "audience-std"},
		filepath.Join(root, "_output/site"), []string{"_output/site/index.html"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exists(root, "_output/site/index.html") {
		t.Error("the output directory was moved despite an empty suffix")
	}
}

// A previous render's directory is replaced, not merged into.
func TestRunReplacesAnOlderOutputDir(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/site/index.html", "new")
	write(t, root, "_output/site-pol/stale.html", "old")

	err := Run(root, []string{"topic-calltaker", "format-website", "audience-pol"},
		filepath.Join(root, "_output/site"), []string{"_output/site/index.html"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exists(root, "_output/site-pol/stale.html") {
		t.Error("the previous render's files survived")
	}
}

// A book needs no fix-up: Quarto resolves book: output-file: itself.
func TestRunDoesNothingForABook(t *testing.T) {
	root := fixture(t)
	write(t, root, "_output/handout/calltaker-pol.docx", "book")

	err := Run(root, []string{"topic-calltaker", "format-handout", "audience-pol"},
		filepath.Join(root, "_output/handout"),
		[]string{"_output/handout/calltaker-pol.docx"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exists(root, "_output/handout/calltaker-pol.docx") {
		t.Error("the book output was touched")
	}
}

func TestRunRejectsBadSelections(t *testing.T) {
	root := fixture(t)
	if err := Run(root, nil, "", nil, nil); err == nil {
		t.Error("expected an error without a profile")
	}
	err := Run(root, []string{"topic-calltaker", "format-slides", "audience-pol"},
		filepath.Join(root, "_output/slides"), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no output files") {
		t.Errorf("error = %v, want a missing-output-files error", err)
	}
}

func TestSplitFiles(t *testing.T) {
	got := splitFiles("_output/a.docx\n_output/b.epub\n")
	if len(got) != 2 || got[0] != "_output/a.docx" || got[1] != "_output/b.epub" {
		t.Errorf("splitFiles = %v", got)
	}
	if got := splitFiles(""); got != nil {
		t.Errorf("splitFiles = %v, want nil", got)
	}
}
