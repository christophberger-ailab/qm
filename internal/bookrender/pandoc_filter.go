package bookrender

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/christophberger-ailab/qm/internal/qmcore"
)

//go:embed pandoc_filter.lua
var pandocFilterBody string

// filterConfig is what the generated filter script is told: the part of the
// Lua filter that varies from render to render.
type filterConfig struct {
	// Kind is the Pandoc output format: "docx" or "pptx".
	Kind string
	// Filters are the project's own Lua filters, as absolute paths.
	Filters []string
	// Icons is the directory holding the callout icons (<type>.png), or "".
	Icons string
	// Titles are the default callout titles by type, in the document language.
	Titles map[string]string
	// Capture is where the final document is written for the PDF layout; ""
	// for a format that has no PDF.
	Capture string
}

// Script returns the filter script: the configuration as a Lua table,
// followed by the filter itself.
//
// The configuration is part of the script instead of the document metadata
// because the project's filters are loaded when the script is, and Pandoc
// does not give a filter its document's metadata at that point.
func (c filterConfig) Script() string {
	var b strings.Builder
	b.WriteString("local QM = {\n")
	fmt.Fprintf(&b, "  kind = %s,\n", luaString(c.Kind))
	b.WriteString("  filters = {")
	for i, f := range c.Filters {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(luaString(f))
	}
	b.WriteString("},\n")
	if c.Icons != "" {
		fmt.Fprintf(&b, "  icons = %s,\n", luaString(c.Icons))
	}
	b.WriteString("  titles = {")
	keys := make([]string, 0, len(c.Titles))
	for k := range c.Titles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s = %s", k, luaString(c.Titles[k]))
	}
	b.WriteString("},\n")
	if c.Capture != "" {
		fmt.Fprintf(&b, "  capture = %s,\n", luaString(c.Capture))
	}
	b.WriteString("}\n\n")
	b.WriteString(pandocFilterBody)
	return b.String()
}

// luaString quotes s as a Lua string literal.
func luaString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\', c == '"':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, `\%03d`, c)
		default:
			b.WriteByte(c) // UTF-8 passes through byte by byte
		}
	}
	b.WriteByte('"')
	return b.String()
}

// projectFilters turns the `filters:` entries that apply to a selection into
// the absolute paths of Lua filters Pandoc can run.
//
// An entry is a project-relative `.lua` file, or the name of a Quarto
// extension (`cboct/targeted-content`), whose `_extension.yml` lists its
// filters. The capture filter of the Quarto-hook renderer is skipped: the
// Pandoc renderer captures the document itself. Quarto's entry points
// (`at:`) have no counterpart here, so an entry that uses one is an error,
// not something to drop silently.
func projectFilters(root string, entries []any) ([]string, error) {
	var out []string
	for _, e := range entries {
		var name string
		switch v := e.(type) {
		case string:
			name = v
		case map[string]any:
			path, _ := v["path"].(string)
			if filepath.ToSlash(filepath.Clean(path)) == CaptureFilterPath {
				continue
			}
			if at, _ := v["at"].(string); at != "" {
				return nil, fmt.Errorf("filter %q uses the entry point %q, which the Pandoc renderer does not support", path, at)
			}
			name = path
		default:
			return nil, fmt.Errorf("unsupported filters: entry %v", e)
		}
		switch {
		case name == "" || name == "quarto":
			continue // Quarto's own filters
		case strings.HasSuffix(name, ".lua"):
			abs, err := qmcore.ProjectPath(root, name)
			if err != nil {
				return nil, fmt.Errorf("filter %q: %w", name, err)
			}
			if _, err := os.Stat(abs); err != nil {
				return nil, fmt.Errorf("filter %q: %w", name, err)
			}
			out = append(out, abs)
		default:
			paths, err := extensionFilters(root, name)
			if err != nil {
				return nil, err
			}
			out = append(out, paths...)
		}
	}
	return out, nil
}

// extensionFilters reads the filters a Quarto extension contributes.
func extensionFilters(root, name string) ([]string, error) {
	dir, err := qmcore.ProjectPath(root, filepath.Join("_extensions", filepath.FromSlash(name)))
	if err != nil {
		return nil, fmt.Errorf("filter %q: %w", name, err)
	}
	var data []byte
	for _, f := range []string{"_extension.yml", "_extension.yaml"} {
		if data, err = os.ReadFile(filepath.Join(dir, f)); err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("filter %q is neither a .lua file nor an extension in _extensions/%s", name, name)
	}
	var ext struct {
		Contributes struct {
			Filters []any `yaml:"filters"`
		} `yaml:"contributes"`
	}
	if err := yaml.Unmarshal(data, &ext); err != nil {
		return nil, fmt.Errorf("_extensions/%s: %w", name, err)
	}
	var out []string
	for _, f := range ext.Contributes.Filters {
		var rel string
		switch v := f.(type) {
		case string:
			rel = v
		case map[string]any:
			rel, _ = v["path"].(string)
		}
		if !strings.HasSuffix(rel, ".lua") {
			return nil, fmt.Errorf("_extensions/%s contributes %v; the Pandoc renderer runs Lua filters only", name, f)
		}
		out = append(out, filepath.Join(dir, filepath.FromSlash(rel)))
	}
	return out, nil
}
