package project

import (
	"bytes"
	"testing"
)

var sample = []byte(`---
title: "Second Chapter"
order: 5
author: me # keep this comment
---

# Heading

Body text.
`)

func TestParseFrontmatter(t *testing.T) {
	fm := ParseFrontmatter(sample)
	if fm.Title != "Second Chapter" {
		t.Errorf("Title = %q, want %q", fm.Title, "Second Chapter")
	}
	if fm.Order == nil || *fm.Order != 5 {
		t.Errorf("Order = %v, want 5", fm.Order)
	}
}

func TestParseFrontmatterNoOrder(t *testing.T) {
	fm := ParseFrontmatter([]byte("---\ntitle: X\n---\nbody\n"))
	if fm.Order != nil {
		t.Errorf("Order = %v, want nil", *fm.Order)
	}
	if fm.Title != "X" {
		t.Errorf("Title = %q, want X", fm.Title)
	}
}

func TestParseFrontmatterAbsent(t *testing.T) {
	fm := ParseFrontmatter([]byte("# Just a heading\n"))
	if fm.Title != "" || fm.Order != nil {
		t.Errorf("want zero FrontMatter, got %+v", fm)
	}
}

func TestSetOrderReplace(t *testing.T) {
	got := SetOrder(sample, 2)
	want := bytes.Replace(sample, []byte("order: 5"), []byte("order: 2"), 1)
	if !bytes.Equal(got, want) {
		t.Errorf("SetOrder replace:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetOrderInsert(t *testing.T) {
	src := []byte("---\ntitle: X\n---\nbody\n")
	got := SetOrder(src, 3)
	want := []byte("---\ntitle: X\norder: 3\n---\nbody\n")
	if !bytes.Equal(got, want) {
		t.Errorf("SetOrder insert:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetOrderCreatesFrontmatter(t *testing.T) {
	got := SetOrder([]byte("body only\n"), 1)
	want := []byte("---\norder: 1\n---\n\nbody only\n")
	if !bytes.Equal(got, want) {
		t.Errorf("SetOrder create:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Regression test: SetOrder must not modify its input, or callers cannot
// detect changes by comparing input and output.
func TestSetOrderDoesNotMutateInput(t *testing.T) {
	src := []byte("---\norder: 1\n---\nbody\n")
	orig := string(src)
	got := SetOrder(src, 2)
	if string(src) != orig {
		t.Errorf("input mutated: %q", src)
	}
	if want := "---\norder: 2\n---\nbody\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestShiftHeadings(t *testing.T) {
	src := []byte(`---
title: T
# yaml comment, not a heading
---

# One

## Two

` + "```" + `
# code comment, not a heading
` + "```" + `

#hashtag not a heading
###### Six
`)
	got := ShiftHeadings(src, 1)
	want := []byte(`---
title: T
# yaml comment, not a heading
---

## One

### Two

` + "```" + `
# code comment, not a heading
` + "```" + `

#hashtag not a heading
###### Six
`)
	if !bytes.Equal(got, want) {
		t.Errorf("ShiftHeadings +1:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestShiftHeadingsDown(t *testing.T) {
	src := []byte("# One\n## Two\n")
	got := ShiftHeadings(src, -1)
	want := []byte("# One\n# Two\n")
	if !bytes.Equal(got, want) {
		t.Errorf("ShiftHeadings -1:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestShiftHeadingsZero(t *testing.T) {
	if got := ShiftHeadings(sample, 0); !bytes.Equal(got, sample) {
		t.Errorf("ShiftHeadings 0 changed content")
	}
}
