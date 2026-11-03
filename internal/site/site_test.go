package site

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("missing %s: %v", rel, err)
	}
	return string(b)
}

func has(t *testing.T, name, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("%s: missing %q", name, sub)
		}
	}
}

func hasNot(t *testing.T, name, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			t.Errorf("%s: unexpected %q", name, sub)
		}
	}
}

func project(t *testing.T) (root, out string) {
	root, out = t.TempDir(), filepath.Join(t.TempDir(), "out")
	write(t, root, "dispatcher/index.qmd", "---\ntitle: Dispatcher\norder: 1\n---\n\n# Dispatcher\n\nIntro.\n")
	write(t, root, "dispatcher/a/index.qmd", "---\ntitle: Page A\norder: 1\n---\n\n# Page A\n\nText.\n\n## First\n\n### Deep\n\n#### Too deep\n\n## Second\n\n::: pol\nPOLICE ONLY\n:::\n")
	write(t, root, "dispatcher/b.qmd", "---\ntitle: Page B\norder: 2\n---\n\n# Page B\n\n"+
		"[abs dir](/dispatcher/a/) [abs](/dispatcher/a) [abs idx](/dispatcher/a/index.qmd) [abs html](/dispatcher/a/index.html)\n"+
		"[rel](a/index.qmd#second) [dot](./a/) [other topic](/x/page.qmd) [up](../x/page.qmd)\n"+
		"[ext](https://example.org/a.qmd) [mail](mailto:a@b.c) [frag](#here) [missing](nope.qmd)\n\n"+
		"![Pic](/assets/pic.png)\n\n![Rel](../assets/pic.png)\n\n[doc](/assets/doc.pdf)\n")
	write(t, root, "dispatcher/group/sub/index.qmd", "---\ntitle: Sub\n---\n\n# Sub\n\n## Heading Two\n\n### Heading Three\n")
	write(t, root, "x/index.qmd", "---\ntitle: Xtopic\n---\n\n# Xtopic\n")
	write(t, root, "x/page.qmd", "---\ntitle: X Page\n---\n\nNo headings here.\n")
	write(t, root, "assets/pic.png", "PNG")
	write(t, root, "assets/doc.pdf", "PDF")
	return
}

func opts(root, out string) Options {
	return Options{
		Root: root, OutDir: out, Title: "Test Site", Lang: "de",
		Topics: []Topic{{Name: "d", Folder: "dispatcher"}, {Name: "x", Folder: "x", Title: "Custom X"}},
		Keep:   func(classes []string) bool { return !contains(classes, "pol") },
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestBuild(t *testing.T) {
	root, out := project(t)
	var logs []string
	o := opts(root, out)
	o.Log = func(f string, a ...any) {
		logs = append(logs, strings.TrimSpace(strings.ReplaceAll(f, "%", "")+" "+anyStr(a)))
	}
	if err := Build(o); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.html", "dispatcher/index.html", "dispatcher/a/index.html", "dispatcher/b.html",
		"dispatcher/group/sub/index.html", "x/index.html", "x/page.html", "assets/pic.png", "assets/doc.pdf",
		"_site/style.css", "_site/site.js"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(f))); err != nil {
			t.Errorf("missing output %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "dispatcher/group/index.html")); err == nil {
		t.Error("directory without index page got a page")
	}
	if read(t, out, "assets/pic.png") != "PNG" {
		t.Error("image not copied")
	}

	// links
	b := read(t, out, "dispatcher/b.html")
	has(t, "b", b,
		`<title>Page B – Test Site</title>`,
		`<html lang="de">`,
		`href="a/index.html"`,
		`href="a/index.html#second"`,
		`href="../x/page.html"`,
		`href="https://example.org/a.qmd"`, `href="mailto:a@b.c"`, `href="#here"`, `href="nope.qmd"`,
		`src="../assets/pic.png"`, `href="../assets/doc.pdf"`,
		`href="../_site/style.css"`)
	art := regexp.MustCompile(`(?s)<article.*?</article>`).FindString(b)
	if n := strings.Count(art, `href="a/index.html"`); n != 5 {
		t.Errorf("want 5 links to a/index.html in the article, got %d", n)
	}
	hasNot(t, "article", art, `.qmd#`, `/dispatcher/`, `"/x/page`)
	if !strings.Contains(strings.Join(logs, "\n"), "nope.qmd") {
		t.Errorf("no warning for missing link: %v", logs)
	}

	// conditional content, headings
	a := read(t, out, "dispatcher/a/index.html")
	hasNot(t, "a", a, "POLICE ONLY")
	has(t, "a", a, `<h2 `, `id="first"`)

	// navbar
	nav := regexp.MustCompile(`(?s)<nav id="topics".*?</nav>`).FindString(b)
	has(t, "nav", nav, `href="index.html" class="active" aria-current="true">Dispatcher`, `href="../x/index.html">Custom X`)
	has(t, "brand", b, `class="site-title" href="../index.html">Test Site`)

	// sidebar
	side := regexp.MustCompile(`(?s)<aside id="sidebar".*?</aside>`).FindString(b)
	has(t, "sidebar", side, `<a href="b.html" aria-current="page">Page B</a>`, `<a href="a/index.html">Page A</a>`,
		`<span class="group">Group</span>`, `href="group/sub/index.html"`)
	if strings.Count(side, "aria-current") != 1 {
		t.Errorf("exactly one current page expected: %s", side)
	}
	hasNot(t, "sidebar", side, "Custom X")
	// branch containing the current page is open
	sub := read(t, out, "dispatcher/group/sub/index.html")
	has(t, "sub", sub, `<details open><summary><span class="group">Group</span>`)
	side = regexp.MustCompile(`(?s)<aside id="sidebar".*?</aside>`).FindString(b)
	hasNot(t, "closed", side, `<details open><summary><span class="group">`)

	// TOC: headings 1 and 2 below the title
	toc := regexp.MustCompile(`(?s)<aside class="toc".*?</aside>`).FindString(a)
	has(t, "toc", toc, `Auf dieser Seite`, `href="#first"`, `href="#deep"`, `href="#second"`)
	hasNot(t, "toc", toc, "too-deep")
	if strings.Contains(b, `class="toc"`) {
		t.Error("page without headings should have no TOC")
	}
	xp := read(t, out, "x/page.html")
	hasNot(t, "xp", xp, `<aside class="toc"`)

	// demoted levels: subfolder page starts at h2, its subheadings h3 (+toc)
	has(t, "sub", sub, `<h2 `, `<h3 `, `<h4 `)
	hasNot(t, "sub", sub, `<h1 `)
	subToc := regexp.MustCompile(`(?s)<aside class="toc".*?</aside>`).FindString(sub)
	has(t, "subtoc", subToc, `href="#heading-two"`, `href="#heading-three"`)
	has(t, "a", a, `<h1 `)

	// prev/next
	has(t, "pager", b, `rel="prev" href="a/index.html"`)
	hasNot(t, "pager", b, `rel="next" href="a`)
	has(t, "pager", a, `rel="prev" href="../index.html"`, `rel="next" href="../b.html"`)

	// landing page lists topics
	home := read(t, out, "index.html")
	has(t, "home", home, `href="dispatcher/index.html"`, `href="x/index.html"`, `Custom X`, `href="_site/style.css"`)
}

func anyStr(a []any) string {
	var s []string
	for _, x := range a {
		if v, ok := x.(string); ok {
			s = append(s, v)
		}
	}
	return strings.Join(s, " ")
}

func TestHomePageAndExpand(t *testing.T) {
	root, out := project(t)
	write(t, root, "index.qmd", "---\ntitle: Start\n---\n\n# Welcome\n\n[Go](/dispatcher/b.qmd) {{< hello >}}\n")
	o := opts(root, out)
	o.Expand = func(s string) (string, error) { return strings.ReplaceAll(s, "{{< hello >}}", "HELLO"), nil }
	if err := Build(o); err != nil {
		t.Fatal(err)
	}
	home := read(t, out, "index.html")
	has(t, "home", home, `href="dispatcher/b.html"`, "HELLO", `<title>Start – Test Site</title>`)
	hasNot(t, "home", home, `<aside id="sidebar"`)
}

func TestNoEscapeFromRoot(t *testing.T) {
	root, out := project(t)
	secret := filepath.Join(filepath.Dir(root), "secret.txt")
	_ = os.WriteFile(secret, []byte("s"), 0o644)
	write(t, root, "x/page.qmd", "---\ntitle: P\n---\n\n![a](../../secret.txt) [b](/../secret.txt)\n")
	if err := Build(opts(root, out)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "secret.txt")); err == nil {
		t.Error("file outside root copied")
	}
	hasNot(t, "x", read(t, out, "x/page.html"), `src="../secret.txt"`)
}

func TestExampleProject(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "project")
	root, _ = filepath.Abs(root)
	out := filepath.Join(t.TempDir(), "o")
	err := Build(Options{Root: root, OutDir: out, Lang: "de", Topics: []Topic{{Name: "book", Folder: "book"}},
		Keep: func([]string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"book/index.html", "book/erster/alpha/tief/index.html", "book/dritter_FW/seite.html", "assets/images/bord.png"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	has(t, "alpha", read(t, out, "book/erster/alpha/index.html"), `src="../../../assets/images/bord.png"`)
	has(t, "book", read(t, out, "book/index.html"), `href="erster/alpha/index.html"`)
}
