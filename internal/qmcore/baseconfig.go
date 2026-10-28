package qmcore

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Base is the project's own `_quarto.yml`, as far as qm reads it: what every
// selection inherits before its profiles are merged in.
//
// qm reads the three profiles itself (see Profile). The base matters to the
// Pandoc renderer because it is where the project declares the document
// language, the audience classes of the content filter (`make: audiences:`),
// and the filters themselves.
type Base struct {
	// Path is the file the base was read from, or "" for a project without
	// one.
	Path    string
	Lang    string         `yaml:"lang"`
	Make    map[string]any `yaml:"make"`
	Filters []any          `yaml:"filters"`
}

// LoadBase reads the project's `_quarto.yml` (or `.yaml`). A project without
// one has nothing to inherit, which is not an error.
func LoadBase(docRoot string) (*Base, error) {
	for _, name := range []string{"_quarto.yml", "_quarto.yaml"} {
		path := filepath.Join(docRoot, name)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", path, err)
		}
		b := &Base{Path: path}
		if err := yaml.Unmarshal(data, b); err != nil {
			return nil, fmt.Errorf("cannot parse %s: %w", path, err)
		}
		return b, nil
	}
	return &Base{}, nil
}

// MakeFlags merges the `make:` sections of the three profiles and the base
// the way Quarto merges configuration: the first-listed profile that defines
// a key wins, and the base has the lowest priority. The audience profile
// supplies `pol`/`fw`, the format profile `explanation`/`slide`/..., and the
// base the list of audience classes; the profiles do not overlap, so the
// order only matters for a conflict.
func (p *Profiles) MakeFlags(base *Base) map[string]any {
	out := map[string]any{}
	add := func(m map[string]any) {
		for k, v := range m {
			if _, taken := out[k]; !taken {
				out[k] = v
			}
		}
	}
	for _, prof := range p.All() {
		if prof != nil {
			add(prof.Make)
		}
	}
	if base != nil {
		add(base.Make)
	}
	return out
}

// Language is the document language: the first profile that sets one wins,
// then the base. Empty when nothing sets it.
func (p *Profiles) Language(base *Base) string {
	for _, prof := range p.All() {
		if prof != nil && prof.Lang != "" {
			return prof.Lang
		}
	}
	if base != nil {
		return base.Lang
	}
	return ""
}

// FilterEntries lists the `filters:` entries that apply to the selection, in
// the order Quarto runs them: Quarto concatenates array keys, base first and
// then the profiles in --profile order. Entries are left as written (a
// string, or a map with `path:` and `at:`); see bookrender for how they are
// read.
func (p *Profiles) FilterEntries(base *Base) []any {
	var out []any
	if base != nil {
		out = append(out, base.Filters...)
	}
	for _, prof := range p.All() {
		if prof != nil {
			out = append(out, prof.Filters...)
		}
	}
	return out
}
