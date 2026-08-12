package bookmaker

import (
	"testing"
)

func TestAnchorID(t *testing.T) {
	tests := []struct{ rel, want string }{
		{"dispatcher", "bm-dispatcher"},
		{"dispatcher/einsatz-starten", "bm-dispatcher-einsatz-starten"},
		{"dispatcher/betriebszustaende_FW", "bm-dispatcher-betriebszustaende-fw"},
		{"a/b/c/d", "bm-a-b-c-d"},
		{"Groß/Änderung", "bm-gross-aenderung"},
		{"spickzettel/spickzettel_POL", "bm-spickzettel-spickzettel-pol"},
		{"trailing/", "bm-trailing"},
	}
	for _, tc := range tests {
		if got := anchorID(tc.rel); got != tc.want {
			t.Errorf("anchorID(%q) = %q, want %q", tc.rel, got, tc.want)
		}
	}
}

func TestAssignAnchorsDeduplicates(t *testing.T) {
	// `a/b-c` and `a-b/c` slugify identically, so the second one must be
	// given a distinct identifier.
	root := &Node{Rel: "book", Dir: "book", Children: []*Node{
		{Rel: "a/b-c", Dir: "a/b-c"},
		{Rel: "a-b/c", Dir: "a-b/c"},
		{Rel: "a/b/c", Dir: "a/b/c"},
	}}
	assignAnchors(root)

	got := []string{root.Anchor}
	seen := map[string]bool{}
	for _, c := range root.Children {
		got = append(got, c.Anchor)
	}
	for _, id := range got {
		if seen[id] {
			t.Fatalf("duplicate anchor %q in %v", id, got)
		}
		seen[id] = true
	}
	if got[1] != "bm-a-b-c" || got[2] != "bm-a-b-c-2" || got[3] != "bm-a-b-c-3" {
		t.Errorf("anchors = %v", got)
	}
}
