package main

import (
	"testing"
)

func TestStripYamlExt(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"_quarto-foo.yaml", "_quarto-foo"},
		{"_quarto-foo.yml", "_quarto-foo"},
		{"_quarto-foo", "_quarto-foo"},
		{"file.txt", "file.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripYamlExt(tt.name); got != tt.want {
				t.Errorf("stripYamlExt(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestParseProfileName(t *testing.T) {
	tests := []struct {
		name        string
		wantFolder  string
		wantVariant string
	}{
		{"_quarto-calltaker", "calltaker", ""},
		{"_quarto-calltaker-fw", "calltaker", "fw"},
		{"_quarto-calltaker-pol", "calltaker", "pol"},
		{"_quarto-a-b-fw", "a-b", "fw"},
		{"_quarto-a-b-pol", "a-b", "pol"},
		{"_quarto-a-b", "a-b", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			folder, variant := parseProfileName(tt.name)
			if folder != tt.wantFolder || variant != tt.wantVariant {
				t.Errorf("parseProfileName(%q) = (%q, %q), want (%q, %q)",
					tt.name, folder, variant, tt.wantFolder, tt.wantVariant)
			}
		})
	}
}

func TestPathFolderVariant(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{"a/foo.qmd", ""},
		{"a/b/foo.qmd", ""},
		{"a_FW/foo.qmd", "fw"},
		{"a_POL/foo.qmd", "pol"},
		{"a/b_FW/foo.qmd", "fw"},
		{"a/b_POL/foo.qmd", "pol"},
		{"a_FW/b/foo.qmd", "fw"},
		{"a_POL/b_FW/foo.qmd", "fw"},  // deepest wins
		{"a_FW/b_POL/foo.qmd", "pol"}, // deepest wins
		{"foo.qmd", ""},               // single segment, no folder
	}
	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			if got := pathFolderVariant(tt.relPath); got != tt.want {
				t.Errorf("pathFolderVariant(%q) = %q, want %q", tt.relPath, got, tt.want)
			}
		})
	}
}
