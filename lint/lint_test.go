package lint

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// A whole-project topic (`all`, `none`) names no content folder, so linting
// it means linting every file — the same as passing no topic at all.
func TestRunWithWholeProjectTopicLintsEverything(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "_quarto-topic-all.yml"), "{}\n")
	if err := os.MkdirAll(filepath.Join(root, "calltaker"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "calltaker", "index.qmd"), "::: a\nx\n:::\n")
	writeFile(t, filepath.Join(root, "index.qmd"), "::: b\ny\n:::\n")

	for _, topic := range []string{"", "all", "none", "topic-all"} {
		if err := Run(root, topic, ""); err != nil {
			t.Errorf("Run(%q) = %v, want no findings", topic, err)
		}
	}
	if err := Run(root, "calltakr", ""); err == nil {
		t.Error("an unknown topic should still be reported as a missing folder")
	}
}

func TestCheckFences(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantLine []int
	}{
		{
			name:     "matched simple",
			body:     "::: {.callout}\nhi\n:::\n",
			wantLine: nil,
		},
		{
			name:     "unmatched open",
			body:     "::: {.callout}\nhi\n",
			wantLine: []int{1},
		},
		{
			name:     "nested matched",
			body:     "::: outer\n::: inner\ntext\n:::\n:::\n",
			wantLine: nil,
		},
		{
			name:     "nested unmatched inner",
			body:     "::: outer\n::: inner\ntext\n:::\n",
			wantLine: []int{1},
		},
		{
			name:     "four-colon fences",
			body:     ":::: {.panel-tabset}\n::: a\nx\n:::\n::::\n",
			wantLine: nil,
		},
		{
			name:     "bare ::: is a closer, not an opener",
			body:     ":::\n",
			wantLine: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "f.qmd")
			writeFile(t, p, tc.body)
			findings, err := CheckFences(p)
			if err != nil {
				t.Fatalf("CheckFences: %v", err)
			}
			if len(findings) != len(tc.wantLine) {
				t.Fatalf("got %d findings, want %d: %+v", len(findings), len(tc.wantLine), findings)
			}
			for i, f := range findings {
				if f.Line != tc.wantLine[i] {
					t.Errorf("finding %d: line %d, want %d", i, f.Line, tc.wantLine[i])
				}
			}
		})
	}
}
