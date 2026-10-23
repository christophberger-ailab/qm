package bookrender

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/christophberger-ailab/qm/internal/docrender"
	"github.com/christophberger-ailab/qm/internal/qmcore"
)

// The qm renderer is carried out by the project's own hooks, so that a plain
// `quarto render --profile …` produces exactly what `qm render` does:
//
//  1. `qm prepare` writes the capture filter to _build/.
//  2. Quarto renders the DOCX, its only format. The format profile declares
//     the capture filter at the post-finalize entry point, which saves the
//     filtered document to _build/ in the same Pandoc run.
//  3. `qm finalize` lays out the PDF beside the DOCX from the captured
//     document and puts the cover on both.
//
// LaTeX and Typst are never invoked: the format profile declares no
// `format: pdf:`, so Quarto never typesets a PDF.

// DefaultPDFTimeout bounds the Go PDF layout. A full handout lays out in
// seconds; the limit only exists so a layout bug fails the render instead
// of hanging it (and qm web) forever. Override with QM_PDF_TIMEOUT
// (a Go duration such as "20m"; "0" disables the limit).
const DefaultPDFTimeout = 5 * time.Minute

func pdfTimeout() (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv("QM_PDF_TIMEOUT"))
	if v == "" {
		return DefaultPDFTimeout, nil
	}
	if v == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("QM_PDF_TIMEOUT: invalid duration %q", v)
	}
	return d, nil
}

//go:embed capture.lua
var captureFilter []byte

// The capture filter and the document it captures, in BuildDir. The
// document's name is also fixed in capture.lua.
const (
	captureFilterName = "qm-capture.lua"
	capturedDocName   = "qm-document.json"
)

// CaptureFilterPath is the filter's project-relative path, the one a format
// profile declares.
const CaptureFilterPath = BuildDir + "/" + captureFilterName

// CaptureFilterFile and CapturedDocFile locate the filter and its output.
func CaptureFilterFile(root string) string {
	return filepath.Join(BuildDirPath(root), captureFilterName)
}
func CapturedDocFile(root string) string { return filepath.Join(BuildDirPath(root), capturedDocName) }

// Native is a selection's qm renderer configuration, resolved and checked.
type Native struct {
	// Cover is the absolute path of the cover PNG, or "" for none.
	Cover string
	// Title is the resolved book title, the PDF's document title.
	Title string
	// TOC adds a contents list to the PDF (`qm: pdf: toc:`).
	TOC bool
}

// LoadNative returns the selection's qm renderer configuration, or nil when
// the format renders with Quarto alone. Every check runs here, so that the
// pre-render hook fails before Quarto has written anything.
func LoadNative(root string, ps *qmcore.Profiles) (*Native, error) {
	f := ps.Format
	switch f.QM.Renderer {
	case "", "quarto":
		return nil, nil
	case "qm":
	default:
		return nil, fmt.Errorf("%s: unknown qm: renderer: %q (want qm or quarto)", f.Path, f.QM.Renderer)
	}
	if _, ok := f.Format["pdf"]; ok {
		return nil, fmt.Errorf("%s: qm: renderer: qm writes the PDF itself; remove format: pdf: "+
			"(Quarto would typeset it with LaTeX) and set its options under qm: pdf:", f.Path)
	}
	if _, ok := f.Format["docx"]; !ok {
		return nil, fmt.Errorf("%s: qm: renderer: qm requires format: docx", f.Path)
	}
	for name := range f.Format {
		if name != "docx" {
			return nil, fmt.Errorf("%s: qm: renderer: qm supports format: docx only, not %s", f.Path, name)
		}
	}
	if !declaresCapture(f) {
		return nil, fmt.Errorf("%s: qm: renderer: qm requires the capture filter:\n"+
			"filters:\n  - path: %s\n    at: post-finalize", f.Path, CaptureFilterPath)
	}
	cover, err := qmcore.ResolveVars(ps.Topic.QM.Cover, ps.Vars())
	if err != nil {
		return nil, fmt.Errorf("%s: qm: cover: %w", ps.Topic.Path, err)
	}
	if cover != "" {
		if cover, err = qmcore.ProjectPath(root, cover); err != nil {
			return nil, fmt.Errorf("%s: qm: cover: %w", ps.Topic.Path, err)
		}
		if err = docrender.ValidateCover(cover); err != nil {
			return nil, fmt.Errorf("%s: qm: cover: %w", ps.Topic.Path, err)
		}
	}
	title, err := ps.Title()
	if err != nil {
		return nil, err
	}
	return &Native{Cover: cover, Title: title, TOC: f.QM.PDF.TOC}, nil
}

// declaresCapture reports whether the format profile runs the capture filter
// at the post-finalize entry point. Any earlier entry point would capture
// the document before Quarto's own filters have finished with it.
func declaresCapture(p *qmcore.Profile) bool {
	for _, f := range p.Filters {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		path, _ := m["path"].(string)
		at, _ := m["at"].(string)
		if filepath.ToSlash(filepath.Clean(path)) == CaptureFilterPath && at == "post-finalize" {
			return true
		}
	}
	return false
}

// Prepare writes the capture filter and removes the document a previous
// render captured, so that a capture that does not happen is noticed
// instead of laying out the previous document.
func (n *Native) Prepare(root string) error {
	if err := os.MkdirAll(BuildDirPath(root), 0o755); err != nil {
		return err
	}
	if err := os.Remove(CapturedDocFile(root)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(CaptureFilterFile(root), captureFilter, 0o644)
}

// Finish turns the DOCX Quarto wrote into the qm renderer's output: the PDF
// beside it, under the same name, and the cover on both. The captured
// document is removed on success and kept for diagnosis otherwise.
func (n *Native) Finish(root, docx string, log Logf) (err error) {
	log = log.or()
	captured := CapturedDocFile(root)
	defer func() {
		if err == nil {
			os.Remove(captured)
		} else if _, statErr := os.Stat(captured); statErr == nil {
			log("captured document retained for diagnosis: %s", captured)
		}
	}()
	f, err := os.Open(captured)
	if err != nil {
		return fmt.Errorf("the filtered document was not captured (%w); "+
			"is %s declared at post-finalize in the format profile?", err, CaptureFilterPath)
	}
	doc, err := docrender.Read(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", captured, err)
	}
	timeout, err := pdfTimeout()
	if err != nil {
		return err
	}
	pdf := strings.TrimSuffix(docx, filepath.Ext(docx)) + ".pdf"
	tmp, err := os.CreateTemp(filepath.Dir(pdf), ".qm-*.pdf")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	options := docrender.Options{Root: root, Cover: n.Cover, Title: n.Title, TOC: n.TOC}
	if err = docrender.RunPDF(context.Background(), timeout, tmp.Name(), doc, options); err != nil {
		return fmt.Errorf("PDF: %w", err)
	}
	if n.Cover != "" {
		if err = docrender.CoverDOCX(docx, n.Cover); err != nil {
			return fmt.Errorf("DOCX cover: %w", err)
		}
	}
	if err = os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), pdf); err != nil {
		return err
	}
	if n.Cover != "" {
		log("cover %s added to %s", relTo(root, n.Cover), relTo(root, docx))
	}
	log("created %s", relTo(root, pdf))
	return nil
}

func relTo(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(rel)
	}
	return p
}
