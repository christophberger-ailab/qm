package main

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestReadOrder_WithOrder(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "f.qmd", "---\norder: 5\ntitle: Foo\n---\n\nbody")
	order, err := readOrder(path)
	if err != nil {
		t.Fatalf("readOrder: %v", err)
	}
	if order == nil || *order != 5 {
		t.Errorf("expected order 5, got %v", order)
	}
}

func TestReadOrder_NoOrder(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "f.qmd", "---\ntitle: Foo\n---\n\nbody")
	order, err := readOrder(path)
	if err != nil {
		t.Fatalf("readOrder: %v", err)
	}
	if order != nil {
		t.Errorf("expected nil order, got %v", *order)
	}
}

func TestReadOrder_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "f.qmd", "just some body text\n")
	order, err := readOrder(path)
	if err != nil {
		t.Fatalf("readOrder: %v", err)
	}
	if order != nil {
		t.Errorf("expected nil order, got %v", *order)
	}
}

func TestReadOrder_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "f.qmd", "")
	order, err := readOrder(path)
	if err != nil {
		t.Fatalf("readOrder: %v", err)
	}
	if order != nil {
		t.Errorf("expected nil order, got %v", *order)
	}
}

func TestReadOrder_DotDotDotTerminator(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "f.qmd", "---\norder: 3\n...\nbody")
	order, err := readOrder(path)
	if err != nil {
		t.Fatalf("readOrder: %v", err)
	}
	if order == nil || *order != 3 {
		t.Errorf("expected order 3, got %v", order)
	}
}
