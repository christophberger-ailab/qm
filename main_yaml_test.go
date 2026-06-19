package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUpdateProfileYaml_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "_quarto-foo.yaml")

	chapters := []string{"index.qmd", "a/foo.qmd", "a/bar.qmd"}
	if err := updateProfileYaml(path, chapters); err != nil {
		t.Fatalf("updateProfileYaml: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "book:") {
		t.Errorf("expected book: in output, got:\n%s", content)
	}
	if !strings.Contains(content, "chapters:") {
		t.Errorf("expected chapters: in output, got:\n%s", content)
	}
	for _, ch := range chapters {
		if !strings.Contains(content, ch) {
			t.Errorf("expected chapter %s in output, got:\n%s", ch, content)
		}
	}
}

func TestUpdateProfileYaml_ReplacesExistingChapters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "_quarto-foo.yaml")

	original := `project:
  type: book
book:
  title: Test Book
  chapters:
    - old1.qmd
    - old2.qmd
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write initial: %v", err)
	}

	chapters := []string{"index.qmd", "new1.qmd"}
	if err := updateProfileYaml(path, chapters); err != nil {
		t.Fatalf("updateProfileYaml: %v", err)
	}

	data, _ := os.ReadFile(path)
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}

	book, ok := parsed["book"].(map[string]any)
	if !ok {
		t.Fatalf("book is not a mapping: %T", parsed["book"])
	}
	if book["title"] != "Test Book" {
		t.Errorf("expected title preserved, got %v", book["title"])
	}
	chaptersList, ok := book["chapters"].([]any)
	if !ok {
		t.Fatalf("chapters is not a list: %T", book["chapters"])
	}
	if len(chaptersList) != 2 {
		t.Errorf("expected 2 chapters, got %d", len(chaptersList))
	}
	if chaptersList[0] != "index.qmd" {
		t.Errorf("expected index.qmd first, got %v", chaptersList[0])
	}
	if chaptersList[1] != "new1.qmd" {
		t.Errorf("expected new1.qmd second, got %v", chaptersList[1])
	}

	// Original old chapters must be gone.
	content := string(data)
	if strings.Contains(content, "old1.qmd") || strings.Contains(content, "old2.qmd") {
		t.Errorf("old chapters still present:\n%s", content)
	}
}

func TestUpdateProfileYaml_AddsBookSectionWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "_quarto-foo.yaml")

	original := `project:
  type: book
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write initial: %v", err)
	}

	chapters := []string{"index.qmd", "a.qmd"}
	if err := updateProfileYaml(path, chapters); err != nil {
		t.Fatalf("updateProfileYaml: %v", err)
	}

	data, _ := os.ReadFile(path)
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := parsed["project"]; !ok {
		t.Error("expected project preserved")
	}
	book, ok := parsed["book"].(map[string]any)
	if !ok {
		t.Fatalf("expected book mapping to be created, got %T", parsed["book"])
	}
	if _, ok := book["chapters"]; !ok {
		t.Error("expected chapters under book")
	}
}

func TestFindMappingValue(t *testing.T) {
	yamlText := `foo: bar
nested:
  key: value
`
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlText), &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mapping := root.Content[0]

	if v := findMappingValue(mapping, "foo"); v == nil || v.Value != "bar" {
		t.Errorf("expected foo=bar, got %v", v)
	}
	if v := findMappingValue(mapping, "missing"); v != nil {
		t.Errorf("expected nil for missing key, got %v", v)
	}
	if v := findMappingValue(nil, "foo"); v != nil {
		t.Errorf("expected nil for nil node, got %v", v)
	}
}

func TestReplaceMappingValue(t *testing.T) {
	yamlText := `foo: bar
baz: qux
`
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlText), &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mapping := root.Content[0]

	newVal := &yaml.Node{Kind: yaml.ScalarNode, Value: "REPLACED"}
	if !replaceMappingValue(mapping, "foo", newVal) {
		t.Error("expected replace to return true")
	}
	if v := findMappingValue(mapping, "foo"); v == nil || v.Value != "REPLACED" {
		t.Errorf("expected REPLACED, got %v", v)
	}
	if replaceMappingValue(mapping, "missing", newVal) {
		t.Error("expected replace to return false for missing key")
	}
	if replaceMappingValue(nil, "foo", newVal) {
		t.Error("expected replace to return false for nil node")
	}
}
