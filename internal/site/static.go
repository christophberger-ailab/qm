package site

import (
	"os"
	"path/filepath"
)

// writeStatic writes the stylesheet and script to OutDir/_site.
func (b *builder) writeStatic() error {
	dir := filepath.Join(b.outAbs, "_site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"style.css", "site.js"} {
		data, err := assetFS.ReadFile("assets/" + name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
