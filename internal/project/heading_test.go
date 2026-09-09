package project

import "testing"

func TestFirstHeading(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "simple heading after frontmatter",
			src:  "---\norder: 1\n---\n\n# Real Title\n\nBody.\n",
			want: "Real Title",
		},
		{
			name: "no frontmatter",
			src:  "# Just a heading\n\ntext\n",
			want: "Just a heading",
		},
		{
			name: "deeper heading level",
			src:  "### Section Three\n",
			want: "Section Three",
		},
		{
			name: "first heading wins",
			src:  "intro text\n\n## First\n\n## Second\n",
			want: "First",
		},
		{
			name: "heading inside fenced code block is skipped",
			src:  "```\n# not a title\n```\n\n# Actual Title\n",
			want: "Actual Title",
		},
		{
			name: "tilde fenced code block is skipped",
			src:  "~~~\n# nope\n~~~\n# Yes\n",
			want: "Yes",
		},
		{
			name: "only heading is inside code fence",
			src:  "```\n# hidden\n```\n",
			want: "",
		},
		{
			name: "no heading at all",
			src:  "just plain text\nwith no heading\n",
			want: "",
		},
		{
			name: "hashtag without space is not a heading",
			src:  "#hashtag not a heading\n",
			want: "",
		},
		{
			name: "closing hash sequence trimmed",
			src:  "# Title #\n",
			want: "Title",
		},
		{
			name: "closing hash run trimmed",
			src:  "## Chapter ##\n",
			want: "Chapter",
		},
		{
			name: "empty heading text falls through",
			src:  "# \n# Next\n",
			want: "Next",
		},
		{
			name: "tab after hashes",
			src:  "#\tTabbed\n",
			want: "Tabbed",
		},
		{
			name: "attribute block stripped",
			src:  "# PERLE OnCall-Schulungen {.unnumbered .unlisted}\n",
			want: "PERLE OnCall-Schulungen",
		},
		{
			name: "attribute block without a space before it",
			src:  "## Title{#sec-title}\n",
			want: "Title",
		},
		{
			name: "attribute block inside the closing hash run",
			src:  "## Chapter {.unnumbered} ##\n",
			want: "Chapter",
		},
		{
			name: "span attributes are not a heading attribute block",
			src:  "# [Polizei-]{.pol}[Feuerwehr-]{.fw}Spickzettel\n",
			want: "[Polizei-]{.pol}[Feuerwehr-]{.fw}Spickzettel",
		},
		{
			name: "trailing span keeps its attributes",
			src:  "# Spickzettel [Polizei]{.pol}\n",
			want: "Spickzettel [Polizei]{.pol}",
		},
		{
			name: "heading of nothing but an attribute block falls through",
			src:  "# {.unlisted}\n# Next\n",
			want: "Next",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstHeading([]byte(tc.src)); got != tc.want {
				t.Errorf("FirstHeading() = %q, want %q", got, tc.want)
			}
		})
	}
}
