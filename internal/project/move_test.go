package project

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func load(t *testing.T, root string) *Tree {
	t.Helper()
	tree, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func mustMove(t *testing.T, root, src, parent string, pos int) *Tree {
	t.Helper()
	if err := load(t, root).Move(src, parent, pos); err != nil {
		t.Fatal(err)
	}
	return load(t, root)
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func order(t *testing.T, p *Page) int {
	t.Helper()
	if p.Order == nil {
		t.Fatalf("page %s is unordered", p.Path)
	}
	return *p.Order
}

// Reordering within a group renumbers the whole group sequentially,
// including previously unordered pages.
func TestMoveReorderWithinGroup(t *testing.T) {
	root := writeFixture(t)
	tree := mustMove(t, root, "chapter2/third.qmd", "chapter2/index.qmd", 0)

	ch2 := tree.Find("chapter2/index.qmd")
	want := []string{"chapter2/third.qmd", "chapter2/second.qmd", "chapter2/fourth.qmd"}
	if got := paths(ch2.Children); !reflect.DeepEqual(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
	for i, c := range ch2.Children {
		if order(t, c) != i+1 {
			t.Errorf("%s order = %d, want %d", c.Path, *c.Order, i+1)
		}
	}
}

// Moving a page up to the root shifts its headings up and renumbers both
// groups; unordered pages in the source group stay unordered.
func TestMoveToRoot(t *testing.T) {
	root := writeFixture(t)
	tree := mustMove(t, root, "chapter2/second.qmd", "", 1)

	want := []string{"index.qmd", "second.qmd", "chapter1/index.qmd", "chapter2/index.qmd", "chapter3.qmd"}
	if got := paths(tree.Pages); !reflect.DeepEqual(got, want) {
		t.Fatalf("root = %v, want %v", got, want)
	}
	for i, p := range tree.Pages {
		if order(t, p) != i+1 {
			t.Errorf("%s order = %d, want %d", p.Path, *p.Order, i+1)
		}
	}

	content := readFile(t, root, "second.qmd")
	if !strings.Contains(content, "\n# Detail") {
		t.Errorf("headings not shifted up:\n%s", content)
	}

	ch2 := tree.Find("chapter2/index.qmd")
	if got := paths(ch2.Children); !reflect.DeepEqual(got, []string{"chapter2/third.qmd", "chapter2/fourth.qmd"}) {
		t.Fatalf("chapter2 children = %v", got)
	}
	if order(t, ch2.Children[0]) != 1 {
		t.Errorf("third.qmd order = %d, want 1 (gap closed)", *ch2.Children[0].Order)
	}
	if ch2.Children[1].Order != nil {
		t.Errorf("fourth.qmd should stay unordered")
	}
}

// Dropping a page into a leaf turns the leaf into a section.
func TestMoveIntoLeaf(t *testing.T) {
	root := writeFixture(t)
	tree := mustMove(t, root, "chapter3/another.qmd", "chapter2/second.qmd", 0)

	second := tree.Find("chapter2/second.qmd")
	if got := paths(second.Children); !reflect.DeepEqual(got, []string{"chapter2/second/another.qmd"}) {
		t.Fatalf("second children = %v", got)
	}
	if !strings.Contains(readFile(t, root, "chapter2/second/another.qmd"), "## Another") {
		t.Error("headings not shifted down")
	}
}

// Moving an index.qmd-style section moves its whole directory.
func TestMoveSectionIndexStyle(t *testing.T) {
	root := writeFixture(t)
	tree := mustMove(t, root, "chapter2/index.qmd", "chapter3.qmd", 0)

	want := []string{"index.qmd", "chapter1/index.qmd", "chapter3.qmd"}
	if got := paths(tree.Pages); !reflect.DeepEqual(got, want) {
		t.Fatalf("root = %v, want %v", got, want)
	}
	ch3 := tree.Find("chapter3.qmd")
	wantCh3 := []string{"chapter3/chapter2/index.qmd", "chapter3/another.qmd", "chapter3/yet-another.qmd", "chapter3/deep/index.qmd"}
	if got := paths(ch3.Children); !reflect.DeepEqual(got, wantCh3) {
		t.Fatalf("chapter3 children = %v, want %v", got, wantCh3)
	}
	if !strings.Contains(readFile(t, root, "chapter3/chapter2/index.qmd"), "## Two") {
		t.Error("section headings not shifted")
	}
	if !strings.Contains(readFile(t, root, "chapter3/chapter2/second.qmd"), "### Detail") {
		t.Error("descendant headings not shifted")
	}
}

// Moving a name.qmd-style section moves both the file and its directory.
func TestMoveSectionNameStyle(t *testing.T) {
	root := writeFixture(t)
	tree := mustMove(t, root, "chapter3.qmd", "chapter1/index.qmd", 0)

	ch1 := tree.Find("chapter1/index.qmd")
	if got := paths(ch1.Children); !reflect.DeepEqual(got, []string{"chapter1/chapter3.qmd"}) {
		t.Fatalf("chapter1 children = %v", got)
	}
	moved := tree.Find("chapter1/chapter3.qmd")
	wantKids := []string{"chapter1/chapter3/another.qmd", "chapter1/chapter3/yet-another.qmd", "chapter1/chapter3/deep/index.qmd"}
	if got := paths(moved.Children); !reflect.DeepEqual(got, wantKids) {
		t.Fatalf("moved children = %v, want %v", got, wantKids)
	}
}

func TestMoveIntoOwnSubtree(t *testing.T) {
	root := writeFixture(t)
	if err := load(t, root).Move("chapter3.qmd", "chapter3/deep/index.qmd", 0); err == nil {
		t.Error("want error moving a section into its own subtree")
	}
}

func TestMoveIntoRootIndex(t *testing.T) {
	root := writeFixture(t)
	if err := load(t, root).Move("chapter2/second.qmd", "index.qmd", 0); err == nil {
		t.Error("want error moving under the root index page")
	}
}

// CreatePage creates the new page as name/index.qmd, ordered after the
// last ordered sibling.
func TestCreatePage(t *testing.T) {
	root := writeFixture(t)
	created, err := load(t, root).CreatePage("chapter2/index.qmd", "fifth", "Fifth")
	if err != nil {
		t.Fatal(err)
	}
	if created != "chapter2/fifth/index.qmd" {
		t.Fatalf("created = %q", created)
	}
	tree := load(t, root)
	p := tree.Find("chapter2/fifth/index.qmd")
	if p == nil {
		t.Fatal("created page not in tree")
	}
	if p.Title != "Fifth" {
		t.Errorf("title = %q", p.Title)
	}
	if order(t, p) != 3 { // after third (order 2), before unordered fourth
		t.Errorf("order = %d, want 3", *p.Order)
	}
}

func TestCreatePageAtRoot(t *testing.T) {
	root := writeFixture(t)
	created, err := load(t, root).CreatePage("", "about", "About")
	if err != nil {
		t.Fatal(err)
	}
	if created != "about/index.qmd" {
		t.Fatalf("created = %q", created)
	}
	if p := load(t, root).Find("about/index.qmd"); p == nil || order(t, p) != 5 {
		t.Fatalf("about/index.qmd = %+v", p)
	}
}

// CreatePageAfter inserts the new page right after a sibling, renumbering
// the whole group so previously unordered pages become ordered.
func TestCreatePageAfter(t *testing.T) {
	root := writeFixture(t)
	created, err := load(t, root).CreatePageAfter("chapter2/second.qmd", "inserted", "Inserted")
	if err != nil {
		t.Fatal(err)
	}
	if created != "chapter2/inserted/index.qmd" {
		t.Fatalf("created = %q", created)
	}
	tree := load(t, root)
	ch2 := tree.Find("chapter2/index.qmd")
	want := []string{
		"chapter2/second.qmd", "chapter2/inserted/index.qmd",
		"chapter2/third.qmd", "chapter2/fourth.qmd",
	}
	if got := paths(ch2.Children); !reflect.DeepEqual(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
	for i, c := range ch2.Children {
		if order(t, c) != i+1 {
			t.Errorf("%s order = %d, want %d", c.Path, *c.Order, i+1)
		}
	}
}

// CreatePageAfter a root page inserts into the root group.
func TestCreatePageAfterAtRoot(t *testing.T) {
	root := writeFixture(t)
	created, err := load(t, root).CreatePageAfter("index.qmd", "intro", "Intro")
	if err != nil {
		t.Fatal(err)
	}
	if created != "intro/index.qmd" {
		t.Fatalf("created = %q", created)
	}
	tree := load(t, root)
	want := []string{
		"index.qmd", "intro/index.qmd", "chapter1/index.qmd",
		"chapter2/index.qmd", "chapter3.qmd",
	}
	if got := paths(tree.Pages); !reflect.DeepEqual(got, want) {
		t.Fatalf("root pages = %v, want %v", got, want)
	}
}

func TestCreatePageExists(t *testing.T) {
	root := writeFixture(t)
	// chapter1/ already exists as a directory, so it is taken.
	if _, err := load(t, root).CreatePage("", "chapter1", "X"); err == nil {
		t.Error("want error creating page whose folder exists")
	}
}

// noSystemTrash keeps test files out of the real system trash by forcing
// the in-project _trash fallback.
func noSystemTrash(t *testing.T) {
	t.Helper()
	saved := TrashCommands
	TrashCommands = nil
	t.Cleanup(func() { TrashCommands = saved })
}

func TestDeletePage(t *testing.T) {
	noSystemTrash(t)
	root := writeFixture(t)
	if err := load(t, root).DeletePage("chapter2/third.qmd"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "chapter2", "third.qmd")); !os.IsNotExist(err) {
		t.Error("third.qmd still exists")
	}
}

func TestDeleteSection(t *testing.T) {
	noSystemTrash(t)
	root := writeFixture(t)
	if err := load(t, root).DeletePage("chapter3.qmd"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "chapter3.qmd")); !os.IsNotExist(err) {
		t.Error("chapter3.qmd still exists")
	}
	if _, err := os.Stat(filepath.Join(root, "chapter3")); !os.IsNotExist(err) {
		t.Error("chapter3/ still exists")
	}
	if tree := load(t, root); tree.Find("chapter3/another.qmd") != nil {
		t.Error("children still in tree")
	}
}
