package insert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cboct/qm/internal/qmcore"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRun_InsertsAndRenumbers(t *testing.T) {
	doc := t.TempDir()
	write(t, doc, "f/index.qmd", "---\norder: 0\n---\n")
	write(t, doc, "f/a.qmd", "---\norder: 1\n---\n")
	write(t, doc, "f/b.qmd", "---\norder: 2\n---\n")
	write(t, doc, "f/c.qmd", "---\norder: 3\n---\n")
	write(t, doc, "f/new.qmd", "---\ninsert-at: 2\ntitle: New\n---\nbody\n")

	if err := Run(doc, filepath.Join(doc, "f")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	check := func(rel string, want int) {
		t.Helper()
		o, err := qmcore.ReadOrder(filepath.Join(doc, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if o == nil || *o != want {
			t.Errorf("%s: want order %d, got %v", rel, want, o)
		}
	}
	check("f/a.qmd", 1)
	check("f/new.qmd", 2)
	check("f/b.qmd", 3) // was 2
	check("f/c.qmd", 4) // was 3

	// insert-at should be gone, replaced by order.
	data, _ := os.ReadFile(filepath.Join(doc, "f/new.qmd"))
	if strings.Contains(string(data), "insert-at") {
		t.Errorf("insert-at was not removed from new.qmd:\n%s", data)
	}
	if !strings.Contains(string(data), "body") {
		t.Errorf("body lost:\n%s", data)
	}
}

func TestRun_NoCandidate(t *testing.T) {
	doc := t.TempDir()
	write(t, doc, "f/index.qmd", "---\norder: 0\n---\n")
	write(t, doc, "f/a.qmd", "---\norder: 1\n---\n")
	err := Run(doc, filepath.Join(doc, "f"))
	if err == nil || !strings.Contains(err.Error(), "insert-at") {
		t.Errorf("expected insert-at error, got %v", err)
	}
}

func TestRun_MultipleCandidates(t *testing.T) {
	doc := t.TempDir()
	write(t, doc, "f/index.qmd", "---\norder: 0\n---\n")
	write(t, doc, "f/a.qmd", "---\norder: 1\n---\n")
	write(t, doc, "f/n1.qmd", "---\ninsert-at: 1\n---\n")
	write(t, doc, "f/n2.qmd", "---\ninsert-at: 2\n---\n")
	err := Run(doc, filepath.Join(doc, "f"))
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Errorf("expected multiple-candidates error, got %v", err)
	}
}

func TestRun_CandidateWithOrderIsError(t *testing.T) {
	doc := t.TempDir()
	write(t, doc, "f/index.qmd", "---\norder: 0\n---\n")
	write(t, doc, "f/new.qmd", "---\norder: 2\ninsert-at: 2\n---\n")
	err := Run(doc, filepath.Join(doc, "f"))
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("expected both-order-and-insert-at error, got %v", err)
	}
}
