package qmcore

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// selectionProject writes the profile files a three-axis project needs.
func selectionProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"_quarto-topic-none.yml": "" +
			"qm:\n  formats: [website]\n_quarto-vars:\n  topic: \"\"\n",
		"_quarto-topic-calltaker.yml": "" +
			"book:\n  title: \"Calltaker{{< var audience-title >}}\"\n" +
			"_quarto-vars:\n  topic: calltaker\n" +
			"qm:\n  formats: [handout, slides]\n  audiences: [pol, fw]\n",
		"_quarto-topic-sysadmin.yml": "" +
			"book:\n  title: Systemadministration\n" +
			"_quarto-vars:\n  topic: systemadministration\n" +
			"qm:\n  folder: sysadmin\n  formats: [handout]\n  audiences: [std]\n",
		"_quarto-format-handout.yml": "" +
			"project:\n  type: book\n  output-dir: _output/handout\n" +
			"book:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n",
		"_quarto-format-slides.yml": "" +
			"project:\n  type: default\n  output-dir: _output/slides\n" +
			"qm:\n  output-file: \"{{< var topic >}}{{< var audience >}}\"\n" +
			"  copy:\n    _build/ref.pptx: \"assets/tpl{{< var audience-template >}}.pptx\"\n",
		"_quarto-format-website.yml": "" +
			"project:\n  type: website\n  output-dir: _output/site\n" +
			"qm:\n  output-dir-suffix: \"{{< var audience >}}\"\n  audiences: [pol, fw]\n",
		"_quarto-audience-std.yml": "" +
			"_quarto-vars:\n  audience: \"\"\n  audience-title: \"\"\n  audience-template: \"\"\n",
		"_quarto-audience-pol.yml": "" +
			"_quarto-vars:\n  audience: \"-pol\"\n  audience-title: \" Polizei\"\n  audience-template: _POL\n",
		"_quarto-audience-fw.yml": "" +
			"_quarto-vars:\n  audience: \"-fw\"\n  audience-title: \" Feuerwehr\"\n  audience-template: _FW\n",
	}
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSplitProfileName(t *testing.T) {
	tests := []struct {
		name      string
		wantAxis  Axis
		wantValue string
		wantOK    bool
	}{
		{"topic-calltaker", AxisTopic, "calltaker", true},
		{"_quarto-topic-calltaker", AxisTopic, "calltaker", true},
		{"format-handout-no-tutorials", AxisFormat, "handout-no-tutorials", true},
		{"audience-pol", AxisAudience, "pol", true},
		// The old scheme's names carry no axis and must not be guessed at.
		{"calltaker-pol", "", "", false},
		{"handout", "", "", false},
		{"topic-", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			axis, value, ok := SplitProfileName(tt.name)
			if ok != tt.wantOK || axis != tt.wantAxis || value != tt.wantValue {
				t.Errorf("SplitProfileName(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.name, axis, value, ok, tt.wantAxis, tt.wantValue, tt.wantOK)
			}
		})
	}
}

// The profiles reach Quarto in a fixed order, because Quarto's merge lets
// the first-listed profile win for scalar keys: the format has to precede
// the audience for a book's output-dir to come from the format.
func TestSelectionStringIsInAxisOrder(t *testing.T) {
	s := Selection{Topic: "calltaker", Format: "handout", Audience: "pol"}
	want := "topic-calltaker,format-handout,audience-pol"
	if got := s.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestParseSelection(t *testing.T) {
	got, err := ParseSelection([]string{"format-handout", "audience-pol", "topic-calltaker"})
	if err != nil {
		t.Fatalf("ParseSelection: %v", err)
	}
	want := Selection{Topic: "calltaker", Format: "handout", Audience: "pol"}
	if got != want {
		t.Errorf("ParseSelection = %+v, want %+v", got, want)
	}
}

// Quarto merges two profiles of the same group without a word, concatenating
// their array keys. That has to be an error before anything is rendered.
func TestParseSelectionRejectsTwoOnOneAxis(t *testing.T) {
	_, err := ParseSelection([]string{"topic-calltaker", "topic-dispatcher",
		"format-handout", "audience-pol"})
	if err == nil || !strings.Contains(err.Error(), "two topic profiles") {
		t.Fatalf("error = %v, want a duplicate-axis error", err)
	}
}

func TestParseSelectionRejectsIncompleteAndUnknown(t *testing.T) {
	if _, err := ParseSelection([]string{"topic-calltaker", "format-handout"}); err == nil {
		t.Error("expected an error when an axis is missing")
	}
	if _, err := ParseSelection([]string{"calltaker-pol"}); err == nil {
		t.Error("expected an error for a name with no axis prefix")
	}
}

func TestAxisValues(t *testing.T) {
	root := selectionProject(t)
	got, err := AxisValues(root, AxisFormat)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"handout", "slides", "website"}
	if !slices.Equal(got, want) {
		t.Errorf("AxisValues(format) = %v, want %v", got, want)
	}
}

// A missing profile file is an error here. Quarto ignores one silently, and
// the variables it should have defined then resolve to `?var:audience`,
// which ends up in the output file name.
func TestLoadProfileRejectsMissingFile(t *testing.T) {
	root := selectionProject(t)
	if _, err := LoadProfile(root, "audience-nope"); err == nil {
		t.Fatal("expected an error for a profile with no file")
	}
}

// Quarto concatenates array keys across the base config and the profiles,
// so a chapter list in a profile is added to the one in _quarto.yml.
func TestLoadProfileRejectsChapterList(t *testing.T) {
	root := selectionProject(t)
	path := filepath.Join(root, "_quarto-topic-bad.yml")
	if err := os.WriteFile(path, []byte("book:\n  chapters:\n    - x.qmd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProfile(root, "topic-bad")
	if err == nil || !strings.Contains(err.Error(), "chapters") {
		t.Fatalf("error = %v, want a chapter-list error", err)
	}
}

// The variables of all three profiles are one namespace — that is what lets
// a file name be composed from two independent axes — and the first
// profile to define a key wins, as in Quarto's own merge.
func TestVarsMergeFirstWins(t *testing.T) {
	root := selectionProject(t)
	ps, err := LoadSelection(root, Selection{Topic: "calltaker", Format: "handout", Audience: "pol"})
	if err != nil {
		t.Fatal(err)
	}
	vars := ps.Vars()
	if vars["topic"] != "calltaker" || vars["audience"] != "-pol" {
		t.Errorf("vars = %v, want topic=calltaker audience=-pol", vars)
	}
	title, err := ps.Title()
	if err != nil {
		t.Fatal(err)
	}
	if title != "Calltaker Polizei" {
		t.Errorf("Title() = %q, want %q", title, "Calltaker Polizei")
	}
}

// The content folder is the topic name unless the profile says otherwise —
// `topic: systemadministration` names the output file, `sysadmin/` holds
// the content.
func TestFolderFollowsTheProfile(t *testing.T) {
	root := selectionProject(t)
	for _, tc := range []struct{ topic, want string }{
		{"calltaker", "calltaker"},
		{"sysadmin", "sysadmin"},
		{NoTopic, ""},
	} {
		ps, err := LoadSelection(root, Selection{Topic: tc.topic, Format: "handout", Audience: "std"})
		if err != nil {
			t.Fatal(err)
		}
		if got := ps.Folder(); got != tc.want {
			t.Errorf("Folder(%s) = %q, want %q", tc.topic, got, tc.want)
		}
	}
}

func TestResolveVarsReportsMissing(t *testing.T) {
	got, err := ResolveVars("{{< var a >}}-{{< var b >}}", map[string]string{"a": "x", "b": ""})
	if err != nil {
		t.Fatal(err)
	}
	if got != "x-" {
		t.Errorf("ResolveVars = %q, want %q", got, "x-")
	}
	if _, err := ResolveVars("{{< var nope >}}", map[string]string{}); err == nil {
		t.Error("expected an error for an undefined variable")
	}
}

// Quarto does not resolve {{< var >}} in project: output-dir: — it would
// create a directory of that literal name. Catching it is cheaper than
// finding the directory later.
func TestFormatOutputDirRejectsVariables(t *testing.T) {
	root := selectionProject(t)
	path := filepath.Join(root, "_quarto-format-bad.yml")
	if err := os.WriteFile(path,
		[]byte("project:\n  output-dir: \"_output/{{< var audience >}}\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadProfile(root, "format-bad")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FormatOutputDir(p); err == nil {
		t.Fatal("expected an error for an interpolated output-dir")
	}
}

func TestBuildMatrixExpandsWhatEachTopicDeclares(t *testing.T) {
	root := selectionProject(t)
	sels, err := BuildMatrix(root, Matrix{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range sels {
		got = append(got, s.String())
	}
	slices.Sort(got)
	want := []string{
		"topic-calltaker,format-handout,audience-fw",
		"topic-calltaker,format-handout,audience-pol",
		"topic-calltaker,format-slides,audience-fw",
		"topic-calltaker,format-slides,audience-pol",
		// topic-none declares no audiences, but format-website does, and
		// both sides have to agree.
		"topic-none,format-website,audience-fw",
		"topic-none,format-website,audience-pol",
		"topic-sysadmin,format-handout,audience-std",
	}
	if !slices.Equal(got, want) {
		t.Errorf("matrix =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestBuildMatrixRejectsUnknownNames(t *testing.T) {
	root := selectionProject(t)
	if _, err := BuildMatrix(root, Matrix{Topics: []string{"nope"}}); err == nil {
		t.Error("expected an error for an unknown topic")
	}
	if _, err := BuildMatrix(root, Matrix{Formats: []string{"nope"}}); err == nil {
		t.Error("expected an error for an unknown format")
	}
}

func TestProjectPathStaysInsideTheProject(t *testing.T) {
	root := t.TempDir()
	if _, err := ProjectPath(root, "../secrets"); err == nil {
		t.Error("expected an error for a path outside the project")
	}
	got, err := ProjectPath(root, "assets/x.png")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "assets", "x.png") {
		t.Errorf("ProjectPath = %q", got)
	}
}
