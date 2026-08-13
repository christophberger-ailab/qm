package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBalancedFences(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"no fences", "# Heading\n\nBody text.\n", true},
		{"paired", "::: {.callout-note}\nNote text.\n:::\n", true},
		{"paired more colons", ":::: columns\ntext\n::::\n", true},
		{"nested", ":::: columns\n::: {.column}\ntext\n:::\n::::\n", true},
		{"unclosed", "::: {.callout-note}\nNote text.\n", false},
		{"close without open", "text\n:::\n", false},
		{"unclosed nested", ":::: columns\n::: {.column}\ntext\n::::\n", false},
		{"close with trailing spaces", "::: {.x}\ntext\n:::  \n", true},
		{"indented pair", "  ::: {.x}\n  text\n  :::\n", true},
		{"fence in code block ignored", "```\n::: not a fence\n```\n", true},
		{"colons in frontmatter ignored", "---\ntitle: \":::\"\n---\nbody\n", true},
		{"two colons are not a fence", "::x\ntext\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BalancedFences([]byte(tt.src)); got != tt.want {
				t.Errorf("BalancedFences(%q) = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestLoadMarksBadFences(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"good.qmd": "::: {.callout-note}\ntext\n:::\n",
		"bad.qmd":  "::: {.callout-note}\ntext\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tree, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Find("bad.qmd").BadFences != true {
		t.Error("bad.qmd not marked BadFences")
	}
	if tree.Find("good.qmd").BadFences != false {
		t.Error("good.qmd wrongly marked BadFences")
	}
}
