package customreport

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"
)

// Rendered is a finished document plus what to call it.
type Rendered struct {
	Filename    string
	ContentType string
	Body        []byte
}

// Doc is the resolved report: a spec, run, ready to render. Renderers see only
// this, so they never touch the database or the report service.
type Doc struct {
	Title      string
	Notes      string
	RangeLabel string
	Generated  time.Time
	Sections   []DocSection
	// Summary is the executive summary, written at render time from the data
	// this run produced. Empty for raw-data formats.
	Summary string
}

// DocSection is one section's results after column selection has been applied.
type DocSection struct {
	Title string
	// Commentary explains the table beneath it. Empty for raw-data formats.
	Commentary string
	Note       string
	Headers    []string
	Rows       [][]string
	// Numeric marks columns whose values are numbers, so Excel writes them as
	// numbers rather than text. Without it every figure arrives as a string and
	// the first thing the recipient does is retype the column to sum it —
	// which is most of the reason they asked for Excel instead of CSV.
	Numeric []bool
}

// Render dispatches to the format's renderer.
func Render(doc Doc, format Format) (Rendered, error) {
	base := safeFilename(doc.Title)
	switch format {
	case FormatXLSX:
		b, err := renderXLSX(doc)
		return Rendered{base + ".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", b}, err
	case FormatCSV:
		b, err := renderCSV(doc)
		return Rendered{base + ".csv", "text/csv; charset=utf-8", b}, err
	case FormatPDF:
		b, err := renderPDF(doc)
		return Rendered{base + ".pdf", "application/pdf", b}, err
	case FormatDOCX:
		b, err := renderDOCX(doc)
		return Rendered{base + ".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", b}, err
	default:
		return Rendered{}, fmt.Errorf("unknown format %q", format)
	}
}

// ---- xlsx -------------------------------------------------------------------

// renderXLSX writes one SHEET PER SECTION.
//
// Stacking sections onto one sheet would look like the PDF, but a spreadsheet
// is not read, it is filtered and pivoted — and neither works on a sheet with
// three header rows in the middle of it. One table per sheet is what makes the
// file usable rather than merely openable.
func renderXLSX(doc Doc) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	header, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F3864"}},
	})

	noteStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Italic: true, Color: "8A6D1B"},
	})
	titleStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})

	// A cover sheet, because a workbook of bare tables loses everything that
	// says what it IS — which report, over what window, generated when. PDF and
	// Word get that from their headers; a spreadsheet has nowhere else to put
	// it, and a file that arrives by email a week later needs it most.
	const cover = "Report"
	_ = f.SetSheetName("Sheet1", cover)
	_ = f.SetColWidth(cover, "A", "A", 18)
	_ = f.SetColWidth(cover, "B", "B", 70)
	_ = f.SetCellStr(cover, "A1", doc.Title)
	_ = f.SetCellStyle(cover, "A1", "A1", titleStyle)
	rowN := 3
	put := func(k, v string) {
		if v == "" {
			return
		}
		a, _ := excelize.CoordinatesToCellName(1, rowN)
		b, _ := excelize.CoordinatesToCellName(2, rowN)
		_ = f.SetCellStr(cover, a, k)
		_ = f.SetCellStr(cover, b, v)
		rowN++
	}
	put("Period", doc.RangeLabel)
	put("Generated", doc.Generated.Format("2 Jan 2006 15:04"))
	put("Notes", doc.Notes)
	put("Summary", doc.Summary)

	used := map[string]int{cover: 1}
	for i, sec := range doc.Sections {
		name := sheetName(sec.Title, i, used)
		if _, err := f.NewSheet(name); err != nil {
			return nil, fmt.Errorf("sheet %q: %w", name, err)
		}

		// A section note goes ABOVE the table, so the header stays the first
		// row of the data block. Notes carry the things that must not be
		// missed: a section that failed, a truncated row count, a column the
		// source could not provide.
		headerRow := 1
		if sec.Note != "" {
			_ = f.SetCellStr(name, "A1", sec.Note)
			_ = f.SetCellStyle(name, "A1", "A1", noteStyle)
			headerRow = 2
		}

		for c, h := range sec.Headers {
			cell, _ := excelize.CoordinatesToCellName(c+1, headerRow)
			_ = f.SetCellStr(name, cell, h)
			_ = f.SetCellStyle(name, cell, cell, header)
			// Roughly fit the header; excelize has no autofit.
			_ = f.SetColWidth(name, colLetter(c+1), colLetter(c+1), float64(clampInt(len(h)+4, 10, 40)))
		}
		for r, row := range sec.Rows {
			for c, v := range row {
				cell, _ := excelize.CoordinatesToCellName(c+1, headerRow+1+r)
				if c < len(sec.Numeric) && sec.Numeric[c] {
					if n, err := strconv.ParseFloat(v, 64); err == nil {
						_ = f.SetCellFloat(name, cell, n, -1, 64)
						continue
					}
				}
				_ = f.SetCellStr(name, cell, v)
			}
		}
		if len(sec.Headers) > 0 {
			first, _ := excelize.CoordinatesToCellName(1, headerRow)
			last, _ := excelize.CoordinatesToCellName(len(sec.Headers), headerRow)
			_ = f.AutoFilter(name, first+":"+last, nil)
			// Freeze the header so scrolling a long report keeps its meaning.
			top, _ := excelize.CoordinatesToCellName(1, headerRow+1)
			_ = f.SetPanes(name, &excelize.Panes{
				Freeze: true, YSplit: headerRow, TopLeftCell: top, ActivePane: "bottomLeft",
			})
		}
	}
	f.SetActiveSheet(0)

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sheetName makes a legal, unique Excel sheet name: <=31 chars, no []:*?/\ and
// unique within the workbook. Two sections called "Alerts" would otherwise
// fail the whole render.
func sheetName(title string, i int, used map[string]int) string {
	n := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(title))
	if n == "" {
		n = fmt.Sprintf("Section %d", i+1)
	}
	if len(n) > 31 {
		n = n[:31]
	}
	if used[n] == 0 {
		used[n] = 1
		return n
	}
	used[n]++
	suffix := fmt.Sprintf(" (%d)", used[n])
	if len(n)+len(suffix) > 31 {
		n = n[:31-len(suffix)]
	}
	return n + suffix
}

func colLetter(n int) string {
	s, _ := excelize.ColumnNumberToName(n)
	return s
}

// ---- csv --------------------------------------------------------------------

// renderCSV concatenates sections with a blank line and a title row between.
//
// CSV has no concept of sections, so this is a compromise rather than a design:
// a multi-section report is more honest as xlsx. Kept because it is the format
// that opens anywhere.
func renderCSV(doc Doc) ([]byte, error) {
	var buf bytes.Buffer
	// UTF-8 BOM: without it Excel mis-decodes non-ASCII names on Windows,
	// which is exactly where these files get opened.
	buf.WriteString("\uFEFF")
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{doc.Title})
	_ = w.Write([]string{doc.RangeLabel})
	if doc.Notes != "" {
		_ = w.Write(sanitiseRow([]string{doc.Notes}))
	}

	for _, sec := range doc.Sections {
		_ = w.Write([]string{})
		if len(doc.Sections) > 1 {
			_ = w.Write([]string{sec.Title})
		}
		// Notes matter more here than anywhere: they are how a failed or
		// truncated section explains itself, and CSV has no other channel.
		if sec.Note != "" {
			_ = w.Write(sanitiseRow([]string{sec.Note}))
		}
		_ = w.Write(sec.Headers)
		for _, row := range sec.Rows {
			_ = w.Write(sanitiseRow(row))
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// sanitiseRow defuses CSV injection.
//
// A cell beginning =, +, - or @ is executed as a formula when the file is
// opened in Excel, and these reports carry free text that ultimately came from
// user input (vehicle names, notes). Prefixing with a quote keeps the value
// visible while making it inert.
func sanitiseRow(row []string) []string {
	out := make([]string, len(row))
	for i, v := range row {
		if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
			v = "'" + v
		}
		out[i] = v
	}
	return out
}

// ---- pdf --------------------------------------------------------------------

const (
	pdfMargin  = 10.0
	pdfFontSz  = 8.0
	pdfRowH    = 5.0
	pdfMaxCols = 12
)

// renderPDF lays each section out as a table, landscape A4.
//
// Portrait was tried first and is wrong for this data: report rows are wide
// (plate, time, location, three numbers) and portrait forces either unreadable
// type or dropped columns.
func renderPDF(doc Doc) ([]byte, error) {
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(pdfMargin, pdfMargin, pdfMargin)
	pdf.SetAutoPageBreak(true, pdfMargin)
	pageW, _ := pdf.GetPageSize()
	usable := pageW - 2*pdfMargin

	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "I", 7)
		pdf.SetTextColor(120, 120, 120)
		pdf.CellFormat(0, 6, tr(fmt.Sprintf("%s  ·  generated %s  ·  page %d",
			doc.Title, doc.Generated.Format("2 Jan 2006 15:04"), pdf.PageNo())), "", 0, "C", false, 0, "")
	})

	pdf.AddPage()
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Helvetica", "B", 16)
	pdf.MultiCell(usable, 8, tr(doc.Title), "", "L", false)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(90, 90, 90)
	pdf.MultiCell(usable, 5, tr(doc.RangeLabel), "", "L", false)
	if doc.Notes != "" {
		pdf.Ln(1)
		pdf.MultiCell(usable, 5, tr(doc.Notes), "", "L", false)
	}
	if doc.Summary != "" {
		pdf.Ln(3)
		pdf.SetFont("Helvetica", "B", 10)
		pdf.SetTextColor(31, 56, 100)
		pdf.MultiCell(usable, 5, "Summary", "", "L", false)
		pdf.SetFont("Helvetica", "", 9.5)
		pdf.SetTextColor(40, 40, 40)
		pdf.MultiCell(usable, 5, tr(doc.Summary), "", "L", false)
	}
	pdf.Ln(3)

	for _, sec := range doc.Sections {
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("Helvetica", "B", 11)
		pdf.MultiCell(usable, 6, tr(sec.Title), "", "L", false)
		if sec.Commentary != "" {
			pdf.SetFont("Helvetica", "", 9)
			pdf.SetTextColor(40, 40, 40)
			pdf.MultiCell(usable, 4.5, tr(sec.Commentary), "", "L", false)
		}
		if sec.Note != "" {
			pdf.SetFont("Helvetica", "I", 8)
			pdf.SetTextColor(120, 120, 120)
			pdf.MultiCell(usable, 4, tr(sec.Note), "", "L", false)
		}
		pdf.Ln(1)

		if len(sec.Headers) == 0 || len(sec.Rows) == 0 {
			pdf.SetFont("Helvetica", "I", 9)
			pdf.SetTextColor(120, 120, 120)
			pdf.MultiCell(usable, 5, "No data for this period.", "", "L", false)
			pdf.Ln(4)
			continue
		}

		// A page cannot hold an unbounded number of columns legibly. Dropping
		// the tail is stated in the document rather than done silently — a
		// report that quietly omits a column is worse than one that says so.
		headers, rows, dropped := limitColumns(sec, pdfMaxCols)
		widths := columnWidths(headers, rows, usable)

		drawHeader := func() {
			pdf.SetFont("Helvetica", "B", pdfFontSz)
			pdf.SetFillColor(31, 56, 100)
			pdf.SetTextColor(255, 255, 255)
			for i, h := range headers {
				pdf.CellFormat(widths[i], pdfRowH+1, clip(tr(h), widths[i], pdf), "1", 0, "L", true, 0, "")
			}
			pdf.Ln(-1)
			pdf.SetFont("Helvetica", "", pdfFontSz)
			pdf.SetTextColor(0, 0, 0)
		}
		drawHeader()

		fill := false
		for _, row := range rows {
			// Repeat the header after a page break, or the continuation page
			// is a wall of unlabelled numbers.
			if pdf.GetY() > 190 {
				pdf.AddPage()
				drawHeader()
			}
			pdf.SetFillColor(245, 247, 250)
			for i := range headers {
				v := ""
				if i < len(row) {
					v = row[i]
				}
				pdf.CellFormat(widths[i], pdfRowH, clip(tr(v), widths[i], pdf), "1", 0, "L", fill, 0, "")
			}
			pdf.Ln(-1)
			fill = !fill
		}
		if dropped > 0 {
			pdf.SetFont("Helvetica", "I", 7)
			pdf.SetTextColor(150, 90, 0)
			pdf.MultiCell(usable, 4, fmt.Sprintf("%d further column(s) omitted to fit the page — export as Excel for the complete table.", dropped), "", "L", false)
		}
		pdf.Ln(5)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func limitColumns(sec DocSection, max int) ([]string, [][]string, int) {
	if len(sec.Headers) <= max {
		return sec.Headers, sec.Rows, 0
	}
	rows := make([][]string, len(sec.Rows))
	for i, r := range sec.Rows {
		if len(r) > max {
			r = r[:max]
		}
		rows[i] = r
	}
	return sec.Headers[:max], rows, len(sec.Headers) - max
}

// columnWidths distributes the page by each column's widest content, so a
// column of timestamps is not given the same space as one of "Y"/"N".
func columnWidths(headers []string, rows [][]string, usable float64) []float64 {
	weights := make([]float64, len(headers))
	total := 0.0
	for i, h := range headers {
		w := float64(len(h))
		// Sampling beats scanning every row: a 20k-row report would otherwise
		// pay a full pass just to pick widths.
		for r := 0; r < len(rows) && r < 200; r++ {
			if i < len(rows[r]) && float64(len(rows[r][i])) > w {
				w = float64(len(rows[r][i]))
			}
		}
		if w > 28 {
			w = 28 // one long address must not squeeze everything else out
		}
		weights[i] = w
		total += w
	}
	out := make([]float64, len(headers))
	for i, w := range weights {
		out[i] = usable * w / total
	}
	return out
}

// clip shortens a value to fit its cell, with an ellipsis.
func clip(s string, w float64, pdf *fpdf.Fpdf) string {
	if pdf.GetStringWidth(s) <= w-2 {
		return s
	}
	for len(s) > 1 {
		s = s[:len(s)-1]
		if pdf.GetStringWidth(s+"…") <= w-2 {
			return s + "…"
		}
	}
	return s
}

// tr maps text into what fpdf's core fonts can actually encode (Latin-1).
//
// The alternative is embedding a Unicode TTF, which means shipping a font file
// and its licence for the sake of an export. Indonesian is Latin script, so the
// practical loss is typographic punctuation — and an em dash rendered as a
// hyphen is far better than the mojibake you get by not converting at all.
//
// Anything outside Latin-1 becomes '?' rather than being dropped: a visible
// placeholder tells the reader a character was lost, whereas silent removal
// turns a name into a subtly different name.
var pdfPunct = strings.NewReplacer(
	"—", "-", "–", "-", "…", "...", "’", "'", "‘", "'",
	"“", `"`, "”", `"`, "·", "-", "\u00a0", " ",
)

func tr(s string) string {
	s = pdfPunct.Replace(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20:
			// Control characters arrive from device metadata and corrupt the
			// content stream rather than merely looking wrong.
		case r < 0x100:
			b.WriteRune(r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// ---- docx -------------------------------------------------------------------

// renderDOCX writes the OOXML by hand.
//
// The mature Go library for this (unioffice) is AGPL/commercial, which is not
// something to take on for a report exporter. A .docx is a zip of a few XML
// parts, and a document that is headings plus tables needs only a small subset
// of WordprocessingML — so this writes that subset directly. It opens in Word,
// LibreOffice and Google Docs.
func renderDOCX(doc Doc) ([]byte, error) {
	var body strings.Builder
	body.WriteString(para(doc.Title, "Title", true, 32))
	body.WriteString(para(doc.RangeLabel, "", false, 20))
	if doc.Notes != "" {
		body.WriteString(para(doc.Notes, "", false, 20))
	}
	if doc.Summary != "" {
		body.WriteString(para("Summary", "", true, 24))
		body.WriteString(para(doc.Summary, "", false, 20))
	}

	for _, sec := range doc.Sections {
		body.WriteString(para(sec.Title, "", true, 26))
		if sec.Commentary != "" {
			body.WriteString(para(sec.Commentary, "", false, 20))
		}
		if sec.Note != "" {
			body.WriteString(para(sec.Note, "", false, 18))
		}
		if len(sec.Headers) == 0 || len(sec.Rows) == 0 {
			body.WriteString(para("No data for this period.", "", false, 20))
			continue
		}
		body.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="5000" w:type="pct"/>`)
		body.WriteString(`<w:tblBorders>`)
		for _, e := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
			body.WriteString(fmt.Sprintf(`<w:%s w:val="single" w:sz="4" w:color="BFBFBF"/>`, e))
		}
		body.WriteString(`</w:tblBorders></w:tblPr>`)

		body.WriteString(`<w:tr>`)
		for _, h := range sec.Headers {
			body.WriteString(`<w:tc><w:tcPr><w:shd w:val="clear" w:fill="1F3864"/></w:tcPr>` +
				runPara(h, true, 16, "FFFFFF") + `</w:tc>`)
		}
		body.WriteString(`</w:tr>`)

		for _, row := range sec.Rows {
			body.WriteString(`<w:tr>`)
			for i := range sec.Headers {
				v := ""
				if i < len(row) {
					v = row[i]
				}
				body.WriteString(`<w:tc>` + runPara(v, false, 16, "") + `</w:tc>`)
			}
			body.WriteString(`</w:tr>`)
		}
		body.WriteString(`</w:tbl>`)
		body.WriteString(para("", "", false, 20)) // spacer between tables
	}

	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>` + body.String() + `<w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/></w:sectPr></w:body></w:document>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`},
		{"word/document.xml", document},
	}
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(p.content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func para(text, style string, bold bool, halfPt int) string {
	var pr string
	if style != "" {
		pr = fmt.Sprintf(`<w:pStyle w:val="%s"/>`, style)
	}
	return `<w:p><w:pPr>` + pr + `</w:pPr>` + run(text, bold, halfPt, "") + `</w:p>`
}

func runPara(text string, bold bool, halfPt int, color string) string {
	return `<w:p>` + run(text, bold, halfPt, color) + `</w:p>`
}

func run(text string, bold bool, halfPt int, color string) string {
	var rpr strings.Builder
	rpr.WriteString(`<w:rPr>`)
	if bold {
		rpr.WriteString(`<w:b/>`)
	}
	rpr.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/>`, halfPt))
	if color != "" {
		rpr.WriteString(fmt.Sprintf(`<w:color w:val="%s"/>`, color))
	}
	rpr.WriteString(`</w:rPr>`)
	// xml.EscapeText, not manual replacement: report values carry &, < and
	// stray control characters from device metadata, any one of which makes
	// the document unopenable rather than merely ugly.
	var esc bytes.Buffer
	_ = xml.EscapeText(&esc, []byte(text))
	return `<w:r>` + rpr.String() + `<w:t xml:space="preserve">` + esc.String() + `</w:t></w:r>`
}

// ---- shared -----------------------------------------------------------------

// safeFilename keeps a downloaded file's name recognisable without letting a
// report title steer the Content-Disposition header.
func safeFilename(title string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == ' ', r == '-', r == '_':
			return '-'
		default:
			return -1
		}
	}, title)
	out = strings.Trim(out, "-")
	if out == "" {
		out = "report"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
