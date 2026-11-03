package docrender

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christophberger-ailab/qm/internal/markdown"
)

func TestPDFMarkdown(t *testing.T) {
	src := "# Titel {#t}\n\nText *kursiv* **fett** `code` [Link](#t).\n\n" +
		"- eins\n- zwei\n\n1. a\n2. b\n\n> Zitat\n\n```go\nx := 1\n```\n\n" +
		"| A | B |\n|---|---|\n| 1 | 2 |\n\n::: {.callout-note}\nHinweis\n:::\n\n" +
		"::: pagebreak\n:::\n\nFußnote[^1].\n\n[^1]: Die Note.\n\nTerm\n: Definition\n"
	d := markdown.Parse([]byte(src))
	d.Prepare(markdown.Options{Lang: "de"})
	bb, err := FromMarkdown(d)
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	breaks, tables, callouts := 0, 0, 0
	for _, b := range bb {
		all = append(all, text(b.Runs))
		if b.Break {
			breaks++
		}
		if b.Rows != nil {
			tables++
		}
		if b.Callout != nil {
			callouts++
			if text(b.Callout.Title[0].Runs) != "Hinweis" {
				t.Errorf("callout title %q", text(b.Callout.Title[0].Runs))
			}
		}
	}
	joined := strings.Join(all, "|")
	for _, want := range []string{"Titel", "• eins", "2. b", "Zitat", "x := 1", "Fußnote[1].", "[1] Die Note.", "Term", "Definition"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %s", want, joined)
		}
	}
	if breaks != 1 || tables != 1 || callouts != 1 {
		t.Errorf("breaks %d tables %d callouts %d", breaks, tables, callouts)
	}
	out := filepath.Join(t.TempDir(), "x.pdf")
	if err := PDFMarkdown(context.Background(), out, d, Options{TOC: true, Title: "T", Lang: "de"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() < 1000 {
		t.Fatalf("pdf not written: %v", err)
	}
}
