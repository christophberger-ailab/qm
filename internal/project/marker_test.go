package project

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMarkerFixture builds a project exercising the _FW/_POL marker rules,
// including a folder-marked subtree, an own-suffix override, multi-level
// inheritance, and a synthetic (index-less) directory node.
func writeMarkerFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pages := map[string]string{
		"index.qmd":                "---\ntitle: Home\norder: 1\n---\n# Home\n",
		"station_FW/index.qmd":     "---\ntitle: Station\norder: 2\n---\n# Station\n",
		"station_FW/alpha.qmd":     "---\ntitle: Alpha\norder: 1\n---\n# Alpha\n",
		"station_FW/bravo_POL.qmd": "---\ntitle: Bravo\norder: 2\n---\n# Bravo\n",
		"report_POL.qmd":           "---\ntitle: Report\norder: 3\n---\n# Report\n",
		"ops_POL/index.qmd":        "---\ntitle: Ops\norder: 4\n---\n# Ops\n",
		"ops_POL/sub/index.qmd":    "---\ntitle: Sub\norder: 1\n---\n# Sub\n",
		"ops_POL/sub/leaf.qmd":     "---\ntitle: Leaf\norder: 1\n---\n# Leaf\n",
		"plain.qmd":                "---\ntitle: Plain\norder: 5\n---\n# Plain\n",
		"zone_FW/only.qmd":         "---\ntitle: Only\norder: 1\n---\n# Only\n",
	}
	for name, content := range pages {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestMarkers(t *testing.T) {
	tree, err := Load(writeMarkerFixture(t))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"index.qmd":                "",        // no suffix, no marked ancestor
		"plain.qmd":                "",        // no suffix
		"station_FW/index.qmd":     markerFW,  // folder's own suffix
		"station_FW/alpha.qmd":     markerFW,  // inherited from folder
		"station_FW/bravo_POL.qmd": markerPOL, // own suffix overrides folder
		"report_POL.qmd":           markerPOL, // own suffix
		"ops_POL/index.qmd":        markerPOL, // folder's own suffix
		"ops_POL/sub/index.qmd":    markerPOL, // inherited one level down
		"ops_POL/sub/leaf.qmd":     markerPOL, // inherited two levels down
		"zone_FW/only.qmd":         markerFW,  // inherited from synthetic dir node
	}
	for p, want := range cases {
		pg := tree.Find(p)
		if pg == nil {
			t.Fatalf("Find(%q) = nil", p)
		}
		if pg.Marker != want {
			t.Errorf("%s marker = %q, want %q", p, pg.Marker, want)
		}
	}

	// The synthetic directory node itself (Path == "", Dir == "zone_FW")
	// must carry the folder marker derived from its directory name.
	var zone *Page
	for _, p := range tree.Pages {
		if p.Path == "" && p.Dir == "zone_FW" {
			zone = p
		}
	}
	if zone == nil {
		t.Fatal("synthetic zone_FW node not found")
	}
	if zone.Marker != markerFW {
		t.Errorf("synthetic zone_FW marker = %q, want %q", zone.Marker, markerFW)
	}
}

// TestLoadHeadingFallback verifies the title falls back to the first body
// heading when frontmatter has no title, and to the filename only when there
// is no heading either.
func TestLoadHeadingFallback(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"heading.qmd":  "---\norder: 1\n---\n\n# From Heading\n\nbody\n",
		"fenced.qmd":   "---\norder: 2\n---\n\n```\n# In Code\n```\n\n# Outside\n",
		"nohead.qmd":   "---\norder: 3\n---\n\njust text\n",
		"codeonly.qmd": "---\norder: 4\n---\n\n```\n# hidden\n```\n",
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
	want := map[string]string{
		"heading.qmd":  "From Heading",
		"fenced.qmd":   "Outside",
		"nohead.qmd":   "nohead",   // filename fallback
		"codeonly.qmd": "codeonly", // heading only in code fence -> filename
	}
	for p, w := range want {
		pg := tree.Find(p)
		if pg == nil {
			t.Fatalf("Find(%q) = nil", p)
		}
		if pg.Title != w {
			t.Errorf("%s title = %q, want %q", p, pg.Title, w)
		}
	}
}
