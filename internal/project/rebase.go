package project

import "bytes"

// Rebasing an edit onto the tree's own writes
//
// The web editor holds a page's text from the moment it opened it, and
// autosaves that whole text back. The tree, meanwhile, writes to the same
// files behind the editor's back: every move and every create renumbers a
// sibling group's `order:`, and a move that changes a page's depth shifts
// its Markdown headings. An autosave posted after one of those has an
// `order:` — and possibly heading levels — from before it, and writing it
// out as it stands puts the move back the way it was.
//
// The two writes the tree makes are the only ones to reckon with, and both
// are mechanical: SetOrder on the frontmatter, ShiftHeadings on the body.
// So an edit is not merged line by line but replayed: the transform that
// took the text the editor started from to the text now on disk is worked
// out, checked against the file, and then applied to what the user typed.
// The user's words are theirs, the structure is the tree's, and neither
// has to be guessed at.
//
// Working the transform out is deliberately done by reconstruction rather
// than by reading the difference: a candidate transform is applied to the
// base and the result compared to the file, byte for byte. A transform
// that does not reproduce the file exactly is not accepted, so anything
// else that touched the page — an external editor, another tool — is a
// conflict rather than something to be papered over. That makes the
// failure mode "refuse and say so", never "write the wrong thing".

// maxShift bounds the heading shifts tried. Headings run from 1 to 6, so
// no larger shift can tell two files apart.
const maxShift = 5

// Rebased is an edit replayed onto the tree's writes: the text to save,
// and what the replay had to put back, which the caller reports to the
// user — the editor still shows the text from before, and only a reload
// brings it in line.
type Rebased struct {
	// Body is the text to write.
	Body []byte
	// HeadingDelta is the shift the tree applied to the page's headings,
	// 0 when it applied none.
	HeadingDelta int
	// OrderChanged reports whether the tree renumbered the page.
	OrderChanged bool
}

// Rebase replays the tree's own writes onto an edit. base is the text the
// editor started from, disk the file as it now stands, and edited what the
// user is saving.
//
// It reports false when the difference between base and disk is not the
// tree's doing — someone else wrote the file — in which case there is
// nothing safe to write and the caller must not.
func Rebase(edited, base, disk []byte) (Rebased, bool) {
	// The common case: nothing moved underneath, so the edit stands as it
	// is. This is also the answer whenever the editor is already current.
	if bytes.Equal(base, disk) {
		return Rebased{Body: edited}, true
	}

	delta, order, ok := transformTo(base, disk)
	if !ok {
		return Rebased{}, false
	}

	out := ShiftHeadings(edited, delta)
	// The order the tree wrote is put back only where the user left the
	// field alone. Someone who typed a new order into the frontmatter
	// meant it, and an edit the user made by hand is never discarded; the
	// next move renumbers the group anyway.
	changed := false
	if order != nil && sameOrder(ParseFrontmatter(edited).Order, ParseFrontmatter(base).Order) {
		out = SetOrder(out, *order)
		changed = true
	}
	return Rebased{Body: out, HeadingDelta: delta, OrderChanged: changed}, true
}

// transformTo finds the heading shift and, if one was written, the order
// that take base to disk exactly. The order is nil when the frontmatter
// did not have to be touched to reproduce disk.
func transformTo(base, disk []byte) (delta int, order *int, ok bool) {
	// Shifts are tried smallest first, so that the shift actually applied
	// is found before a larger one that happens to clamp to the same text.
	for n := 0; n <= maxShift; n++ {
		for _, d := range shiftsOfSize(n) {
			shifted := ShiftHeadings(base, d)
			if bytes.Equal(shifted, disk) {
				return d, nil, true
			}
			// setOrder writes the field only when it differs, so the
			// order is tried as a second step rather than always.
			if o := ParseFrontmatter(disk).Order; o != nil && bytes.Equal(SetOrder(shifted, *o), disk) {
				return d, o, true
			}
		}
	}
	return 0, nil, false
}

// shiftsOfSize lists the shifts of a given magnitude: none for 0, both
// directions otherwise.
func shiftsOfSize(n int) []int {
	if n == 0 {
		return []int{0}
	}
	return []int{n, -n}
}

// sameOrder compares two order fields, either of which may be absent.
func sameOrder(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
