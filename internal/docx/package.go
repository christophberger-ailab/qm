package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// pkg is an OOXML package held in memory. Part order is kept so the
// [Content_Types].xml part stays first, as Word likes it.
type pkg struct {
	names []string
	files map[string][]byte
}

func readPkg(data []byte) (*pkg, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("docx: not a DOCX package: %w", err)
	}
	p := &pkg{files: map[string][]byte{}}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return nil, err
		}
		p.set(f.Name, b)
	}
	return p, nil
}

func (p *pkg) get(name string) []byte { return p.files[name] }

func (p *pkg) has(name string) bool { _, ok := p.files[name]; return ok }

func (p *pkg) set(name string, data []byte) {
	if _, ok := p.files[name]; !ok {
		p.names = append(p.names, name)
	}
	p.files[name] = data
}

func (p *pkg) bytes() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range p.names {
		f, err := zw.Create(n)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(p.files[n]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const (
	nsW   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsR   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsWP  = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	nsA   = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsPic = "http://schemas.openxmlformats.org/drawingml/2006/picture"

	relBase     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	relImage    = relBase + "image"
	relLink     = relBase + "hyperlink"
	relFootnote = relBase + "footnotes"
	relNumber   = relBase + "numbering"
)

// splitBody cuts word/document.xml into the part before the body content
// (root element and <w:body> start), the final section properties that are
// a direct child of the body, and the part from </w:body> on. Everything in
// between — the template's own content — is dropped.
func splitBody(doc []byte) (prefix, sect, suffix string, err error) {
	dec := xml.NewDecoder(bytes.NewReader(doc))
	depth := 0
	bodyStart, bodyEnd, sStart, sEnd := int64(-1), int64(-1), int64(-1), int64(-1)
	for {
		off := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", "", fmt.Errorf("docx: word/document.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 && t.Name.Local == "body" {
				bodyStart = dec.InputOffset()
			}
			if depth == 3 && t.Name.Local == "sectPr" {
				sStart = off
			}
		case xml.EndElement:
			if depth == 3 && t.Name.Local == "sectPr" {
				sEnd = dec.InputOffset()
			}
			if depth == 2 && t.Name.Local == "body" {
				bodyEnd = off
			}
			depth--
		}
	}
	if bodyStart < 0 || bodyEnd < 0 {
		return "", "", "", fmt.Errorf("docx: word/document.xml has no body")
	}
	if sStart >= 0 && sEnd > sStart {
		sect = string(doc[sStart:sEnd])
	}
	return string(doc[:bodyStart]), sect, string(doc[bodyEnd:]), nil
}

var rootTag = regexp.MustCompile(`<w:document\b[^>]*>`)

// declareNS makes sure the document root declares the namespaces the
// generated body uses.
func declareNS(prefix string) string {
	return rootTag.ReplaceAllStringFunc(prefix, func(tag string) string {
		for _, ns := range [][2]string{{"w", nsW}, {"r", nsR}, {"wp", nsWP}, {"a", nsA}, {"pic", nsPic}} {
			if !strings.Contains(tag, "xmlns:"+ns[0]+"=") {
				tag = strings.TrimSuffix(tag, ">") + ` xmlns:` + ns[0] + `="` + ns[1] + `">`
			}
		}
		return tag
	})
}

var (
	reSectW    = regexp.MustCompile(`<w:pgSz\b[^>]*\bw:w="(\d+)"`)
	reMarLeft  = regexp.MustCompile(`<w:pgMar\b[^>]*\bw:left="(\d+)"`)
	reMarRight = regexp.MustCompile(`<w:pgMar\b[^>]*\bw:right="(\d+)"`)
)

// textWidth is the width of the text area in twips, read from the section
// properties; A4 with 2.5 cm margins if they say nothing.
func textWidth(sect string) int {
	num := func(re *regexp.Regexp, def int) int {
		if m := re.FindStringSubmatch(sect); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				return v
			}
		}
		return def
	}
	w := num(reSectW, 11906) - num(reMarLeft, 1417) - num(reMarRight, 1417)
	if w < 1440 {
		w = 9072
	}
	return w
}

const defaultSect = `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
	`<w:pgMar w:top="1417" w:right="1417" w:bottom="1417" w:left="1417" w:header="709" w:footer="709" w:gutter="0"/></w:sectPr>`

type relationship struct{ id, typ, target string }

// addRels appends relationships to word/_rels/document.xml.rels.
func addRels(p *pkg, rels []relationship) {
	if len(rels) == 0 {
		return
	}
	const name = "word/_rels/document.xml.rels"
	var b strings.Builder
	for _, r := range rels {
		fmt.Fprintf(&b, `<Relationship Id="%s" Type="%s" Target="%s"`, r.id, r.typ, esc(r.target))
		if r.typ == relLink {
			b.WriteString(` TargetMode="External"`)
		}
		b.WriteString(`/>`)
	}
	x := string(p.get(name))
	if x == "" {
		x = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`
	}
	p.set(name, []byte(insertBefore(x, "</Relationships>", b.String())))
}

// relTarget finds the target of the first document relationship of a type,
// as a package part name.
func relTarget(p *pkg, typ string) string {
	var rels struct {
		R []struct {
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(p.get("word/_rels/document.xml.rels"), &rels) != nil {
		return ""
	}
	for _, r := range rels.R {
		if r.Type == typ {
			t := strings.TrimPrefix(r.Target, "/")
			if !strings.HasPrefix(r.Target, "/") {
				t = "word/" + t
			}
			return t
		}
	}
	return ""
}

// ensureContentType registers a default (by extension) or an override (by
// part name) content type unless the package has one already.
func ensureContentType(p *pkg, key, ctype string, override bool) {
	const name = "[Content_Types].xml"
	x := string(p.get(name))
	var tag string
	if override {
		if strings.Contains(x, `PartName="`+key+`"`) {
			return
		}
		tag = fmt.Sprintf(`<Override PartName="%s" ContentType="%s"/>`, key, ctype)
	} else {
		if regexp.MustCompile(`(?i)Extension="` + regexp.QuoteMeta(key) + `"`).MatchString(x) {
			return
		}
		tag = fmt.Sprintf(`<Default Extension="%s" ContentType="%s"/>`, key, ctype)
	}
	p.set(name, []byte(insertBefore(x, "</Types>", tag)))
}

// insertBefore inserts s before the last occurrence of marker; without
// marker the document is returned unchanged.
func insertBefore(doc, marker, s string) string {
	i := strings.LastIndex(doc, marker)
	if i < 0 {
		return doc
	}
	return doc[:i] + s + doc[i:]
}

// insertBeforeFirst inserts s before the first of the markers that occurs
// in doc (searching marker by marker, in the order given), else before
// fallback.
func insertBeforeFirst(doc string, markers []string, fallback, s string) string {
	best := -1
	for _, m := range markers {
		if i := strings.Index(doc, m); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	if best < 0 {
		return insertBefore(doc, fallback, s)
	}
	return doc[:best] + s + doc[best:]
}

// settingsAfterFootnotePr lists the settings elements that follow
// w:footnotePr in the schema's sequence.
var settingsAfterFootnotePr = []string{"<w:endnotePr", "<w:compat", "<w:docVars", "<w:rsids", "<m:mathPr",
	"<w:attachedSchema", "<w:themeFontLang", "<w:clrSchemeMapping", "<w:doNotIncludeSubdocsInStats",
	"<w:doNotAutoCompressPictures", "<w:forceUpgrade", "<w:captions", "<w:readModeInkLockDown",
	"<w:smartTagType", "<w:schemaLibrary", "<w:shapeDefaults", "<w:doNotEmbedSmartTags",
	"<w:decimalSymbol", "<w:listSeparator"}

// patchSettings adds w:updateFields (Word refreshes the TOC on opening)
// and the separator references for footnotes.
func patchSettings(p *pkg, updateFields, footnotes bool) {
	x := string(p.get("word/settings.xml"))
	if x == "" {
		return
	}
	if footnotes && !strings.Contains(x, "<w:footnotePr") {
		x = insertBeforeFirst(x, settingsAfterFootnotePr, "</w:settings>",
			`<w:footnotePr><w:footnote w:id="-1"/><w:footnote w:id="0"/></w:footnotePr>`)
	}
	if updateFields && !strings.Contains(x, "<w:updateFields") {
		x = insertBeforeFirst(x, append([]string{"<w:hdrShapeDefaults", "<w:footnotePr"}, settingsAfterFootnotePr...),
			"</w:settings>", `<w:updateFields w:val="true"/>`)
	}
	p.set("word/settings.xml", []byte(x))
}

// setTitle writes dc:title into the core properties.
func setTitle(p *pkg, title string) {
	const name = "docProps/core.xml"
	x := string(p.get(name))
	if x == "" || title == "" {
		return
	}
	el := "<dc:title>" + esc(title) + "</dc:title>"
	re := regexp.MustCompile(`(?s)<dc:title\s*/>|<dc:title\b[^>]*>.*?</dc:title>`)
	if re.MatchString(x) {
		x = re.ReplaceAllLiteralString(x, el)
	} else {
		x = insertBefore(x, "</cp:coreProperties>", el)
	}
	p.set(name, []byte(x))
}
