package render

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/cboct/qm/internal/bookrender"
	"github.com/cboct/qm/internal/qmcore"
)

// fixture builds a small project with two topics. `chapter2` takes part in
// both formats and both audiences; `extra` declares a narrower matrix.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.qmd":           "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"chapter2/index.qmd":  "---\ntitle: Chapter 2\norder: 2\n---\n# Two\n",
		"chapter2/second.qmd": "---\ntitle: Second\norder: 1\n---\n# Second\n",
		"extra/index.qmd":     "---\ntitle: Extra\norder: 3\n---\n# Extra\n",
		"_quarto.yml":         "project:\n  type: book\nbook:\n  chapters:\n    - index.qmd\n",

		"_quarto-topic-chapter2.yml": "book:\n  title: Two\n_quarto-vars:\n  topic: chapter2\n",
		"_quarto-topic-extra.yml": "book:\n  title: Extra\n_quarto-vars:\n  topic: extra\n" +
			"qm:\n  formats: [handbook]\n  audiences: [std]\n",
		"_quarto-format-handout.yml":  "project:\n  type: book\n  output-dir: _output/handout\n",
		"_quarto-format-handbook.yml": "project:\n  type: book\n  output-dir: _output/handbook\n",
		"_quarto-audience-std.yml":    "_quarto-vars:\n  audience: \"\"\n",
		"_quarto-audience-pol.yml":    "_quarto-vars:\n  audience: \"-pol\"\n",
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

func selections(opts bookrender.Options) []string {
	out := make([]string, 0, len(opts.Selections))
	for _, s := range opts.Selections {
		out = append(out, s.String())
	}
	return out
}

// The default is the whole matrix — but only the part each topic declares
// it takes part in. `extra` has one format and one audience, so the full
// cross product of 2 × 2 × 2 does not apply to it.
func TestBuildOptionsExpandsTheDeclaredMatrix(t *testing.T) {
	root := fixture(t)
	opts, err := BuildOptions(root, qmcore.Matrix{}, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"topic-chapter2,format-handbook,audience-pol",
		"topic-chapter2,format-handbook,audience-std",
		"topic-chapter2,format-handout,audience-pol",
		"topic-chapter2,format-handout,audience-std",
		"topic-extra,format-handbook,audience-std",
	}
	got := selections(opts)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("selections =\n%v\nwant\n%v", got, want)
	}
}

func TestBuildOptionsNarrowsByFlag(t *testing.T) {
	root := fixture(t)
	opts, err := BuildOptions(root, qmcore.Matrix{
		Topics:    []string{"chapter2"},
		Formats:   []string{"handout"},
		Audiences: []string{"pol"},
	}, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"topic-chapter2,format-handout,audience-pol"}
	if got := selections(opts); !slices.Equal(got, want) {
		t.Errorf("selections = %v, want %v", got, want)
	}
}

// A topic never renders in a format it does not declare, even when the
// flag asks for it.
func TestBuildOptionsHonoursTheTopicsOwnAxes(t *testing.T) {
	root := fixture(t)
	opts, err := BuildOptions(root, qmcore.Matrix{
		Topics:  []string{"extra"},
		Formats: []string{"handout", "handbook"},
	}, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"topic-extra,format-handbook,audience-std"}
	if got := selections(opts); !slices.Equal(got, want) {
		t.Errorf("selections = %v, want %v", got, want)
	}
}

func TestBuildOptionsRejectsUnknownNames(t *testing.T) {
	root := fixture(t)
	for _, req := range []qmcore.Matrix{
		{Topics: []string{"nope"}},
		{Formats: []string{"nope"}},
		{Audiences: []string{"nope"}},
	} {
		if _, err := BuildOptions(root, req, false, false); err == nil {
			t.Errorf("%+v: expected an error for a name no profile backs", req)
		}
	}
}

func TestBuildOptionsNeedsProfiles(t *testing.T) {
	if _, err := BuildOptions(t.TempDir(), qmcore.Matrix{}, false, false); err == nil {
		t.Fatal("expected an error for a project without profiles")
	}
}

// One `quarto render --profile <t>,<f>,<a>` per selection, and nothing
// else: no input file, no --to, no --output-dir.
func TestRunInvokesQuartoPerSelection(t *testing.T) {
	log := fakeQuarto(t)
	root := fixture(t)

	err := Run(root, qmcore.Matrix{Topics: []string{"chapter2"}, Formats: []string{"handout"}},
		false, false)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	got := calls(t, log)
	for _, want := range []string{
		"args: render --profile topic-chapter2,format-handout,audience-pol --no-clean\n",
		"args: render --profile topic-chapter2,format-handout,audience-std --no-clean\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing invocation %q, got:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "args:"); n != 2 {
		t.Errorf("%d quarto runs, want 2:\n%s", n, got)
	}
}

func TestRunReportsFailure(t *testing.T) {
	fakeQuarto(t)
	root := fixture(t)
	if err := os.WriteFile(bookrender.QuartoCommand,
		[]byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Run(root, qmcore.Matrix{Topics: []string{"extra"}}, false, false); err == nil {
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
