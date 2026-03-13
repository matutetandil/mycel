package pdf

import (
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
	"golang.org/x/net/html"
)

// renderer converts an HTML subset to PDF using fpdf.
//
// Supported HTML:
//   - h1-h6: headings with decreasing font sizes
//   - p: paragraphs
//   - strong/b: bold text
//   - em/i: italic text
//   - table/thead/tbody/tr/th/td: tables with borders
//   - hr: horizontal rule
//   - br: line break
//   - ul/ol/li: lists (bulleted/numbered)
//   - img: images (local files only)
//   - div: container
//
// Supported inline styles (via style attribute):
//   - text-align: left, center, right
//   - font-size: NNpx
//   - color: #RRGGBB
//   - background-color: #RRGGBB
type renderer struct {
	pdf    *fpdf.Fpdf
	config *Config
	width  float64 // usable page width (after margins)
}

func newRenderer(config *Config) *renderer {
	orientation := "P"
	size := config.PageSize

	pdf := fpdf.New(orientation, "mm", size, "")
	pdf.SetMargins(config.MarginLeft, config.MarginTop, config.MarginRight)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()
	pdf.SetFont(config.Font, "", 12)

	w, _ := pdf.GetPageSize()
	usable := w - config.MarginLeft - config.MarginRight

	return &renderer{pdf: pdf, config: config, width: usable}
}

func (r *renderer) render(htmlStr string) error {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return fmt.Errorf("html parse error: %w", err)
	}
	r.walkNode(doc)
	return nil
}

func (r *renderer) walkNode(n *html.Node) {
	if n.Type == html.ElementNode {
		r.handleElement(n)
		return
	}

	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			r.pdf.Write(r.lineHeight(), text)
		}
		return
	}

	// Walk children for non-element nodes (document, etc.)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.walkNode(c)
	}
}

func (r *renderer) handleElement(n *html.Node) {
	tag := strings.ToLower(n.Data)
	style := parseStyle(getAttr(n, "style"))

	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		r.renderHeading(n, tag, style)
	case "p":
		r.renderParagraph(n, style)
	case "strong", "b":
		r.renderInline(n, "B")
	case "em", "i":
		r.renderInline(n, "I")
	case "table":
		r.renderTable(n, style)
	case "hr":
		r.renderHR()
	case "br":
		r.pdf.Ln(r.lineHeight())
	case "ul":
		r.renderList(n, false)
	case "ol":
		r.renderList(n, true)
	case "img":
		r.renderImage(n)
	case "div":
		r.renderDiv(n, style)
	case "html", "head", "body", "thead", "tbody", "title", "meta", "link":
		// Walk through structural tags
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			r.walkNode(c)
		}
	default:
		// Unknown tag — just render children
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			r.walkNode(c)
		}
	}
}

func (r *renderer) renderHeading(n *html.Node, tag string, style cssStyle) {
	sizes := map[string]float64{
		"h1": 24, "h2": 20, "h3": 16, "h4": 14, "h5": 12, "h6": 10,
	}
	size := sizes[tag]
	if style.fontSize > 0 {
		size = style.fontSize
	}

	r.pdf.Ln(4)
	r.pdf.SetFont(r.config.Font, "B", size)
	r.applyColor(style)
	align := r.alignStr(style.textAlign)

	text := r.collectText(n)
	r.pdf.CellFormat(r.width, size*0.5, text, "", 1, align, false, 0, "")

	r.resetFont()
	r.pdf.Ln(2)
}

func (r *renderer) renderParagraph(n *html.Node, style cssStyle) {
	r.pdf.Ln(2)

	if style.fontSize > 0 {
		r.pdf.SetFontSize(style.fontSize)
	}
	r.applyColor(style)
	align := r.alignStr(style.textAlign)

	if align != "L" {
		// For aligned paragraphs, collect full text
		text := r.collectText(n)
		r.pdf.CellFormat(r.width, r.lineHeight(), text, "", 1, align, false, 0, "")
	} else {
		// Normal paragraph — walk children for inline formatting
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			r.walkNode(c)
		}
	}

	r.resetFont()
	r.pdf.Ln(2)
}

func (r *renderer) renderInline(n *html.Node, styleFlag string) {
	_, origStyle := r.pdf.GetFontSize()
	r.pdf.SetFont(r.config.Font, styleFlag, origStyle)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.walkNode(c)
	}
	r.pdf.SetFont(r.config.Font, "", origStyle)
}

func (r *renderer) renderHR() {
	r.pdf.Ln(3)
	x := r.pdf.GetX()
	y := r.pdf.GetY()
	r.pdf.SetDrawColor(180, 180, 180)
	r.pdf.Line(x, y, x+r.width, y)
	r.pdf.SetDrawColor(0, 0, 0)
	r.pdf.Ln(3)
}

func (r *renderer) renderList(n *html.Node, ordered bool) {
	r.pdf.Ln(2)
	idx := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && strings.ToLower(c.Data) == "li" {
			idx++
			text := r.collectText(c)
			var bullet string
			if ordered {
				bullet = fmt.Sprintf("%d. ", idx)
			} else {
				bullet = "• "
			}
			r.pdf.CellFormat(8, r.lineHeight(), "", "", 0, "", false, 0, "")
			r.pdf.Write(r.lineHeight(), bullet+text)
			r.pdf.Ln(r.lineHeight())
		}
	}
	r.pdf.Ln(2)
}

func (r *renderer) renderImage(n *html.Node) {
	src := getAttr(n, "src")
	if src == "" {
		return
	}

	// Only local files supported
	opts := fpdf.ImageOptions{ReadDpi: true}
	r.pdf.ImageOptions(src, r.pdf.GetX(), r.pdf.GetY(), 0, 0, true, opts, 0, "")
}

func (r *renderer) renderDiv(n *html.Node, style cssStyle) {
	if style.fontSize > 0 {
		r.pdf.SetFontSize(style.fontSize)
	}
	r.applyColor(style)

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.walkNode(c)
	}

	r.resetFont()
}

// --- Table rendering ---

func (r *renderer) renderTable(n *html.Node, _ cssStyle) {
	r.pdf.Ln(2)

	// Collect all rows (from thead and tbody)
	var rows []tableRow
	r.collectTableRows(n, &rows)

	if len(rows) == 0 {
		return
	}

	// Calculate column widths (equal distribution)
	maxCols := 0
	for _, row := range rows {
		if len(row.cells) > maxCols {
			maxCols = len(row.cells)
		}
	}
	if maxCols == 0 {
		return
	}
	colWidth := r.width / float64(maxCols)

	// Render rows
	for _, row := range rows {
		for i, cell := range row.cells {
			if i >= maxCols {
				break
			}
			style := ""
			if cell.isHeader {
				style = "B"
			}
			r.pdf.SetFont(r.config.Font, style, 10)

			// Draw cell with border
			fill := cell.isHeader
			if fill {
				r.pdf.SetFillColor(230, 230, 230)
			}
			r.pdf.CellFormat(colWidth, 7, cell.text, "1", 0, "L", fill, 0, "")
		}
		r.pdf.Ln(-1)
	}

	r.resetFont()
	r.pdf.Ln(2)
}

type tableRow struct {
	cells []tableCell
}

type tableCell struct {
	text     string
	isHeader bool
}

func (r *renderer) collectTableRows(n *html.Node, rows *[]tableRow) {
	if n.Type == html.ElementNode {
		tag := strings.ToLower(n.Data)
		if tag == "tr" {
			row := tableRow{}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode {
					cellTag := strings.ToLower(c.Data)
					if cellTag == "td" || cellTag == "th" {
						row.cells = append(row.cells, tableCell{
							text:     r.collectText(c),
							isHeader: cellTag == "th",
						})
					}
				}
			}
			if len(row.cells) > 0 {
				*rows = append(*rows, row)
			}
			return
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.collectTableRows(c, rows)
	}
}

// --- Helpers ---

func (r *renderer) collectText(n *html.Node) string {
	var sb strings.Builder
	r.collectTextRecursive(n, &sb)
	return strings.TrimSpace(sb.String())
}

func (r *renderer) collectTextRecursive(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.collectTextRecursive(c, sb)
	}
}

func (r *renderer) lineHeight() float64 {
	return 6.0
}

func (r *renderer) resetFont() {
	r.pdf.SetFont(r.config.Font, "", 12)
	r.pdf.SetTextColor(0, 0, 0)
}

func (r *renderer) applyColor(style cssStyle) {
	if style.color != "" {
		cr, cg, cb := parseHexColor(style.color)
		r.pdf.SetTextColor(cr, cg, cb)
	}
}

func (r *renderer) alignStr(align string) string {
	switch strings.ToLower(align) {
	case "center":
		return "C"
	case "right":
		return "R"
	default:
		return "L"
	}
}

// getAttr returns the value of an HTML attribute.
func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.ToLower(a.Key) == key {
			return a.Val
		}
	}
	return ""
}

// --- CSS style parsing ---

type cssStyle struct {
	textAlign       string
	fontSize        float64
	color           string
	backgroundColor string
}

func parseStyle(s string) cssStyle {
	style := cssStyle{}
	if s == "" {
		return style
	}

	for _, decl := range strings.Split(s, ";") {
		decl = strings.TrimSpace(decl)
		parts := strings.SplitN(decl, ":", 2)
		if len(parts) != 2 {
			continue
		}
		prop := strings.TrimSpace(strings.ToLower(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch prop {
		case "text-align":
			style.textAlign = val
		case "font-size":
			style.fontSize = parsePx(val)
		case "color":
			style.color = val
		case "background-color":
			style.backgroundColor = val
		}
	}
	return style
}

// parsePx extracts a numeric value from "16px" or "1.5em".
func parsePx(s string) float64 {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSuffix(s, "px")
	s = strings.TrimSuffix(s, "pt")
	s = strings.TrimSuffix(s, "em")

	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v
}

// parseHexColor parses "#RRGGBB" to RGB values.
func parseHexColor(s string) (int, int, int) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, 0, 0
	}
	var r, g, b int
	fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}
