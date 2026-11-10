package site

import (
	"html/template"
	"strings"

	"github.com/christophberger-ailab/qm/internal/bookmaker"
)

type labels struct {
	Menu, Contents, OnThisPage, Prev, Next, Skip, Topics, Sidebar string
}

func labelsFor(lang string) labels {
	if strings.HasPrefix(strings.ToLower(lang), "de") {
		return labels{"Menü", "Inhalt", "Auf dieser Seite", "Zurück", "Weiter", "Zum Inhalt springen", "Themen", "Seitenleiste"}
	}
	return labels{"Menu", "Contents", "On this page", "Previous", "Next", "Skip to content", "Topics", "Sidebar"}
}

type navLink struct {
	Title, URL string
	Active     bool
}

type navItem struct {
	Title, URL string
	Current    bool
	Open       bool
	Children   []*navItem
}

type sideData struct {
	Title, URL string
	Current    bool
	Items      []*navItem
}

type pageData struct {
	Lang, Title, SiteTitle, Base string
	HomeURL                      string
	CSS                          []string
	Nav                          []navLink
	Sidebar                      *sideData
	TOC                          []tocItem
	Content                      template.HTML
	Prev, Next                   *navLink
	L                            labels
	Home                         bool
}

func (b *builder) pageData(p *pageInfo, content template.HTML, toc []tocItem) *pageData {
	depth := strings.Count(p.out, "/")
	d := &pageData{
		Lang: b.o.Lang, SiteTitle: b.title, Title: p.title + " – " + b.title,
		Base: strings.Repeat("../", depth), Content: content, TOC: toc, L: b.labels,
		HomeURL: relURL(p.out, "index.html"), Home: p.home, CSS: b.css,
	}
	if p.home {
		d.Title = b.title
		if p.title != "" && p.title != b.title {
			d.Title = p.title + " – " + b.title
		}
	}
	for _, t := range b.topics {
		d.Nav = append(d.Nav, navLink{Title: t.title, URL: relURL(p.out, t.home.out), Active: t == p.topic})
	}
	if t := p.topic; t != nil {
		sd := &sideData{Title: t.title, Current: false}
		if t.root.Page != nil {
			sd.URL = relURL(p.out, t.ti(t.root).out)
			sd.Current = t.ti(t.root) == p
		}
		for _, c := range t.root.Children {
			if it := b.navItem(t, c, p); it != nil {
				sd.Items = append(sd.Items, it)
			}
		}
		d.Sidebar = sd
		for i, q := range t.pages {
			if q != p {
				continue
			}
			if i > 0 {
				d.Prev = &navLink{Title: t.pages[i-1].title, URL: relURL(p.out, t.pages[i-1].out)}
			}
			if i+1 < len(t.pages) {
				d.Next = &navLink{Title: t.pages[i+1].title, URL: relURL(p.out, t.pages[i+1].out)}
			}
		}
	}
	return d
}

func (t *topicInfo) ti(n *bookmaker.Node) *pageInfo { return t.byNod[n] }

// navItem builds the sidebar entry for a node; open is set on the branch
// holding the current page.
func (b *builder) navItem(t *topicInfo, n *bookmaker.Node, cur *pageInfo) *navItem {
	it := &navItem{Title: n.Title()}
	if pg := t.byNod[n]; pg != nil {
		it.URL = relURL(cur.out, pg.out)
		it.Current = pg == cur
		it.Open = it.Current
	}
	for _, c := range n.Children {
		if ci := b.navItem(t, c, cur); ci != nil {
			it.Children = append(it.Children, ci)
			if ci.Open {
				it.Open = true
			}
		}
	}
	if it.URL == "" && len(it.Children) == 0 {
		return nil
	}
	return it
}
