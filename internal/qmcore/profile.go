package qmcore

import (
	"os"
	"path/filepath"
	"strings"
)

// StripYamlExt strips a .yaml or .yml extension.
func StripYamlExt(name string) string {
	if strings.HasSuffix(name, ".yaml") {
		return strings.TrimSuffix(name, ".yaml")
	}
	return strings.TrimSuffix(name, ".yml")
}

// ParseProfileName derives the base folder and optional variant ("fw", "pol",
// or "") from a profile name like "_quarto-calltaker-fw".
func ParseProfileName(name string) (baseFolder, variant string) {
	const prefix = "_quarto-"
	if !strings.HasPrefix(name, prefix) {
		return name, ""
	}
	name = strings.TrimPrefix(name, prefix)
	if strings.HasSuffix(name, "-fw") {
		return strings.TrimSuffix(name, "-fw"), "fw"
	}
	if strings.HasSuffix(name, "-pol") {
		return strings.TrimSuffix(name, "-pol"), "pol"
	}
	return name, ""
}

// ResolveProfilePath finds the profile yaml in docRoot. It accepts either a
// bare name ("foo" or "_quarto-foo"), or a full file name with extension.
// When no extension is given, .yaml is preferred, falling back to .yml.
func ResolveProfilePath(docRoot, arg string) string {
	if strings.HasSuffix(arg, ".yaml") || strings.HasSuffix(arg, ".yml") {
		return filepath.Join(docRoot, arg)
	}
	for _, ext := range []string{".yaml", ".yml"} {
		p := filepath.Join(docRoot, arg+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(docRoot, arg+".yaml")
}

// NormalizeProfileArg ensures the _quarto- prefix is present (BOOKS.2).
func NormalizeProfileArg(arg string) string {
	if strings.HasPrefix(arg, "_quarto-") {
		return arg
	}
	return "_quarto-" + arg
}

// DiscoverProfiles returns the list of profile yaml files (relative to
// docRoot) that exist in docRoot and match the `_quarto-*.yml(.yaml)`
// pattern. Used when the user does not pass a specific profile name.
func DiscoverProfiles(docRoot string) ([]string, error) {
	entries, err := os.ReadDir(docRoot)
	if err != nil {
		return nil, err
	}
	var profiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "_quarto-") {
			continue
		}
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		profiles = append(profiles, name)
	}
	return profiles, nil
}
