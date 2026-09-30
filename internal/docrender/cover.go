package docrender

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const wordNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// CoverDOCX prepends a page-anchored PNG in its own section. All original
// package parts, styles, relationships and body section properties survive.
// The output is staged beside the destination and replaced only after closing.
func CoverDOCX(path, cover string) error {
	if err := ValidateCover(cover); err != nil {
		return err
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer z.Close()
	files := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return err
		}
		files[f.Name] = b
	}
	doc := files["word/document.xml"]
	rels := files["word/_rels/document.xml.rels"]
	types := files["[Content_Types].xml"]
	if len(doc) == 0 || len(rels) == 0 || len(types) == 0 {
		return fmt.Errorf("incomplete DOCX package")
	}
	bodyStart, section, w, h, err := bodyGeometry(doc)
	if err != nil {
		return err
	}
	// Read the section explicitly to validate it, but do not rewrite it. Its
	// margins, headers/footers, page numbering and other settings belong to body.
	_ = section
	id := "qmCover"
	for bytes.Contains(rels, []byte(`Id="`+id+`"`)) {
		id += "x"
	}
	media := "qm-cover.png"
	for files["word/media/"+media] != nil {
		media = "x" + media
	}
	image, err := os.ReadFile(cover)
	if err != nil {
		return err
	}
	files["word/media/"+media] = image
	rel := fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/%s"/>`, id, media)
	rels, err = appendXML(rels, rel)
	if err != nil {
		return err
	}
	files["word/_rels/document.xml.rels"] = rels
	types, err = appendXML(types, fmt.Sprintf(`<Override PartName="/word/media/%s" ContentType="image/png"/>`, media))
	if err != nil {
		return err
	}
	files["[Content_Types].xml"] = types
	c, err := imageSize(cover)
	if err != nil {
		return err
	}
	cropX, cropY := 0, 0
	if float64(c.Width)/float64(c.Height) > float64(w)/float64(h) {
		cropX = int((1 - float64(w)*float64(c.Height)/(float64(h)*float64(c.Width))) * 50000)
	} else {
		cropY = int((1 - float64(h)*float64(c.Width)/(float64(w)*float64(c.Height))) * 50000)
	}
	// Local namespaces avoid assumptions about the document root's prefixes.
	// The cover section has no header/footer references; the first section
	// consequently has none. The following original section keeps its own.
	fragment := fmt.Sprintf(`<w:p xmlns:w="%s" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:pPr><w:spacing w:before="0" w:after="0"/><w:sectPr><w:type w:val="nextPage"/><w:pgSz w:w="%d" w:h="%d"/><w:pgMar w:top="0" w:right="0" w:bottom="0" w:left="0" w:header="0" w:footer="0" w:gutter="0"/></w:sectPr></w:pPr><w:r><w:drawing><wp:anchor xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" distT="0" distB="0" distL="0" distR="0" simplePos="0" relativeHeight="0" behindDoc="1" locked="0" layoutInCell="1" allowOverlap="1"><wp:simplePos x="0" y="0"/><wp:positionH relativeFrom="page"><wp:posOffset>0</wp:posOffset></wp:positionH><wp:positionV relativeFrom="page"><wp:posOffset>0</wp:posOffset></wp:positionV><wp:extent cx="%d" cy="%d"/><wp:effectExtent l="0" t="0" r="0" b="0"/><wp:wrapNone/><wp:docPr id="2147483646" name="qm cover"/><wp:cNvGraphicFramePr/><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:nvPicPr><pic:cNvPr id="0" name="Cover"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:srcRect l="%d" r="%d" t="%d" b="%d"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:anchor></w:drawing></w:r></w:p>`, wordNS, w, h, w*635, h*635, id, cropX, cropX, cropY, cropY, w*635, h*635)
	files["word/document.xml"] = append(append(append([]byte{}, doc[:bodyStart]...), []byte(fragment)...), doc[bodyStart:]...)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".qm-cover-*.docx")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	zw := zip.NewWriter(tmp)
	for _, f := range z.File {
		header := f.FileHeader
		out, err := zw.CreateHeader(&header)
		if err != nil {
			tmp.Close()
			return err
		}
		if _, err = out.Write(files[f.Name]); err != nil {
			tmp.Close()
			return err
		}
		delete(files, f.Name)
	}
	for name, b := range files {
		out, err := zw.Create(name)
		if err != nil {
			tmp.Close()
			return err
		}
		if _, err = out.Write(b); err != nil {
			tmp.Close()
			return err
		}
	}
	if err = zw.Close(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = z.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func bodyGeometry(doc []byte) (start int, section []byte, w, h int, err error) {
	w, h = 11906, 16838
	d := xml.NewDecoder(bytes.NewReader(doc))
	depth := 0
	sectionStart := -1
	for {
		before := int(d.InputOffset())
		token, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			err = e
			return
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Space == wordNS && t.Name.Local == "body" {
				start = int(d.InputOffset())
			}
			if t.Name.Space == wordNS && t.Name.Local == "sectPr" && depth == 3 {
				sectionStart = before
			}
			if sectionStart >= 0 && t.Name.Local == "pgSz" {
				for _, a := range t.Attr {
					if a.Name.Local == "w" {
						w, _ = strconv.Atoi(a.Value)
					}
					if a.Name.Local == "h" {
						h, _ = strconv.Atoi(a.Value)
					}
				}
			}
		case xml.EndElement:
			if sectionStart >= 0 && depth == 3 && t.Name.Local == "sectPr" {
				section = doc[sectionStart:int(d.InputOffset())]
				sectionStart = -1
			}
			depth--
		}
	}
	if start == 0 || w <= 0 || h <= 0 {
		err = fmt.Errorf("invalid DOCX body/page geometry")
	}
	return
}
func appendXML(b []byte, fragment string) ([]byte, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	depth := 0
	for {
		offset := int(d.InputOffset())
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch t.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
			if depth == 0 {
				if !strings.HasPrefix(string(b[offset:]), "</") {
					return nil, fmt.Errorf("self-closing package XML root")
				}
				return append(append(append([]byte{}, b[:offset]...), []byte(fragment)...), b[offset:]...), nil
			}
		}
	}
}
