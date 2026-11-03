// Package build renders a qm project: every selected topic × format ×
// audience combination, without Quarto, Pandoc or LaTeX.
//
// A document (DOCX, PDF) is one topic folder flattened into one Markdown
// text by internal/bookmaker — pages in folder order, headings demoted by
// folder depth, in-book links turned into anchors — with its shortcodes
// resolved, parsed by internal/markdown, stripped of the conditional
// content the selection does not show, and written by internal/docx or
// laid out by internal/docrender.
//
// A website is rendered page by page by internal/site, from the same tree
// and with the same demotion.
package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/christophberger-ailab/qm/internal/bookmaker"
	"github.com/christophberger-ailab/qm/internal/config"
	"github.com/christophberger-ailab/qm/internal/docrender"
	"github.com/christophberger-ailab/qm/internal/docx"
	"github.com/christophberger-ailab/qm/internal/markdown"
	"github.com/christophberger-ailab/qm/internal/shortcode"
	"github.com/christophberger-ailab/qm/internal/site"
)

// Logf receives the render's progress, one line per call, in the format of
// fmt.Printf. A nil Logf discards the output.
type Logf func(format string, args ...any)

func (l Logf) or() Logf {
	if l == nil {
		return func(string, ...any) {}
	}
	return l
}

// Options describe a render run.
type Options struct {
	Selections []config.Selection
	// Clean empties each selected format's output directory once, before
	// the first render.
	Clean bool
	// DryRun prints what would be written without writing anything.
	DryRun bool
}

// pdfTimeout bounds one PDF layout, which runs in a library that cannot be
// interrupted.
const pdfTimeout = 5 * time.Minute

// Run renders every selection. A failing selection does not stop the
// others; the error reports how many failed.
func Run(o Options, log Logf) error {
	log = log.or()
	if o.Clean && !o.DryRun {
		if err := clean(o.Selections, log); err != nil {
			return err
		}
	}
	failed := 0
	for i, sel := range o.Selections {
		log("[%d/%d] %s", i+1, len(o.Selections), sel)
		if err := Render(sel, o.DryRun, log); err != nil {
			log("error: %v", err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d renders failed", failed, len(o.Selections))
	}
	log("done: %d render(s)", len(o.Selections))
	return nil
}

// Render produces one selection's output.
func Render(sel config.Selection, dryRun bool, log Logf) error {
	log = log.or()
	dir, err := sel.OutputDir()
	if err != nil {
		return err
	}
	if sel.Format.IsWeb() {
		return renderSite(sel, dir, dryRun, log)
	}
	stem, err := sel.FileStem()
	if err != nil {
		return err
	}
	cover, err := sel.Cover()
	if err != nil {
		return err
	}
	if cover != "" {
		if err := docrender.ValidateCover(cover); err != nil {
			return fmt.Errorf("topic %s: cover: %w", sel.Topic.Name, err)
		}
	}
	template, err := sel.Template()
	if err != nil {
		return err
	}
	if template != "" {
		if _, err := os.Stat(template); err != nil {
			return fmt.Errorf("format %s: template: %w", sel.Format.Name, err)
		}
	}
	if dryRun {
		for _, t := range sel.Format.Type {
			log("would write %s", rel(sel.Config.Root, filepath.Join(dir, stem+"."+t)))
		}
		return nil
	}

	doc, title, err := Document(sel, log)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lang := sel.Config.Lang
	for _, t := range sel.Format.Type {
		out := filepath.Join(dir, stem+"."+t)
		switch t {
		case config.TypeDOCX:
			err = docx.Write(out, doc, docx.Options{
				Root: sel.Config.Root, Template: template, Title: title, Lang: lang, TOC: sel.Format.TOC,
			})
			if err == nil && cover != "" {
				err = docrender.CoverDOCX(out, cover)
			}
		case config.TypePDF:
			var fonts map[string]string
			var notes []string
			fonts, notes, err = brandFonts(sel.Config.Root)
			for _, n := range notes {
				log("note: %s", n)
			}
			if err == nil {
				err = docrender.RunPDF(context.Background(), pdfTimeout, out, nil, docrender.Options{
					Root: sel.Config.Root, Cover: cover, Title: title, TOC: sel.Format.TOC, Lang: lang, Fonts: fonts,
					Markdown: doc,
				})
			}
		}
		if err != nil {
			return fmt.Errorf("%s: %w", rel(sel.Config.Root, out), err)
		}
		log("wrote %s", rel(sel.Config.Root, out))
	}
	return nil
}

// Document builds the parsed, filtered document of a document selection,
// and its title.
func Document(sel config.Selection, log Logf) (*markdown.Document, string, error) {
	log = log.or()
	text, title, err := Flatten(sel, log)
	if err != nil {
		return nil, "", err
	}
	doc := markdown.Parse([]byte(text))
	doc.Prepare(markdown.Options{Keep: sel.Keep, Formats: sel.Types(), Lang: sel.Config.Lang})
	return doc, title, nil
}

// Flatten returns a document selection's Markdown — the topic folder
// flattened, shortcodes resolved — and the document's title.
func Flatten(sel config.Selection, log Logf) (string, string, error) {
	log = log.or()
	root := sel.Config.Root
	tree, err := loadTopic(root, sel.Topic)
	if err != nil {
		return "", "", err
	}
	res, err := bookmaker.Flatten(tree, bookmaker.Options{
		ProjectRoot:   root,
		Title:         sel.Topic.Title,
		RewriteLinks:  true,
		WrapAudience:  true,
		ExtractSlides: true,
	})
	if err != nil {
		return "", "", err
	}
	for _, w := range res.Warnings {
		log("%s: warning: %s", sel.Topic.Folder, w)
	}
	log("%s: %d pages, %d links rewritten", sel.Topic.Folder, res.Pages, res.Links)
	text, err := Expander(sel, log).Expand(res.Content)
	if err != nil {
		return "", "", err
	}
	return text, tree.Title(), nil
}

// Expander returns the shortcode expander of a selection.
func Expander(sel config.Selection, log Logf) *shortcode.Expander {
	log = log.or()
	return shortcode.New(sel.Config.Root, shortcode.Variables(sel.Config.Root, sel.Vars()),
		func(format string, args ...any) { log("warning: "+format, args...) })
}

func loadTopic(root string, t config.Topic) (*bookmaker.Node, error) {
	dir := filepath.Join(root, filepath.FromSlash(t.Folder))
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("topic %s: folder %s does not exist", t.Name, t.Folder)
	}
	tree, err := bookmaker.LoadTree(dir, root)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, fmt.Errorf("topic %s: %s holds no pages", t.Name, t.Folder)
	}
	return tree, nil
}

func renderSite(sel config.Selection, dir string, dryRun bool, log Logf) error {
	if dryRun {
		log("would write the site to %s/", rel(sel.Config.Root, dir))
		return nil
	}
	topics := make([]site.Topic, len(sel.Topics))
	for i, t := range sel.Topics {
		topics[i] = site.Topic{Name: t.Name, Folder: t.Folder, Title: t.Title}
	}
	exp := Expander(sel, log)
	err := site.Build(site.Options{
		Root:    sel.Config.Root,
		OutDir:  dir,
		Title:   sel.Config.Title,
		Lang:    sel.Config.Lang,
		Topics:  topics,
		Keep:    sel.Keep,
		Formats: sel.Types(),
		Expand:  exp.Expand,
		Log:     log,
	})
	if err != nil {
		return err
	}
	log("wrote %s/index.html", rel(sel.Config.Root, dir))
	return nil
}

// clean empties the output directory of every selection once.
func clean(sels []config.Selection, log Logf) error {
	seen := map[string]bool{}
	for _, sel := range sels {
		dir, err := sel.OutputDir()
		if err != nil {
			return err
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out := filepath.Join(sel.Config.Root, filepath.FromSlash(sel.Config.Output))
		if r, err := filepath.Rel(out, dir); err != nil || r == "." || strings.HasPrefix(r, "..") {
			return fmt.Errorf("refusing to clean %s: it is not below %s", dir, out)
		}
		log("cleaning %s/", rel(sel.Config.Root, dir))
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return nil
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}
