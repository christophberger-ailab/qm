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
