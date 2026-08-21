package qmcore

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func intPtr(i int) *int { return &i }

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func TestShouldExcludeChapter(t *testing.T) {
	defaultPattern := regexp.MustCompile(DefaultExcludePattern)

	tests := []struct {
		name    string
		pattern *regexp.Regexp
		relPath string
		want    bool
	}{
		{"plain file included", defaultPattern, "a/foo.qmd", false},
		{"underscore file excluded", defaultPattern, "a/_foo.qmd", true},
		{"dot file excluded", defaultPattern, "a/.bar.qmd", true},
		{"underscore in middle included", defaultPattern, "a/foo_bar.qmd", false},
		{"nested underscore file excluded", defaultPattern, "a/b/_hidden.qmd", true},
		{"nested dot file excluded", defaultPattern, "a/b/.hidden.qmd", true},
		{"index file included", defaultPattern, "a/index.qmd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldExcludeChapter(tt.relPath, tt.pattern); got != tt.want {
				t.Errorf("ShouldExcludeChapter(%q) = %v, want %v", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestShouldExcludeChapterCustomPattern(t *testing.T) {
	pattern := regexp.MustCompile(`draft`)
	tests := []struct {
		relPath string
		want    bool
	}{
		{"a/draft.qmd", true},
		{"a/my-draft-notes.qmd", true},
		{"a/final.qmd", false},
		{"a/index.qmd", false},
	}
	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			if got := ShouldExcludeChapter(tt.relPath, pattern); got != tt.want {
				t.Errorf("ShouldExcludeChapter(%q) = %v, want %v", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestShouldExcludeChapterNilPattern(t *testing.T) {
	if ShouldExcludeChapter("a/_foo.qmd", nil) {
		t.Error("nil pattern should not exclude anything")
	}
}

func TestStripYamlExt(t *testing.T) {
	tests := []struct{ name, want string }{
		{"_quarto-foo.yaml", "_quarto-foo"},
		{"_quarto-foo.yml", "_quarto-foo"},
		{"_quarto-foo", "_quarto-foo"},
		{"file.txt", "file.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripYamlExt(tt.name); got != tt.want {
				t.Errorf("StripYamlExt(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestPathFolderVariant(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{"a/foo.qmd", ""},
		{"a/b/foo.qmd", ""},
		{"a_FW/foo.qmd", "fw"},
		{"a_POL/foo.qmd", "pol"},
		{"a/b_FW/foo.qmd", "fw"},
		{"a/b_POL/foo.qmd", "pol"},
		{"a_FW/b/foo.qmd", "fw"},
		{"a_POL/b_FW/foo.qmd", "fw"},
		{"a_FW/b_POL/foo.qmd", "pol"},
		{"foo.qmd", ""},
	}
	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			if got := PathFolderVariant(tt.relPath); got != tt.want {
				t.Errorf("PathFolderVariant(%q) = %q, want %q", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestResolveProfilePath(t *testing.T) {
	t.Run("with .yaml ext", func(t *testing.T) {
		dir := t.TempDir()
		got := ResolveProfilePath(dir, "_quarto-foo.yaml")
		want := filepath.Join(dir, "_quarto-foo.yaml")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("with .yml ext", func(t *testing.T) {
		dir := t.TempDir()
		got := ResolveProfilePath(dir, "_quarto-foo.yml")
		want := filepath.Join(dir, "_quarto-foo.yml")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("prefers existing yaml", func(t *testing.T) {
		dir := t.TempDir()
		yamlPath := filepath.Join(dir, "_quarto-foo.yaml")
		if err := os.WriteFile(yamlPath, []byte("{}"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		got := ResolveProfilePath(dir, "_quarto-foo")
		if got != yamlPath {
			t.Errorf("got %q, want %q", got, yamlPath)
		}
	})
	t.Run("prefers existing yml", func(t *testing.T) {
		dir := t.TempDir()
		ymlPath := filepath.Join(dir, "_quarto-foo.yml")
		if err := os.WriteFile(ymlPath, []byte("{}"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		got := ResolveProfilePath(dir, "_quarto-foo")
		if got != ymlPath {
			t.Errorf("got %q, want %q", got, ymlPath)
		}
	})
	t.Run("defaults to yaml", func(t *testing.T) {
		dir := t.TempDir()
		got := ResolveProfilePath(dir, "_quarto-foo")
		want := filepath.Join(dir, "_quarto-foo.yaml")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestReadOrder(t *testing.T) {
	dir := t.TempDir()

	t.Run("with order", func(t *testing.T) {
		path := writeTempFile(t, dir, "a/f.qmd", "---\norder: 5\ntitle: Foo\n---\n\nbody")
		order, err := ReadOrder(path)
		if err != nil {
			t.Fatalf("ReadOrder: %v", err)
		}
		if order == nil || *order != 5 {
			t.Errorf("expected order 5, got %v", order)
		}
	})
	t.Run("no order", func(t *testing.T) {
		path := writeTempFile(t, dir, "b/f.qmd", "---\ntitle: Foo\n---\n\nbody")
		order, err := ReadOrder(path)
		if err != nil {
			t.Fatalf("ReadOrder: %v", err)
		}
		if order != nil {
			t.Errorf("expected nil order, got %v", *order)
		}
	})
	t.Run("no frontmatter", func(t *testing.T) {
		path := writeTempFile(t, dir, "c/f.qmd", "just some body text\n")
		order, err := ReadOrder(path)
		if err != nil {
			t.Fatalf("ReadOrder: %v", err)
		}
		if order != nil {
			t.Errorf("expected nil order, got %v", *order)
		}
	})
	t.Run("dot-dot-dot terminator", func(t *testing.T) {
		path := writeTempFile(t, dir, "d/f.qmd", "---\norder: 3\n...\nbody")
		order, err := ReadOrder(path)
		if err != nil {
			t.Fatalf("ReadOrder: %v", err)
		}
		if order == nil || *order != 3 {
			t.Errorf("expected order 3, got %v", order)
		}
	})
}

func TestScanFiles_BasicStructure(t *testing.T) {
	docRoot := t.TempDir()
	baseFolder := filepath.Join(docRoot, "a")

	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/foo.qmd", "---\norder: 1\n---\n")
	writeTempFile(t, docRoot, "a/bar.qmd", "---\norder: 2\n---\n")
	writeTempFile(t, docRoot, "a/b/index.qmd", "---\norder: 3\n---\n")
	writeTempFile(t, docRoot, "a/b/baz.qmd", "---\norder: 1\n---\n")

	entries, err := ScanFiles(docRoot, baseFolder, "", nil)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

func TestScanFiles_ExcludesByPattern(t *testing.T) {
	docRoot := t.TempDir()
	baseFolder := filepath.Join(docRoot, "a")

	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/_hidden.qmd", "---\norder: 99\n---\n")
	writeTempFile(t, docRoot, "a/.dotfile.qmd", "---\norder: 99\n---\n")

	pattern := regexp.MustCompile(DefaultExcludePattern)
	entries, err := ScanFiles(docRoot, baseFolder, "", pattern)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
	if _, ok := entries["a/index.qmd"]; !ok {
		t.Error("expected a/index.qmd to be present")
	}
}

func TestScanFiles_VariantFiltering(t *testing.T) {
	docRoot := t.TempDir()
	baseFolder := filepath.Join(docRoot, "a")

	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\n---\n")
	writeTempFile(t, docRoot, "a/foo_FW.qmd", "---\norder: 1\n---\n")
	writeTempFile(t, docRoot, "a/foo_POL.qmd", "---\norder: 1\n---\n")

	entries, err := ScanFiles(docRoot, baseFolder, "fw", nil)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	if _, ok := entries["a/foo_FW.qmd"]; !ok {
		t.Error("expected a/foo_FW.qmd to be kept for fw")
	}
	if _, ok := entries["a/foo_POL.qmd"]; ok {
		t.Error("expected a/foo_POL.qmd to be excluded for fw")
	}
}

func TestApplyVariantFilter_VariantFilesMatchingFW(t *testing.T) {
	entries := map[string]*FileEntry{
		"a/foo_FW.qmd":  {RelPath: "a/foo_FW.qmd", Order: intPtr(1)},
		"a/foo_POL.qmd": {RelPath: "a/foo_POL.qmd", Order: intPtr(1)},
	}
	got := ApplyVariantFilter(entries, "fw")
	if len(got) != 1 {
		t.Errorf("expected 1 entry, got %d", len(got))
	}
	if _, ok := got["a/foo_FW.qmd"]; !ok {
		t.Error("expected a/foo_FW.qmd to be kept for fw profile")
	}
}

func TestApplyVariantFilter_PlainSupersededByVariant(t *testing.T) {
	entries := map[string]*FileEntry{
		"a/foo.qmd":    {RelPath: "a/foo.qmd", Order: intPtr(1)},
		"a/foo_FW.qmd": {RelPath: "a/foo_FW.qmd", Order: intPtr(1)},
	}
	got := ApplyVariantFilter(entries, "fw")
	if len(got) != 1 {
		t.Errorf("expected 1 entry, got %d", len(got))
	}
	if _, ok := got["a/foo_FW.qmd"]; !ok {
		t.Error("expected a/foo_FW.qmd to win over plain a/foo.qmd")
	}
	if _, ok := got["a/foo.qmd"]; ok {
		t.Error("expected plain a/foo.qmd to be excluded")
	}
}

func TestApplyVariantFilter_FolderSuffixExcludesNonMatching(t *testing.T) {
	entries := map[string]*FileEntry{
		"a/b_POL/foo.qmd": {RelPath: "a/b_POL/foo.qmd", Order: intPtr(1)},
		"a/b_FW/foo.qmd":  {RelPath: "a/b_FW/foo.qmd", Order: intPtr(1)},
		"a/c/foo.qmd":     {RelPath: "a/c/foo.qmd", Order: intPtr(1)},
	}
	got := ApplyVariantFilter(entries, "fw")
	if _, ok := got["a/b_POL/foo.qmd"]; ok {
		t.Error("expected _POL folder content to be excluded for fw profile")
	}
	if _, ok := got["a/b_FW/foo.qmd"]; !ok {
		t.Error("expected _FW folder content to be kept for fw profile")
	}
	if _, ok := got["a/c/foo.qmd"]; !ok {
		t.Error("expected plain folder content to be kept")
	}
}

func TestApplyVariantFilter_PlainFolderSupersededByVariantFolder(t *testing.T) {
	entries := map[string]*FileEntry{
		"a/b/foo.qmd":    {RelPath: "a/b/foo.qmd", Order: intPtr(1)},
		"a/b_FW/bar.qmd": {RelPath: "a/b_FW/bar.qmd", Order: intPtr(1)},
	}
	got := ApplyVariantFilter(entries, "fw")
	if _, ok := got["a/b/foo.qmd"]; ok {
		t.Error("expected plain a/b/ folder to be superseded by a/b_FW/")
	}
	if _, ok := got["a/b_FW/bar.qmd"]; !ok {
		t.Error("expected a/b_FW/bar.qmd to be kept")
	}
}

func TestApplyVariantFilter_NoProfileVariantConflictDrops(t *testing.T) {
	entries := map[string]*FileEntry{
		"a/foo.qmd":     {RelPath: "a/foo.qmd", Order: intPtr(1)},
		"a/foo_FW.qmd":  {RelPath: "a/foo_FW.qmd", Order: intPtr(1)},
		"a/foo_POL.qmd": {RelPath: "a/foo_POL.qmd", Order: intPtr(1)},
	}
	got := ApplyVariantFilter(entries, "")
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(got) != 0 {
		t.Errorf("expected 0 entries for empty variant with conflicting pair, got %v", keys)
	}
}

func TestFindReplaceMappingValue(t *testing.T) {
	yamlText := `foo: bar
nested:
  key: value
`
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlText), &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mapping := root.Content[0]

	if v := FindMappingValue(mapping, "foo"); v == nil || v.Value != "bar" {
		t.Errorf("expected foo=bar, got %v", v)
	}
	if v := FindMappingValue(mapping, "missing"); v != nil {
		t.Errorf("expected nil for missing key, got %v", v)
	}

	newVal := &yaml.Node{Kind: yaml.ScalarNode, Value: "REPLACED"}
	if !ReplaceMappingValue(mapping, "foo", newVal) {
		t.Error("expected replace to return true")
	}
	if v := FindMappingValue(mapping, "foo"); v == nil || v.Value != "REPLACED" {
		t.Errorf("expected REPLACED, got %v", v)
	}
}

func TestUpdateFrontmatter_RoundtripsBody(t *testing.T) {
	dir := t.TempDir()
	original := "---\norder: 3\ntitle: Hello\n---\n\nbody text here\nmore text\n"
	path := writeTempFile(t, dir, "x.qmd", original)

	err := UpdateFrontmatter(path, func(n *yaml.Node) error {
		return SetMappingScalar(n, "order", "7")
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "body text here\nmore text\n") {
		t.Errorf("body not preserved: %s", got)
	}
	order, err := ReadOrder(path)
	if err != nil {
		t.Fatalf("ReadOrder: %v", err)
	}
	if order == nil || *order != 7 {
		t.Errorf("expected order=7, got %v", order)
	}
}

func TestUpdateFrontmatter_NoFrontmatterCreatesOne(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "x.qmd", "just body\n")
	err := UpdateFrontmatter(path, func(n *yaml.Node) error {
		return SetMappingScalar(n, "order", "1")
	})
	if err != nil {
		t.Fatalf("UpdateFrontmatter: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "---\n") {
		t.Errorf("expected new frontmatter at start, got: %q", string(data))
	}
	if !strings.Contains(string(data), "just body") {
		t.Errorf("body lost: %q", string(data))
	}
}

func TestScanFolderChapters(t *testing.T) {
	docRoot := t.TempDir()
	writeTempFile(t, docRoot, "a/index.qmd", "---\norder: 0\ntitle: Home\n---\n")
	writeTempFile(t, docRoot, "a/foo.qmd", "---\norder: 2\ntitle: Foo\n---\n")
	writeTempFile(t, docRoot, "a/bar.qmd", "---\norder: 1\ntitle: Bar\n---\n")
	writeTempFile(t, docRoot, "a/sub/index.qmd", "---\norder: 3\ntitle: Sub\n---\n")
	writeTempFile(t, docRoot, "a/sub/inside.qmd", "---\norder: 1\n---\n")
	writeTempFile(t, docRoot, "a/_hidden.qmd", "---\norder: 99\n---\n")

	items, err := ScanFolderChapters(docRoot, filepath.Join(docRoot, "a"))
	if err != nil {
		t.Fatalf("ScanFolderChapters: %v", err)
	}
	// Expect: bar (order 1), foo (order 2), sub (order 3). index.qmd is
	// the folder's own index, not its own chapter at this level.
	// _hidden.qmd is excluded.
	if len(items) != 3 {
		t.Fatalf("expected 3 chapter items, got %d: %#v", len(items), items)
	}
	if items[0].RelPath != "a/bar.qmd" || *items[0].Order != 1 {
		t.Errorf("item 0 wrong: %#v", items[0])
	}
	if items[1].RelPath != "a/foo.qmd" || *items[1].Order != 2 {
		t.Errorf("item 1 wrong: %#v", items[1])
	}
	if !items[2].IsDir || items[2].RelPath != "a/sub/index.qmd" || *items[2].Order != 3 {
		t.Errorf("item 2 wrong: %#v", items[2])
	}
}
