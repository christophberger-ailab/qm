package site

import (
	"bytes"
	"embed"
	"fmt"
	"html"
	"html/template"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/christophberger-ailab/qm/internal/bookmaker"
	"github.com/christophberger-ailab/qm/internal/markdown"
	"github.com/yuin/goldmark/ast"
	"go.yaml.in/yaml/v4"
)

//go:embed assets/*
var assetFS embed.FS

// Topic is one top-level section of the site: a menu entry and a sidebar.
type Topic struct {
	Name string
	// Folder is the topic's content folder, relative to the project root.
	Folder string
	// Title is the menu text; "" uses the title of the folder's index page.
	Title string
}

// Options configure Build.
type Options struct {
	Root    string // project root, absolute
	OutDir  string // output directory, absolute
	Title   string // site title; "" = name of the root folder
	Lang    string // html lang attribute
	Topics  []Topic
	Keep    markdown.Keep
	Formats []string
	// Expand expands shortcodes in a page's Markdown; may be nil.
	Expand func(string) (string, error)
	// Log receives warnings and progress; may be nil.
	Log func(format string, args ...any)
}

// pageInfo is one page of the site.
type pageInfo struct {
	node     *bookmaker.Node
	topic    *topicInfo // nil for the home page
	out      string     // output file, slash-separated, relative to OutDir
	title    string
	audience string
	srcDir   string // project-relative directory links resolve against
	home     bool
	// landing, when set, is generated content for a page without source.
	landing template.HTML
}

type topicInfo struct {
	Topic
	title string
	root  *bookmaker.Node
	pages []*pageInfo // in sidebar order
	home  *pageInfo   // the topic's entry page
	byNod map[*bookmaker.Node]*pageInfo
}

type builder struct {
	o       Options
	root    string
	title   string
	topics  []*topicInfo
	pages   map[string]*pageInfo // by source key: node Rel, "" for home
	homePg  *pageInfo
	copied  map[string]bool
	tmpl    *template.Template
	labels  labels
	outAbs  string
	rootAbs string
}

func (b *builder) logf(format string, args ...any) {
	if b.o.Log != nil {
		b.o.Log(format, args...)
	}
}

// Build renders the website into o.OutDir.
func Build(o Options) error {
	if o.Root == "" || o.OutDir == "" {
		return fmt.Errorf("site: Root and OutDir are required")
	}
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return err
	}
	out, err := filepath.Abs(o.OutDir)
	if err != nil {
		return err
	}
	b := &builder{o: o, root: root, rootAbs: root, outAbs: out,
		pages: map[string]*pageInfo{}, copied: map[string]bool{}}
	b.title = o.Title
	if b.title == "" {
		b.title = filepath.Base(root)
	}
	if b.o.Lang == "" {
		b.o.Lang = "en"
	}
	b.labels = labelsFor(b.o.Lang)

	b.tmpl, err = template.ParseFS(assetFS, "assets/page.html")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := b.load(); err != nil {
		return err
	}
	for _, p := range b.allPages() {
		if err := b.renderPage(p); err != nil {
			return err
		}
	}
	return b.writeStatic()
}

func (b *builder) allPages() []*pageInfo {
	var all []*pageInfo
	if b.homePg != nil {
		all = append(all, b.homePg)
	}
	for _, t := range b.topics {
		all = append(all, t.pages...)
	}
	return all
}

// load reads the topic trees and the home page.
func (b *builder) load() error {
	if data, err := os.ReadFile(filepath.Join(b.root, "index.qmd")); err == nil {
		title, body := splitFront(string(data))
		n := &bookmaker.Node{Name: "index", Rel: "index", Level: 1,
			Page: &bookmaker.Page{File: filepath.Join(b.root, "index.qmd"), Rel: "index.qmd", Body: body}}
		n.Page.Title = title
		bookmaker.Pages(n, "")
		p := &pageInfo{node: n, out: "index.html", title: n.Title(), srcDir: ".", home: true}
		b.homePg = p
		b.pages[""] = p
	}
	for _, t := range b.o.Topics {
		dir := filepath.Join(b.root, filepath.FromSlash(t.Folder))
		tree, err := bookmaker.LoadTree(dir, b.root)
		if err != nil {
			return fmt.Errorf("site: topic %q: %w", t.Name, err)
		}
		bookmaker.Pages(tree, t.Title)
		ti := &topicInfo{Topic: t, root: tree, title: tree.Title(), byNod: map[*bookmaker.Node]*pageInfo{}}
		var walk func(n *bookmaker.Node, aud string)
		walk = func(n *bookmaker.Node, aud string) {
			if n.Audience != "" {
				aud = n.Audience
			}
			if n.Page != nil {
				p := &pageInfo{node: n, topic: ti, title: n.Title(), audience: aud, srcDir: path.Dir(n.Page.Rel)}
				if n.IsDir() {
					p.out = n.Rel + "/index.html"
				} else {
					p.out = n.Rel + ".html"
				}
				b.pages[n.Rel] = p
				ti.pages = append(ti.pages, p)
				ti.byNod[n] = p
			}
			for _, c := range n.Children {
				walk(c, aud)
			}
		}
		walk(tree, "")
		if len(ti.pages) == 0 {
			return fmt.Errorf("site: topic %q (%s) has no pages", t.Name, t.Folder)
		}
		ti.home = ti.pages[0]
		b.topics = append(b.topics, ti)
	}
	if b.homePg == nil {
		b.homePg = &pageInfo{out: "index.html", title: b.title, srcDir: ".", home: true,
			landing: b.landingHTML()}
	}
	return nil
}

func (b *builder) landingHTML() template.HTML {
	var s strings.Builder
	s.WriteString("<h1 class=\"page-title\">" + html.EscapeString(b.title) + "</h1>\n<ul class=\"topic-list\">\n")
	for _, t := range b.topics {
		s.WriteString(`<li><a href="` + html.EscapeString(relURL("index.html", t.home.out)) + `">` +
			html.EscapeString(t.title) + "</a></li>\n")
	}
	s.WriteString("</ul>\n")
	return template.HTML(s.String())
}

// splitFront separates the title from the body of a page without a tree.
func splitFront(src string) (title, body string) {
	src = strings.ReplaceAll(strings.TrimPrefix(src, "\xEF\xBB\xBF"), "\r\n", "\n")
	if !strings.HasPrefix(src, "---\n") {
		return "", src
	}
	rest := src[4:]
	for _, end := range []string{"\n---\n", "\n...\n"} {
		if i := strings.Index(rest, end); i >= 0 {
			var fm struct {
				Title string `yaml:"title"`
			}
			_ = yaml.Unmarshal([]byte(rest[:i]), &fm)
			return strings.TrimSpace(fm.Title), rest[i+len(end):]
		}
	}
	if strings.HasSuffix(rest, "\n---") {
		return "", ""
	}
	return "", src
}

type tocItem struct {
	Text, ID string
	Level    int // 1 or 2
}

func (b *builder) renderPage(p *pageInfo) error {
	var content template.HTML
	var toc []tocItem
	if p.landing != "" {
		content = p.landing
	} else {
		md, warns := bookmaker.PageMarkdown(p.node, p.audience,
			bookmaker.Options{ProjectRoot: b.root, WrapAudience: true})
		for _, w := range warns {
			b.logf("warning: %s", w)
		}
		if b.o.Expand != nil {
			var err error
			if md, err = b.o.Expand(md); err != nil {
				return fmt.Errorf("site: %s: %w", p.node.Page.Rel, err)
			}
		}
		doc := markdown.Parse([]byte(md))
		doc.Prepare(markdown.Options{Keep: b.o.Keep, Formats: b.o.Formats, Lang: b.o.Lang})
		b.rewriteLinks(doc, p)
		toc = tocOf(doc)
		s, err := markdown.HTML(doc)
		if err != nil {
			return fmt.Errorf("site: %s: %w", p.node.Page.Rel, err)
		}
		content = template.HTML(s)
	}
	data := b.pageData(p, content, toc)
	var buf bytes.Buffer
	if err := b.tmpl.ExecuteTemplate(&buf, "page.html", data); err != nil {
		return err
	}
	dst := filepath.Join(b.outAbs, filepath.FromSlash(p.out))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	b.logf("site: %s", p.out)
	return os.WriteFile(dst, buf.Bytes(), 0o644)
}

// tocOf lists the headings one and two levels below the page's title
// heading (the shallowest heading). The first heading of that level is
// marked as the page title.
func tocOf(doc *markdown.Document) []tocItem {
	var hs []*ast.Heading
	_ = ast.Walk(doc.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			hs = append(hs, h)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	if len(hs) == 0 {
		return nil
	}
	base := 7
	for _, h := range hs {
		base = min(base, h.Level)
	}
	if hs[0].Level == base {
		hs[0].SetAttributeString("class", "page-title")
	}
	var items []tocItem
	for _, h := range hs {
		d := h.Level - base
		if d < 1 || d > 2 {
			continue
		}
		items = append(items, tocItem{Text: markdown.PlainText(h, doc.Source), ID: markdown.HeadingID(h), Level: d})
	}
	if len(items) < 2 {
		return nil
	}
	return items
}
