package bookrender

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/christophberger-ailab/qm/internal/docrender"
	"github.com/christophberger-ailab/qm/internal/qmcore"
)

//go:embed capture.lua
var captureFilter []byte

// renderSelection retains Quarto's full preprocessing and mature DOCX writer.
// A final Pandoc filter captures the same filtered AST for the Go PDF backend.
func renderSelection(root string, sel qmcore.Selection, dry bool, log Logf) error {
	ps, err := qmcore.LoadSelection(root, sel)
	if err != nil {
		return err
	}
	switch ps.Format.QM.Renderer {
	case "", "quarto":
		return quarto(root, sel, dry, log)
	case "qm":
	default:
		return fmt.Errorf("unknown qm renderer %q", ps.Format.QM.Renderer)
	}
	if _, ok := ps.Format.Format["docx"]; !ok {
		return fmt.Errorf("qm: renderer: go requires format: docx")
	}
	if _, ok := ps.Format.Format["pdf"]; !ok {
		return fmt.Errorf("qm: renderer: go requires format: pdf and docx")
	}
	for name := range ps.Format.Format {
		if name != "pdf" && name != "docx" {
			return fmt.Errorf("qm: renderer: go supports PDF/DOCX, not %s", name)
		}
	}
	cover, err := qmcore.ResolveVars(ps.Topic.QM.Cover, ps.Vars())
	if err != nil {
		return err
	}
	if cover != "" {
		cover, err = qmcore.ProjectPath(root, cover)
		if err != nil {
			return err
		}
		if err = docrender.ValidateCover(cover); err != nil {
			return err
		}
	}
	dir, err := qmcore.FormatOutputDir(ps.Format)
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("Go renderer requires project: output-dir")
	}
	suffix, err := qmcore.ResolveVars(ps.Format.QM.OutputDirSuffix, ps.Vars())
	if err != nil {
		return err
	}
	dir, err = qmcore.ProjectPath(root, dir+suffix)
	if err != nil {
		return err
	}
	stem := ps.Format.QM.OutputFile
	if stem == "" {
		for _, p := range ps.All() {
			if p.Book.OutputFile != "" {
				stem = p.Book.OutputFile
				break
			}
		}
	}
	stem, err = qmcore.ResolveVars(stem, ps.Vars())
	if err != nil {
		return err
	}
	if stem == "" || filepath.Base(stem) != stem || strings.ContainsAny(stem, `/\`) || stem == "." || stem == ".." {
		return fmt.Errorf("Go renderer requires a filename stem in book: output-file or qm: output-file")
	}
	docx := filepath.Join(dir, stem+".docx")
	pdf := filepath.Join(dir, stem+".pdf")
	log("$ %s render --profile %s --no-clean --to docx [capture filtered AST]", QuartoCommand, sel)
	log("Go renderer: %s + %s (cover: %s)", pdf, docx, cover)
	if dry {
		return nil
	}
	if err = os.MkdirAll(BuildDirPath(root), 0755); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(BuildDirPath(root), "native-")
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if complete {
			os.RemoveAll(scratch)
		} else {
			log("intermediates retained for diagnosis: %s", scratch)
		}
	}()
	filter := filepath.Join(scratch, "capture.lua")
	ast := filepath.Join(scratch, "document.json")
	if err = os.WriteFile(filter, captureFilter, 0644); err != nil {
		return err
	}
	cmd := exec.Command(QuartoCommand, "render", "--profile", sel.String(), "--no-clean", "--to", "docx", "--lua-filter", filter)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "QM_CAPTURE_AST="+ast)
	w := &lineWriter{log: log}
	cmd.Stdout, cmd.Stderr = w, w
	err = cmd.Run()
	w.flush()
	if err != nil {
		return fmt.Errorf("Quarto DOCX preprocessing: %w", err)
	}
	f, err := os.Open(ast)
	if err != nil {
		return fmt.Errorf("filtered document was not captured: %w", err)
	}
	doc, err := docrender.Read(f)
	f.Close()
	if err != nil {
		return err
	}
	title, err := ps.Title()
	if err != nil {
		return err
	}
	options := docrender.Options{Root: root, Cover: cover, Title: title, TOC: ps.Format.Format["pdf"].TOC}
	tmpPDF := filepath.Join(scratch, "document.pdf")
	if err = docrender.PDF(tmpPDF, doc, options); err != nil {
		return fmt.Errorf("PDF: %w", err)
	}
	if cover != "" {
		if err = docrender.CoverDOCX(docx, cover); err != nil {
			return fmt.Errorf("DOCX cover: %w", err)
		}
	}
	if err = os.Rename(tmpPDF, pdf); err != nil {
		return err
	}
	log("created %s and %s", pdf, docx)
	complete = true
	return nil
}
