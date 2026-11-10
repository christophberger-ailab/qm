package site

import (
	"os"
	"path/filepath"
)

// writeStatic writes the stylesheet and script to OutDir/_site, and copies
// the extra stylesheets.
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
	for i, src := range b.o.CSS {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		dst := filepath.Join(b.outAbs, filepath.FromSlash(b.css[i]))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
