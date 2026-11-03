package site

import (
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/christophberger-ailab/qm/internal/markdown"
	"github.com/yuin/goldmark/ast"
)

var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// relURL is the relative URL leading from the output file `from` to the
// output file `to` (both slash-separated, relative to the output root).
func relURL(from, to string) string {
	rel, err := filepath.Rel(filepath.FromSlash(path.Dir(from)), filepath.FromSlash(to))
	if err != nil {
		return to
	}
	return escapePath(filepath.ToSlash(rel))
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// rewriteLinks makes every link and image destination of a page relative
// to the page's output file, copying referenced project files.
func (b *builder) rewriteLinks(doc *markdown.Document, p *pageInfo) {
	_ = ast.Walk(doc.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch x := n.(type) {
		case *ast.Link:
			x.Destination = b.rewrite(x.Destination, p, false)
		case *ast.Image:
			x.Destination = b.rewrite(x.Destination, p, true)
		}
		return ast.WalkContinue, nil
	})
}

func (b *builder) rewrite(dest []byte, p *pageInfo, image bool) []byte {
	d := string(dest)
	if d == "" || strings.HasPrefix(d, "#") || strings.HasPrefix(d, "//") || schemeRe.MatchString(d) {
		return dest
	}
	suffix := ""
	if i := strings.IndexAny(d, "?#"); i >= 0 {
		d, suffix = d[:i], d[i:]
	}
	if u, err := url.PathUnescape(d); err == nil {
		d = u
	}
	var proj string
	if strings.HasPrefix(d, "/") {
		proj = path.Clean(d)
	} else {
		proj = path.Join("/", p.srcDir, d)
	}
	proj = strings.TrimPrefix(proj, "/")
	if proj == "." {
		proj = ""
	}
	if strings.HasPrefix(proj, "..") {
		b.logf("warning: %s: link %q leaves the project", p.out, string(dest))
		return dest
	}

	// A page?
	key := strings.TrimSuffix(proj, "/")
	isDirLink := strings.HasSuffix(d, "/") || d == "" || key == ""
	ext := strings.ToLower(path.Ext(key))
	if ext == ".qmd" || ext == ".html" || ext == ".md" || isDirLink || ext == "" {
		k := key
		if ext == ".qmd" || ext == ".html" || ext == ".md" {
			k = strings.TrimSuffix(k, path.Ext(k))
		}
		if path.Base(k) == "index" {
			k = strings.TrimSuffix(strings.TrimSuffix(k, "index"), "/")
		}
		if t, ok := b.pages[k]; ok {
			return []byte(relURL(p.out, t.out) + suffix)
		}
		if ext == ".qmd" || isDirLink {
			b.logf("warning: %s: link target %q not found", p.out, string(dest))
			return dest
		}
	}
	// A file of the project?
	if proj != "" && b.copyFile(proj) {
		return []byte(escapePath(relPathTo(path.Dir(p.out), proj)) + suffix)
	}
	b.logf("warning: %s: link target %q not found", p.out, string(dest))
	return dest
}

func relPathTo(fromDir, to string) string {
	rel, err := filepath.Rel(filepath.FromSlash(fromDir), filepath.FromSlash(to))
	if err != nil {
		return to
	}
	return filepath.ToSlash(rel)
}

// safeSource returns the absolute path of a regular file inside the
// project root, following no link out of it.
func (b *builder) safeSource(proj string) (string, bool) {
	src := filepath.Join(b.root, filepath.FromSlash(proj))
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return "", false
	}
	root, err := filepath.EvalSymlinks(b.root)
	if err != nil {
		return "", false
	}
	if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	return real, true
}

// copyFile copies a project file to the same path below the output.
func (b *builder) copyFile(proj string) bool {
	src, ok := b.safeSource(proj)
	if !ok {
		return false
	}
	if b.copied[proj] {
		return true
	}
	dst := filepath.Join(b.outAbs, filepath.FromSlash(proj))
	if filepath.Clean(dst) == filepath.Clean(src) {
		b.copied[proj] = true
		return true
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		b.logf("warning: %v", err)
		return false
	}
	in, err := os.Open(src)
	if err != nil {
		b.logf("warning: %v", err)
		return false
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		b.logf("warning: %v", err)
		return false
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		b.logf("warning: %v", err)
		return false
	}
	b.copied[proj] = true
	return out.Close() == nil
}
