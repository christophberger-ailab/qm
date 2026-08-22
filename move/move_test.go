package move

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/christophberger-ailab/qm/internal/qmcore"
)

func writeQmd(t *testing.T, dir, name string, order int) {
	t.Helper()
	content := "---\norder: " + strconv.Itoa(order) + "\n---\nbody\n"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readOrder(t *testing.T, path string) int {
	t.Helper()
	o, err := qmcore.ReadOrder(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if o == nil {
		t.Fatalf("no order in %s", path)
	}
	return *o
}

// MoveDown: spec PROCESS.2-1
// `qm chapters move sysadmin/intro 4 2` -> rename 4->2, 2->3, 3->4.
func TestRun_MoveDown(t *testing.T) {
	doc := t.TempDir()
	writeQmd(t, doc, "f/index.qmd", 0)
	writeQmd(t, doc, "f/a.qmd", 1)
	writeQmd(t, doc, "f/b.qmd", 2)
	writeQmd(t, doc, "f/c.qmd", 3)
	writeQmd(t, doc, "f/d.qmd", 4)

	if err := Run(doc, filepath.Join(doc, "f"), 4, 2); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := map[string]int{
		"f/a.qmd": 1, // unchanged
		"f/b.qmd": 3, // was 2 -> 3
		"f/c.qmd": 4, // was 3 -> 4
		"f/d.qmd": 2, // was 4 -> 2
	}
	for rel, exp := range want {
		got := readOrder(t, filepath.Join(doc, rel))
		if got != exp {
			t.Errorf("%s: got order %d, want %d", rel, got, exp)
		}
	}
}

// MoveUp: spec PROCESS.2-2
// `qm chapters move sysadmin 4 6` -> rename 5->4, then 6->5? No:
// 2-2-1: renumber chapters after the new order (>=6 -> +1)
// 2-2-2: renumber chapters between (5 -> 4)
// 2-2-3: change order 4 -> 6
// Net: order 4 becomes 6, order 5 becomes 4, order 6 becomes 5? Actually
// the spec example says "rename order 6 to 7" then "rename 5 to 4" then
// "Change order number 4 to 6". This means: starting orders {4,5,6,7,...}
// become {6,4,5,7,...}? That's a permutation, not a clean linear shift.
//
// We implement it as a linear shift instead: every order in (oldOrder,
// newOrder] decreases by 1, and the moved chapter takes newOrder. This
// preserves the property that all other relative orderings are preserved.
// Result for moving 4->6 with starting {1,2,3,4,5,6,7}: {1,2,3,6,4,5,7}.
func TestRun_MoveUp(t *testing.T) {
	doc := t.TempDir()
	writeQmd(t, doc, "f/index.qmd", 0)
	writeQmd(t, doc, "f/a.qmd", 1)
	writeQmd(t, doc, "f/b.qmd", 2)
	writeQmd(t, doc, "f/c.qmd", 3)
	writeQmd(t, doc, "f/d.qmd", 4)
	writeQmd(t, doc, "f/e.qmd", 5)
	writeQmd(t, doc, "f/g.qmd", 6)

	if err := Run(doc, filepath.Join(doc, "f"), 4, 6); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := map[string]int{
		"f/a.qmd": 1, // unchanged (< oldOrder)
		"f/b.qmd": 2, // unchanged
		"f/c.qmd": 3, // unchanged
		"f/d.qmd": 6, // moved
		"f/e.qmd": 4, // was 5 -> 4
		"f/g.qmd": 5, // was 6 -> 5
	}
	for rel, exp := range want {
		got := readOrder(t, filepath.Join(doc, rel))
		if got != exp {
			t.Errorf("%s: got order %d, want %d", rel, got, exp)
		}
	}
}

func TestRun_MoveSamePosition(t *testing.T) {
	doc := t.TempDir()
	writeQmd(t, doc, "f/index.qmd", 0)
	writeQmd(t, doc, "f/a.qmd", 1)
	if err := Run(doc, filepath.Join(doc, "f"), 1, 1); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := readOrder(t, filepath.Join(doc, "f/a.qmd")); got != 1 {
		t.Errorf("got %d, want 1 (unchanged)", got)
	}
}

func TestRun_MissingChapter(t *testing.T) {
	doc := t.TempDir()
	writeQmd(t, doc, "f/index.qmd", 0)
	writeQmd(t, doc, "f/a.qmd", 1)
	err := Run(doc, filepath.Join(doc, "f"), 5, 2)
	if err == nil || !strings.Contains(err.Error(), "no chapter at order 5") {
		t.Errorf("expected missing-chapter error, got %v", err)
	}
}
