package bookmaker

import (
	"strings"
	"testing"
)

func lines(s string) []string { return strings.Split(strings.TrimPrefix(s, "\n"), "\n") }

func TestSplitFrontMatter(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		yaml  string
		body  string
		found bool
	}{
		{
			name:  "simple",
			src:   "---\ntitle: A\n---\nbody\n",
			yaml:  "title: A",
			body:  "body\n",
			found: true,
		},
		{
			name:  "closed with dots",
			src:   "---\norder: 3\n...\nbody",
			yaml:  "order: 3",
			body:  "body",
			found: true,
		},
		{
			name:  "no front matter",
			src:   "# Heading\n\ntext",
			body:  "# Heading\n\ntext",
			found: false,
		},
		{
			name:  "unterminated block is treated as body",
			src:   "---\ntitle: A\nbody",
			body:  "---\ntitle: A\nbody",
			found: false,
		},
		{
			name:  "byte order mark",
			src:   "\uFEFF---\ntitle: A\n---\nbody",
			yaml:  "title: A",
			body:  "body",
			found: true,
		},
		{
			name:  "thematic break is not front matter",
			src:   "text\n\n---\n\nmore",
			body:  "text\n\n---\n\nmore",
			found: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yamlText, body, found := splitFrontMatter(tc.src)
			if found != tc.found {
				t.Fatalf("found = %v, want %v", found, tc.found)
			}
			if yamlText != tc.yaml {
				t.Errorf("yaml = %q, want %q", yamlText, tc.yaml)
			}
			if body != tc.body {
				t.Errorf("body = %q, want %q", body, tc.body)
			}
		})
	}
}

func TestParseFrontMatter(t *testing.T) {
	fm, body, err := parseFrontMatter("---\ntitle: \"Die Wartenwand\"\norder: 7\nlisting:\n  type: table\n---\n\ntext\n")
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Die Wartenwand" {
		t.Errorf("Title = %q", fm.Title)
	}
	if !fm.HasOrder || fm.Order != 7 {
		t.Errorf("Order = %d, HasOrder = %v", fm.Order, fm.HasOrder)
	}
	if fm.SortKey() != 7 {
		t.Errorf("SortKey = %d", fm.SortKey())
	}
	if strings.TrimSpace(body) != "text" {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontMatterWithoutOrder(t *testing.T) {
	fm, _, err := parseFrontMatter("---\ntitle: A\n---\n")
	if err != nil {
		t.Fatal(err)
	}
	if fm.HasOrder {
		t.Fatal("HasOrder should be false")
	}
	if fm.SortKey() != noOrder {
		t.Errorf("SortKey = %d, want %d", fm.SortKey(), noOrder)
	}
}

func TestParseFrontMatterInvalidYAML(t *testing.T) {
	if _, _, err := parseFrontMatter("---\ntitle: [unclosed\n---\nbody"); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}
