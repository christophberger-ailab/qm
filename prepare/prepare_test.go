package prepare

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cboct/qm/internal/bookrender"
)

// fixture builds a project with one topic, three formats, and two
// audiences — enough to exercise every branch of the hook.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.qmd":           "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"calltaker/index.qmd": "---\ntitle: Calltaker\norder: 1\n---\n# Calltaker\n",
		"calltaker/page.qmd": "---\ntitle: Page\norder: 2\n---\n# Page\n\n" +
			"::: slide\n## Deck\n:::\n",
		"_quarto.yml":            "project:\n  type: book\nbook:\n  chapters:\n    - index.qmd\n",
		"assets/tpl_POL.pptx":    "POL TEMPLATE",
		"assets/tpl_FW.pptx":     "FW TEMPLATE",
		"_quarto-topic-none.yml": "_quarto-vars:\n  topic: \"\"\n",
		"_quarto-topic-calltaker.yml": "book:\n  title: \"Calltaker{{< var audience-title >}}\"\n" +
			"_quarto-vars:\n  topic: calltaker\n",
		"_quarto-topic-broken.yml": "book:\n  title: Broken\n" +
			"_quarto-vars:\n  topic: broken\nqm:\n  folder: does-not-exist\n",
		"_quarto-format-handout.yml": "project:\n  type: book\n  output-dir: _output/handout\n" +
			"book:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n",
		"_quarto-format-slides.yml": "project:\n  type: default\n  output-dir: _output/slides\n" +
			"qm:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n" +
			"  copy:\n    _build/reference-slides.pptx: \"assets/tpl{{< var audience-template >}}.pptx\"\n",
		"_quarto-format-website.yml": "project:\n  type: website\n  output-dir: _output/site\n",
		"_quarto-audience-pol.yml": "make:\n  pol: true\n" +
			"_quarto-vars:\n  audience: \"-pol\"\n  audience-title: \" Polizei\"\n  audience-template: _POL\n",
		"_quarto-audience-fw.yml": "make:\n  fw: true\n" +
			"_quarto-vars:\n  audience: \"-fw\"\n  audience-title: \" Feuerwehr\"\n  audience-template: _FW\n",
		// Defines no audience-template, so the slides format cannot resolve
		// the template it wants to copy.
		"_quarto-audience-std.yml": "_quarto-vars:\n  audience: \"\"\n  audience-title: \"\"\n",
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

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRunBuildsTheSelectedTopic(t *testing.T) {
	root := fixture(t)
	err := Run(root, []string{"topic-calltaker", "format-handout", "audience-pol"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(read(t, bookrender.BookFile(root)), "Calltaker") {
		t.Error("the topic was not flattened into the book document")
	}
	// The deck's title comes from the topic's book title with the
	// audience's variable resolved — the one place both axes meet.
	if !strings.Contains(read(t, bookrender.SlidesFile(root)), "Calltaker Polizei") {
		t.Error("the deck has no resolved title")
	}
}

// A website render selects no topic, and the build documents must then
// contribute nothing to index.qmd.
func TestRunWithoutTopicEmptiesTheBuild(t *testing.T) {
	root := fixture(t)
	if err := Run(root, []string{"topic-calltaker", "format-handout", "audience-pol"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := Run(root, []string{"topic-none", "format-website", "audience-pol"}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(read(t, bookrender.BookFile(root)), "Calltaker") {
		t.Error("the previous topic is still in the book document")
	}
}

// The PPTX template depends on the format *and* the audience, which no
// interpolating Quarto key can express, so it is copied to a fixed path the
// format profile points `reference-doc:` at.
func TestRunCopiesTheAudiencesTemplate(t *testing.T) {
	root := fixture(t)
	for _, tc := range []struct{ audience, want string }{
		{"audience-pol", "POL TEMPLATE"},
		{"audience-fw", "FW TEMPLATE"},
	} {
		err := Run(root, []string{"topic-calltaker", "format-slides", tc.audience}, nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.audience, err)
		}
		got := read(t, filepath.Join(root, "_build", "reference-slides.pptx"))
		if got != tc.want {
			t.Errorf("%s: template = %q, want %q", tc.audience, got, tc.want)
		}
	}
}

// The hook is the project's validation gate: a failing pre-render aborts
// the whole render, which is the only thing standing between a typo and a
// wrongly named — or wrongly targeted — artefact.
func TestRunRejectsBadSelections(t *testing.T) {
	root := fixture(t)
	tests := []struct {
		name     string
		profiles []string
		want     string
	}{
		{"missing axis", []string{"topic-calltaker", "format-handout"}, "audience"},
		{"two on one axis",
			[]string{"topic-calltaker", "topic-none", "format-handout", "audience-pol"},
			"two topic profiles"},
		{"unknown profile",
			[]string{"topic-calltaker", "format-handout", "audience-nope"},
			"no profile file"},
		{"undefined variable",
			[]string{"topic-calltaker", "format-slides", "audience-std"},
			"undefined variable"},
		{"missing content folder",
			[]string{"topic-broken", "format-handout", "audience-pol"},
			"does not exist"},
		{"no profile at all", nil, "no profile selected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Run(root, tt.profiles, nil)
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// Quarto passes the *effective* selection, group defaults included, so the
// hook reads its input from the environment when no flag is given.
func TestActiveProfilesFallsBackToTheEnvironment(t *testing.T) {
	t.Setenv("QUARTO_PROFILE", "topic-calltaker,format-handout,audience-pol")
	got := ActiveProfiles("")
	want := []string{"topic-calltaker", "format-handout", "audience-pol"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ActiveProfiles = %v, want %v", got, want)
	}
	if got := ActiveProfiles("topic-x,format-y,audience-z"); len(got) != 3 || got[0] != "topic-x" {
		t.Errorf("ActiveProfiles = %v, want the flag to win", got)
	}
}
