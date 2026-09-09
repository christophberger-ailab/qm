package project

import (
	"strings"
	"testing"
)

// page builds a page file with the given order and heading level.
func page(order, level int, body string) string {
	return "---\ntitle: Zugriffsebene setzen\norder: " + itoa(order) + "\n---\n" +
		strings.Repeat("#", level) + " Zugriffsebene setzen\n" + body
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// The tree renumbering a page must survive the autosave that follows it:
// the editor still holds the old order, and writing that back would put
// the page where it was dragged from.
func TestRebaseKeepsTheTreesOrder(t *testing.T) {
	base := page(3, 1, "text\n")
	disk := page(1, 1, "text\n")
	edited := page(3, 1, "text the user just typed\n")

	got, ok := Rebase([]byte(edited), []byte(base), []byte(disk))
	if !ok {
		t.Fatal("want the renumbering recognized as the tree's own")
	}
	if want := page(1, 1, "text the user just typed\n"); string(got.Body) != want {
		t.Errorf("body =\n%q\nwant\n%q", got.Body, want)
	}
	if !got.OrderChanged {
		t.Error("OrderChanged = false, want true")
	}
}

// A move to another depth shifts the page's headings, and that shift has
// to be reapplied to whatever the user typed in the meantime.
func TestRebaseKeepsTheTreesHeadingShift(t *testing.T) {
	base := page(1, 1, "text\n")
	disk := page(1, 2, "text\n")
	edited := page(1, 1, "text\n\n# A heading the user added\n")

	got, ok := Rebase([]byte(edited), []byte(base), []byte(disk))
	if !ok {
		t.Fatal("want the heading shift recognized as the tree's own")
	}
	want := page(1, 2, "text\n\n## A heading the user added\n")
	if string(got.Body) != want {
		t.Errorf("body =\n%q\nwant\n%q", got.Body, want)
	}
	if got.HeadingDelta != 1 {
		t.Errorf("HeadingDelta = %d, want 1", got.HeadingDelta)
	}
}

// A move into another book does both at once.
func TestRebaseKeepsOrderAndShiftTogether(t *testing.T) {
	base := page(3, 2, "text\n")
	disk := page(1, 1, "text\n")
	edited := page(3, 2, "edited\n")

	got, ok := Rebase([]byte(edited), []byte(base), []byte(disk))
	if !ok {
		t.Fatal("want both recognized as the tree's own")
	}
	if want := page(1, 1, "edited\n"); string(got.Body) != want {
		t.Errorf("body =\n%q\nwant\n%q", got.Body, want)
	}
	if got.HeadingDelta != -1 || !got.OrderChanged {
		t.Errorf("delta = %d, orderChanged = %v; want -1, true", got.HeadingDelta, got.OrderChanged)
	}
}

// A file nothing touched leaves the edit exactly as the user wrote it.
func TestRebaseUntouchedFilePassesTheEditThrough(t *testing.T) {
	base := page(1, 1, "text\n")
	edited := page(1, 1, "edited\n")

	got, ok := Rebase([]byte(edited), []byte(base), []byte(base))
	if !ok {
		t.Fatal("want an untouched file accepted")
	}
	if string(got.Body) != edited {
		t.Errorf("body = %q, want the edit unchanged", got.Body)
	}
	if got.HeadingDelta != 0 || got.OrderChanged {
		t.Error("want nothing reported as replayed")
	}
}

// Anything the tree did not write is a conflict: someone else edited the
// page, and there is no safe way to write over them.
func TestRebaseRefusesAnOutsideEdit(t *testing.T) {
	base := page(1, 1, "text\n")
	disk := page(1, 1, "text somebody else wrote\n")
	edited := page(1, 1, "text the user typed\n")

	if _, ok := Rebase([]byte(edited), []byte(base), []byte(disk)); ok {
		t.Error("want an outside edit refused, not merged")
	}
}

// An outside edit alongside the tree's own write is still a conflict: the
// replay has to reproduce the file exactly or not be trusted at all.
func TestRebaseRefusesAnOutsideEditAlongsideARenumber(t *testing.T) {
	base := page(3, 1, "text\n")
	disk := page(1, 1, "text somebody else wrote\n")
	edited := page(3, 1, "text the user typed\n")

	if _, ok := Rebase([]byte(edited), []byte(base), []byte(disk)); ok {
		t.Error("want the outside edit refused despite the renumber")
	}
}

// An order the user typed themselves is their own, and is kept over the
// one the tree wrote — a hand-made edit is never silently dropped.
func TestRebaseKeepsAnOrderTheUserTyped(t *testing.T) {
	base := page(3, 1, "text\n")
	disk := page(1, 1, "text\n")
	edited := page(9, 1, "text\n")

	got, ok := Rebase([]byte(edited), []byte(base), []byte(disk))
	if !ok {
		t.Fatal("want the renumbering recognized")
	}
	if want := page(9, 1, "text\n"); string(got.Body) != want {
		t.Errorf("body =\n%q\nwant the user's own order kept\n%q", got.Body, want)
	}
	if got.OrderChanged {
		t.Error("OrderChanged = true, want false when the user set it")
	}
}

// Two moves before a single save compose into one replay.
func TestRebaseComposesTwoMoves(t *testing.T) {
	base := page(3, 1, "text\n")
	disk := page(7, 3, "text\n") // renumbered twice, shifted down twice
	edited := page(3, 1, "edited\n")

	got, ok := Rebase([]byte(edited), []byte(base), []byte(disk))
	if !ok {
		t.Fatal("want both moves recognized")
	}
	if want := page(7, 3, "edited\n"); string(got.Body) != want {
		t.Errorf("body =\n%q\nwant\n%q", got.Body, want)
	}
	if got.HeadingDelta != 2 {
		t.Errorf("HeadingDelta = %d, want 2", got.HeadingDelta)
	}
}

// Applying the replay repeatedly must not compound it: the base moves on
// with every save, so the same shift is never applied twice.
func TestRebaseIsStableAcrossRepeatedSaves(t *testing.T) {
	base := page(1, 1, "text\n")
	disk := page(1, 2, "text\n")

	first, ok := Rebase([]byte(page(1, 1, "one\n")), []byte(base), []byte(disk))
	if !ok {
		t.Fatal("first rebase refused")
	}
	// The editor still holds unshifted text, and the file now holds what
	// the first save wrote, so the next save rebases from there.
	second, ok := Rebase([]byte(page(1, 1, "two\n")), []byte(page(1, 1, "one\n")), first.Body)
	if !ok {
		t.Fatal("second rebase refused")
	}
	if want := page(1, 2, "two\n"); string(second.Body) != want {
		t.Errorf("body =\n%q\nwant\n%q", second.Body, want)
	}
}
