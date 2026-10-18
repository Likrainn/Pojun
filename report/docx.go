// Package report 的 docx 子模块:手写的最小 OOXML(.docx)生成器。
// 不引入第三方文档库——.docx 本质是 zip + XML,报告需要的元素(标题层级/
// 段落/列表/表格/分页)用一小段生成器即可覆盖,换来:零新依赖、单二进制哲学
// 不破、输出结构完全可控(内置中文报告模板的字体/字号/表格边框都在 styles.xml)。
package report

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
)

// Docx 是一份正在组装的文档;方法按顺序追加元素。
type Docx struct {
	buf bytes.Buffer
}

// NewDocx 建立空文档(自动加封面后的第一个标题也由调用方控制)。
func NewDocx() *Docx { return &Docx{} }

var xmlEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

func esc(s string) string { return xmlEsc.Replace(s) }

// H1/H2/H3 标题。
func (d *Docx) H1(text string) { d.heading(text, 1) }
func (d *Docx) H2(text string) { d.heading(text, 2) }
func (d *Docx) H3(text string) { d.heading(text, 3) }

func (d *Docx) heading(text string, level int) {
	fmt.Fprintf(&d.buf, `<w:p><w:pPr><w:pStyle w:val="Heading%d"/></w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, level, esc(text))
}

// P 正文段落(支持 \n 拆多段)。
func (d *Docx) P(text string) {
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(&d.buf, `<w:p><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, esc(line))
	}
}

// Code 等宽代码块(证据/PoC 用):浅灰底、Consolas、保留空白。
func (d *Docx) Code(text string) {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		b.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="Code"/><w:shd w:val="clear" w:fill="F2F2F2"/></w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, esc(line)))
	}
	d.buf.WriteString(b.String())
}

// Bullet 无序列表项。
func (d *Docx) Bullet(text string) {
	fmt.Fprintf(&d.buf, `<w:p><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, esc(text))
}

// Table 带表头的表格。rows 与 header 列数不一致时按最短补齐。
func (d *Docx) Table(header []string, rows [][]string) {
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid>`)
	for range header {
		b.WriteString(`<w:gridCol w:w="1800"/>`)
	}
	b.WriteString(`</w:tblGrid>`)
	cell := func(text string, head bool) {
		sty := ""
		if head {
			sty = `<w:shd w:val="clear" w:fill="DEEAF6"/>`
		}
		fmt.Fprintf(&b, `<w:tc><w:tcPr>%s</w:tcPr><w:p><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p></w:tc>`, sty, esc(text))
	}
	b.WriteString(`<w:tr>`)
	for _, h := range header {
		cell(h, true)
	}
	b.WriteString(`</w:tr>`)
	for _, r := range rows {
		b.WriteString(`<w:tr>`)
		for i := range header {
			v := ""
			if i < len(r) {
				v = r[i]
			}
			cell(v, false)
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	// OOXML 要求表格后跟一个空段落
	b.WriteString(`<w:p/>`)
	d.buf.WriteString(b.String())
}

// PageBreak 分页。
func (d *Docx) PageBreak() {
	d.buf.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
}

// Encode 打包成 .docx 字节流。
func (d *Docx) Encode(w io.Writer) error {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		d.buf.String() +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
</Types>`

	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

	documentRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`

	zw := zip.NewWriter(w)
	files := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rels},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + document},
		{"word/_rels/document.xml.rels", documentRels},
		{"word/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + docxStyles},
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return err
		}
		if _, err := io.Copy(fw, strings.NewReader(f.body)); err != nil {
			return err
		}
	}
	return zw.Close()
}

// docxStyles 内置中文报告样式:标题黑体分级、正文宋体小四、代码 Consolas、
// Heading1 大纲级别 0(Word 导航窗格可见 → 天然目录结构)。
const docxStyles = `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr>
<w:rFonts w:ascii="Calibri" w:eastAsia="宋体" w:hAnsi="Calibri"/>
<w:sz w:val="24"/><w:szCs w:val="24"/>
</w:rPr></w:rPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:styleId="Normal" w:default="1"><w:name w:val="Normal"/>
<w:pPr><w:spacing w:after="120"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/>
<w:pPr><w:outlineLvl w:val="0"/><w:spacing w:before="360" w:after="180"/></w:pPr>
<w:rPr><w:rFonts w:ascii="Calibri" w:eastAsia="黑体"/><w:b/><w:sz w:val="40"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/>
<w:pPr><w:outlineLvl w:val="1"/><w:spacing w:before="280" w:after="140"/></w:pPr>
<w:rPr><w:rFonts w:ascii="Calibri" w:eastAsia="黑体"/><w:b/><w:sz w:val="32"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/>
<w:pPr><w:outlineLvl w:val="2"/><w:spacing w:before="200" w:after="120"/></w:pPr>
<w:rPr><w:rFonts w:ascii="Calibri" w:eastAsia="黑体"/><w:b/><w:sz w:val="28"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/>
<w:rPr><w:rFonts w:ascii="Consolas" w:eastAsia="宋体" w:hAnsi="Consolas"/><w:sz w:val="18"/></w:rPr></w:style>
<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/>
<w:tblPr><w:tblBorders>
<w:top w:val="single" w:sz="4" w:color="999999"/><w:left w:val="single" w:sz="4" w:color="999999"/>
<w:bottom w:val="single" w:sz="4" w:color="999999"/><w:right w:val="single" w:sz="4" w:color="999999"/>
<w:insideH w:val="single" w:sz="4" w:color="999999"/><w:insideV w:val="single" w:sz="4" w:color="999999"/>
</w:tblBorders></w:tblPr></w:style>
</w:styles>`

// SeverityCN 严重度中文映射(报告用)。
func SeverityCN(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return "严重(critical)"
	case "high":
		return "高危(high)"
	case "medium":
		return "中危(medium)"
	case "low":
		return "低危(low)"
	default:
		return s
	}
}

// FormatTimeUTC 统一时间展示(UTC,报告里避免时区歧义)。
func FormatTimeUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}
