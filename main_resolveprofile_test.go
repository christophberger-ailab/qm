package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProfilePath_WithYamlExt(t *testing.T) {
	dir := t.TempDir()
	got := resolveProfilePath(dir, "_quarto-foo.yaml")
	want := filepath.Join(dir, "_quarto-foo.yaml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveProfilePath_WithYmlExt(t *testing.T) {
	dir := t.TempDir()
	got := resolveProfilePath(dir, "_quarto-foo.yml")
	want := filepath.Join(dir, "_quarto-foo.yml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveProfilePath_PrefersExistingYaml(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "_quarto-foo.yaml")
	if err := os.WriteFile(yamlPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := resolveProfilePath(dir, "_quarto-foo")
	if got != yamlPath {
		t.Errorf("got %q, want %q", got, yamlPath)
	}
}

func TestResolveProfilePath_PrefersExistingYml(t *testing.T) {
	dir := t.TempDir()
	ymlPath := filepath.Join(dir, "_quarto-foo.yml")
	if err := os.WriteFile(ymlPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := resolveProfilePath(dir, "_quarto-foo")
	if got != ymlPath {
		t.Errorf("got %q, want %q", got, ymlPath)
	}
}

func TestResolveProfilePath_DefaultsToYaml(t *testing.T) {
	dir := t.TempDir()
	got := resolveProfilePath(dir, "_quarto-foo")
	want := filepath.Join(dir, "_quarto-foo.yaml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
