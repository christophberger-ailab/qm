package docx

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

// styleSet looks styles up by their display name, so that a template's
// "Heading 1" is found whatever its style ID, and adds a plain fallback
// definition for the ones the template lacks.
type styleSet struct {
	byName map[string]string // "type|lower name" -> style ID
	ids    map[string]bool
	memo   map[string]string
	added  []string
}

func parseStyles(data []byte) *styleSet {
	s := &styleSet{byName: map[string]string{}, ids: map[string]bool{}, memo: map[string]string{}}
	var doc struct {
		Styles []struct {
			Type string `xml:"type,attr"`
			ID   string `xml:"styleId,attr"`
			Name struct {
				Val string `xml:"val,attr"`
			} `xml:"name"`
		} `xml:"style"`
	}
	_ = xml.Unmarshal(data, &doc) // a damaged styles part just means every style is added
	for _, st := range doc.Styles {
		s.ids[st.ID] = true
		key := st.Type + "|" + strings.ToLower(st.Name.Val)
		if _, dup := s.byName[key]; !dup {
			s.byName[key] = st.ID
		}
	}
	return s
}

// find returns the ID of the first style (of the type) named like one of
// names, or "".
func (s *styleSet) find(typ string, names ...string) string {
	for _, n := range names {
		if id, ok := s.byName[typ+"|"+strings.ToLower(n)]; ok {
			return id
		}
	}
	return ""
}

// need returns the ID of the style named like one of names; failing that
// it adds the fallback definition under id and returns that.
func (s *styleSet) need(typ, id, body string, names ...string) string {
	key := typ + "|" + id
	if v, ok := s.memo[key]; ok {
		return v
	}
	found := s.find(typ, names...)
	if found == "" {
		for s.ids[id] {
			id += "Qm"
		}
		s.ids[id] = true
		s.byName[typ+"|"+strings.ToLower(names[0])] = id
		s.added = append(s.added, fmt.Sprintf(`<w:style w:type="%s" w:customStyle="1" w:styleId="%s"><w:name w:val="%s"/>%s</w:style>`,
			typ, id, esc(names[0]), body))
		found = id
	}
	s.memo[key] = found
	return found
}

func (s *styleSet) normal() string {
	if id := s.find("paragraph", "Normal"); id != "" {
		return id
	}
	return "Normal"
}

func (s *styleSet) heading(level int) string {
	sizes := []int{32, 28, 26, 24, 22, 22}
	body := fmt.Sprintf(`<w:basedOn w:val="%s"/><w:next w:val="%s"/><w:qFormat/>`+
		`<w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="%d"/></w:pPr>`+
		`<w:rPr><w:b/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr>`,
		s.normal(), s.normal(), level-1, sizes[level-1], sizes[level-1])
	id := fmt.Sprintf("Heading%d", level)
	return s.need("paragraph", id, body, fmt.Sprintf("heading %d", level), fmt.Sprintf("Heading %d", level))
}

// body is the style of ordinary paragraphs: "Body Text" if the document has
// it, else none (Normal).
func (s *styleSet) body() string { return s.find("paragraph", "Body Text") }

func (s *styleSet) listPara() string { return s.find("paragraph", "List Paragraph") }

func (s *styleSet) quote() string {
	return s.need("paragraph", "Quote", `<w:basedOn w:val="`+s.normal()+`"/><w:qFormat/>`+
		`<w:pPr><w:pBdr><w:left w:val="single" w:sz="18" w:space="8" w:color="BFBFBF"/></w:pBdr><w:ind w:left="567" w:right="567"/></w:pPr>`+
		`<w:rPr><w:i/></w:rPr>`, "Block Text", "Quote")
}

func (s *styleSet) code() string {
	return s.need("paragraph", "SourceCode", `<w:basedOn w:val="`+s.normal()+`"/>`+
		`<w:pPr><w:keepLines/><w:shd w:val="clear" w:color="auto" w:fill="F5F5F5"/><w:spacing w:before="120" w:after="120" w:line="240" w:lineRule="auto"/></w:pPr>`+
		`<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:eastAsia="Consolas" w:cs="Courier New"/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr>`,
		"Source Code")
}

func (s *styleSet) verbatim() string {
	return s.need("character", "VerbatimChar",
		`<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:eastAsia="Consolas" w:cs="Courier New"/><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr>`,
		"Verbatim Char")
}

func (s *styleSet) hyperlink() string {
	return s.need("character", "Hyperlink",
		`<w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr>`, "Hyperlink")
}

func (s *styleSet) footnoteText() string {
	return s.need("paragraph", "FootnoteText", `<w:basedOn w:val="`+s.normal()+`"/>`+
		`<w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:rPr><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr>`,
		"footnote text")
}

func (s *styleSet) footnoteRef() string {
	return s.need("character", "FootnoteReference", `<w:rPr><w:vertAlign w:val="superscript"/></w:rPr>`,
		"footnote reference")
}

func (s *styleSet) tocHeading() string {
	return s.need("paragraph", "TOCHeading", `<w:basedOn w:val="`+s.normal()+`"/><w:next w:val="`+s.normal()+`"/>`+
		`<w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/><w:szCs w:val="32"/></w:rPr>`,
		"TOC Heading")
}

func (s *styleSet) toc(level int) string {
	return s.need("paragraph", fmt.Sprintf("TOC%d", level), fmt.Sprintf(
		`<w:basedOn w:val="%s"/><w:next w:val="%s"/><w:uiPriority w:val="39"/><w:pPr><w:spacing w:after="100"/><w:ind w:left="%d"/></w:pPr>`,
		s.normal(), s.normal(), 220*(level-1)), fmt.Sprintf("toc %d", level))
}

// appendTo adds the fallback definitions to styles.xml.
func (s *styleSet) appendTo(styles string) string {
	if len(s.added) == 0 {
		return styles
	}
	return insertBefore(styles, "</w:styles>", strings.Join(s.added, ""))
}

var (
	reRPrDefault = regexp.MustCompile(`(<w:rPrDefault>\s*<w:rPr>)`)
	reSpacingDef = regexp.MustCompile(`(?s)(<w:pPrDefault>.*?)<w:spacing[^>]*>(?:</w:spacing>)?`)
	reLangTag    = regexp.MustCompile(`<w:lang\b[^>]*?(?:/>|>\s*</w:lang>)`)
)

// tuneBase gives the docxgo base document, whose Normal style carries no
// font at all, a readable body font and paragraph spacing.
func tuneBase(styles string) string {
	styles = reRPrDefault.ReplaceAllString(styles,
		`$1<w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/><w:sz w:val="22"/><w:szCs w:val="22"/>`)
	return reSpacingDef.ReplaceAllString(styles,
		`${1}<w:spacing w:before="0" w:after="120" w:line="276" w:lineRule="auto"/>`)
}

// setLang sets the proofing language in the document defaults of a
// template's styles.
func setLang(styles, lang string) string {
	if lang == "" {
		return styles
	}
	start := strings.Index(styles, "<w:docDefaults>")
	end := strings.Index(styles, "</w:docDefaults>")
	if start < 0 || end < start {
		return styles
	}
	defaults := styles[start:end]
	tag := `<w:lang w:val="` + esc(lang) + `"/>`
	if reLangTag.MatchString(defaults) {
		defaults = reLangTag.ReplaceAllLiteralString(defaults, tag)
	} else if i := strings.Index(defaults, "</w:rPr>"); i >= 0 && strings.Contains(defaults, "<w:rPrDefault>") {
		defaults = defaults[:i] + tag + defaults[i:]
	} else if i := strings.Index(defaults, "<w:rPrDefault/>"); i >= 0 {
		defaults = strings.Replace(defaults, "<w:rPrDefault/>", "<w:rPrDefault><w:rPr>"+tag+"</w:rPr></w:rPrDefault>", 1)
	} else {
		defaults = strings.Replace(defaults, "<w:docDefaults>", "<w:docDefaults><w:rPrDefault><w:rPr>"+tag+"</w:rPr></w:rPrDefault>", 1)
	}
	return styles[:start] + defaults + styles[end:]
}
