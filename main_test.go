package main

import (
    "regexp"
    "testing"
)

func TestShouldExcludeChapter(t *testing.T) {
    defaultPattern := regexp.MustCompile(defaultExcludePattern)

    tests := []struct {
        name    string
        pattern *regexp.Regexp
        relPath string
        want    bool
    }{
        {"plain file included", defaultPattern, "a/foo.qmd", false},
        {"underscore file excluded", defaultPattern, "a/_foo.qmd", true},
        {"dot file excluded", defaultPattern, "a/.bar.qmd", true},
        {"underscore in middle included", defaultPattern, "a/foo_bar.qmd", false},
        {"nested underscore file excluded", defaultPattern, "a/b/_hidden.qmd", true},
        {"nested dot file excluded", defaultPattern, "a/b/.hidden.qmd", true},
        {"index file included", defaultPattern, "a/index.qmd", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := shouldExcludeChapter(tt.relPath, tt.pattern)
            if got != tt.want {
                t.Errorf("shouldExcludeChapter(%q) = %v, want %v", tt.relPath, got, tt.want)
            }
        })
    }
}

func TestShouldExcludeChapterCustomPattern(t *testing.T) {
    // Custom pattern: exclude any file with "draft" in its basename
    pattern := regexp.MustCompile(`draft`)

    tests := []struct {
        relPath string
        want    bool
    }{
        {"a/draft.qmd", true},
        {"a/my-draft-notes.qmd", true},
        {"a/final.qmd", false},
        {"a/index.qmd", false},
    }

    for _, tt := range tests {
        t.Run(tt.relPath, func(t *testing.T) {
            got := shouldExcludeChapter(tt.relPath, pattern)
            if got != tt.want {
                t.Errorf("shouldExcludeChapter(%q) = %v, want %v", tt.relPath, got, tt.want)
            }
        })
    }
}

func TestShouldExcludeChapterNilPattern(t *testing.T) {
    // A nil pattern means no filtering — nothing is excluded.
    if shouldExcludeChapter("a/_foo.qmd", nil) {
        t.Error("nil pattern should not exclude anything")
    }
}