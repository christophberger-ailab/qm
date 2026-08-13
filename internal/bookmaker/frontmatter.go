package bookmaker

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v4"
)

// FrontMatter holds the subset of Quarto front-matter fields that the
// bookmaker cares about. Everything else (listing, toc-depth, sidebar, ...)
// is page-level website metadata that is meaningless once the pages are
// flattened into a single document, and is therefore discarded.
type FrontMatter struct {
	// Title is the `title:` field. Empty when absent; in that case the page
	// heading is taken from the first ATX heading in the body.
	Title string
	// Order is the `order:` field, used for sorting siblings.
	Order int
	// HasOrder reports whether `order:` was present. Pages without an order
	// sort after all ordered siblings.
	HasOrder bool
}

// noOrder is the sort key used for pages without an `order:` field. Quarto
// sorts such pages last, alphabetically among themselves.
const noOrder = 1 << 30

// SortKey returns the value used to order this page among its siblings.
func (fm FrontMatter) SortKey() int {
	if !fm.HasOrder {
		return noOrder
	}
	return fm.Order
}

// splitFrontMatter separates a leading YAML front-matter block from the body.
//
// A front-matter block must start on the very first line with `---` and end
// with a line of `---` or `...`. Returns the raw YAML (without the fences),
// the remaining body, and whether a block was found.
func splitFrontMatter(src string) (yamlText, body string, found bool) {
	// Tolerate a UTF-8 BOM and leading blank lines before the opening fence.
	src = strings.TrimPrefix(src, "\uFEFF")

	lines := strings.Split(src, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || strings.TrimRight(lines[start], " \t\r") != "---" {
		return "", src, false
	}

	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimRight(lines[i], " \t\r")
		if trimmed == "---" || trimmed == "..." {
			yamlText = strings.Join(lines[start+1:i], "\n")
			body = strings.Join(lines[i+1:], "\n")
			return yamlText, body, true
		}
	}

	// Unterminated block: treat the whole file as body rather than guessing.
	return "", src, false
}

// parseFrontMatter extracts the fields the bookmaker needs from a page's
// front matter. Files without front matter yield a zero FrontMatter.
func parseFrontMatter(src string) (FrontMatter, string, error) {
	yamlText, body, found := splitFrontMatter(src)
	if !found {
		return FrontMatter{}, body, nil
	}

	var raw struct {
		Title *string `yaml:"title"`
		Order *int    `yaml:"order"`
	}
	if err := yaml.Unmarshal([]byte(yamlText), &raw); err != nil {
		return FrontMatter{}, body, fmt.Errorf("parsing front matter: %w", err)
	}

	fm := FrontMatter{}
	if raw.Title != nil {
		fm.Title = strings.TrimSpace(*raw.Title)
	}
	if raw.Order != nil {
		fm.Order = *raw.Order
		fm.HasOrder = true
	}
	return fm, body, nil
}
