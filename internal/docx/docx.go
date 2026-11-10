// Package docx writes a Word document from a parsed Markdown syntax tree,
// in pure Go.
//
// The document body is generated as WordprocessingML and placed into a base
// package: the reference template if there is one (its styles, page setup,
// headers, footers and numbering are kept, its body is discarded), else a
// blank document made with docxgo (github.com/mmonterroca/docxgo/v2).
// docxgo has no bookmarks, internal hyperlinks, footnotes, list restarts or
// table shading, so the body itself is written as raw OOXML here.
package docx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"

	"github.com/christophberger-ailab/qm/internal/markdown"
)

// Options configure Write.
type Options struct {
	// Root is the project root (absolute). Image paths starting with "/"
	// are relative to it; other relative image paths are relative to Root
	// as well.
	Root string
	// Template is an optional absolute path to a reference .docx whose
	// styles, page setup, headers/footers and numbering are used; its body
	// content is discarded.
	Template string
	// Title is the document title (core properties).
	Title string
	// Lang is the document language, like "de" or "de-DE"; it sets the
	// proofing language.
	Lang string
	// TOC inserts a Word table of contents field, headed "Inhaltsverzeichnis"
	// for German and "Contents" otherwise, at the start.
	TOC bool
}

// Write writes the prepared document d to a DOCX file at path.
func Write(path string, d *markdown.Document, o Options) error {
	base, err := basePackage(o)
	if err != nil {
		return err
	}
	p, err := readPkg(base)
	if err != nil {
		return err
	}
	docXML := p.get("word/document.xml")
	if docXML == nil {
		return fmt.Errorf("docx: no word/document.xml in %s", cmpOr(o.Template, "the base document"))
	}
	prefix, sect, suffix, err := splitBody(docXML)
	if err != nil {
		return err
	}
	if sect == "" {
		sect = defaultSect
	}

	styles := string(p.get("word/styles.xml"))
	if o.Template != "" {
		styles = setLang(styles, o.Lang)
	} else {
		styles = tuneBase(styles)
	}
	w := newWriter(d, o, p, parseStyles([]byte(styles)), textWidth(sect))
	w.document()
	if w.err != nil {
		return w.err
	}

	p.set("word/document.xml", []byte(declareNS(prefix)+w.out.String()+sect+suffix))
	p.set("word/styles.xml", []byte(w.styles.appendTo(styles)))
	w.finishParts(p)
	if o.Template != "" {
		setTitle(p, o.Title)
	}
	data, err := p.bytes()
	if err != nil {
		return err
	}
	return writeFile(path, data)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// basePackage returns the bytes of the DOCX the content is poured into.
func basePackage(o Options) ([]byte, error) {
	if o.Template != "" {
		b, err := os.ReadFile(o.Template)
		if err != nil {
			return nil, fmt.Errorf("docx: template: %w", err)
		}
		return b, nil
	}
	doc := dx.NewDocument()
	if err := doc.SetMetadata(&domain.Metadata{Title: o.Title}); err != nil {
		return nil, err
	}
	if o.Lang != "" {
		if err := doc.SetLanguage(&domain.Language{Val: o.Lang}); err != nil {
			return nil, err
		}
	}
	sec, err := doc.DefaultSection()
	if err != nil {
		return nil, err
	}
	if err := sec.SetPageSize(domain.PageSizeA4); err != nil {
		return nil, err
	}
	// 2.5 cm margins
	if err := sec.SetMargins(domain.Margins{Top: 1417, Right: 1417, Bottom: 1417, Left: 1417, Header: 709, Footer: 709}); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeFile writes data to path through a temporary file, so a failed run
// leaves no half-written document behind.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".docx-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// esc escapes text for XML content and attribute values and drops the
// characters XML 1.0 cannot carry.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000:
			b.WriteRune(r)
		}
	}
	return b.String()
}
