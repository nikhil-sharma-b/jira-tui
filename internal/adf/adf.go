// Package adf renders Atlassian Document Format into styled terminal lines.
//
// It is a pure function of its input: no I/O, no dependency on the Jira
// client, no TUI types. That makes it testable directly on documents captured
// from real tickets, which is the only credible source -- nobody hand-writes a
// convincing ADF table or a correctly nested mixed list.
package adf

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// Node is one ADF tree node. The format is open-ended, so unknown types are
// expected rather than exceptional.
type Node struct {
	Type    string          `json:"type"`
	Text    string          `json:"text,omitempty"`
	Attrs   map[string]any  `json:"attrs,omitempty"`
	Marks   []Mark          `json:"marks,omitempty"`
	Content []Node          `json:"content,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

type Mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Options controls rendering. Width drives wrapping and table layout.
type Options struct {
	Width int
	// Plain disables styling, for tests and for piping.
	Plain bool
}

// Supported node types render with real structure. Everything else falls
// through to ExtractText: unknown constructs lose their formatting but never
// their content, and never render as an error marker.
//
// media, expand and inlineCard degrade deliberately to one-line placeholders
// rather than being extracted, because their text content is not the point.
var supported = map[string]bool{
	"doc": true, "paragraph": true, "text": true, "heading": true,
	"bulletList": true, "orderedList": true, "listItem": true,
	"codeBlock": true, "blockquote": true, "rule": true, "hardBreak": true,
	"panel": true, "table": true, "tableRow": true, "tableCell": true,
	"tableHeader": true, "mention": true, "status": true, "emoji": true,
	"date": true,
}

var styledRenderer = func() *lipgloss.Renderer {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.ANSI256)
	return renderer
}()

// Render walks doc and returns styled lines ready for a viewport.
func Render(doc []byte, opts Options) ([]string, error) {
	root, err := decodeDocument(doc)
	if err != nil {
		return nil, err
	}
	return renderBlocks(root.Content, opts), nil
}

func decodeDocument(doc []byte) (Node, error) {
	if len(strings.TrimSpace(string(doc))) == 0 {
		return Node{}, nil
	}
	var root Node
	if err := json.Unmarshal(doc, &root); err != nil {
		return Node{}, err
	}
	return root, nil
}

func renderBlocks(nodes []Node, opts Options) []string {
	var lines []string
	for _, n := range nodes {
		block := renderBlock(n, opts)
		if len(block) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, block...)
	}
	return lines
}

func renderBlock(n Node, opts Options) []string {
	if placeholder, ok := nodePlaceholder(n); ok {
		return placeholderLine(placeholder, opts.Width)
	}
	if n.Type == "mediaSingle" || n.Type == "mediaGroup" {
		var lines []string
		for _, child := range n.Content {
			if placeholder, ok := nodePlaceholder(child); ok {
				lines = append(lines, placeholderLine(placeholder, opts.Width)...)
			}
		}
		return lines
	}
	if !supported[n.Type] {
		return wrap(ExtractText(n), opts.Width)
	}

	switch n.Type {
	case "doc":
		return renderBlocks(n.Content, opts)
	case "paragraph":
		return wrap(renderInline(n.Content, opts.Plain, opts.Width), opts.Width)
	case "heading":
		level := intAttr(n.Attrs, "level", 1)
		if level < 1 || level > 6 {
			level = 1
		}
		prefix := strings.Repeat("#", level) + " "
		inlineWidth := opts.Width - lipgloss.Width(prefix)
		return prefixFirst(wrap(renderInline(n.Content, opts.Plain, inlineWidth), inlineWidth), prefix, opts.Width)
	case "bulletList", "orderedList":
		return renderList(n, opts)
	case "codeBlock":
		text := ExtractText(n)
		parts := strings.Split(text, "\n")
		for i := range parts {
			parts[i] = "    " + parts[i]
		}
		return parts
	case "blockquote":
		return prefixEvery(renderBlocks(n.Content, Options{Width: opts.Width - 2, Plain: opts.Plain}), "> ", opts.Width)
	case "panel":
		label := "[" + strings.ToUpper(stringAttr(n.Attrs, "panelType")) + "] "
		if label == "[] " {
			label = "[PANEL] "
		}
		return prefixFirst(renderBlocks(n.Content, Options{Width: opts.Width - lipgloss.Width(label), Plain: opts.Plain}), label, opts.Width)
	case "table":
		return renderTable(n, opts)
	case "rule":
		width := opts.Width
		if width <= 0 {
			width = 20
		}
		return []string{strings.Repeat("─", width)}
	default:
		return wrap(ExtractText(n), opts.Width)
	}
}

func renderTable(table Node, opts Options) []string {
	rows := make([][]string, 0, len(table.Content))
	header := false
	columns := 0
	for rowIndex, row := range table.Content {
		var cells []string
		rowIsHeader := len(row.Content) > 0
		for _, cell := range row.Content {
			rowIsHeader = rowIsHeader && cell.Type == "tableHeader"
			var parts []string
			for _, line := range renderBlocks(cell.Content, Options{Plain: opts.Plain}) {
				if strings.TrimSpace(ansi.Strip(line)) != "" {
					parts = append(parts, line)
				}
			}
			cells = append(cells, strings.Join(parts, " / "))
		}
		if rowIndex == 0 {
			header = rowIsHeader
		}
		columns = max(columns, len(cells))
		rows = append(rows, cells)
	}
	if columns == 0 {
		return nil
	}

	widths := make([]int, columns)
	for _, row := range rows {
		for column, cell := range row {
			widths[column] = max(widths[column], ansi.StringWidth(cell))
		}
	}
	tableWidth := 1 + 3*columns
	for _, width := range widths {
		tableWidth += width
	}
	if opts.Width > 0 && tableWidth > opts.Width {
		return renderStackedTable(rows, header, opts.Width)
	}

	var lines []string
	lines = append(lines, tableBorder("┌", "┬", "┐", widths))
	for rowIndex, row := range rows {
		var line strings.Builder
		line.WriteString("│")
		for column := range columns {
			cell := ""
			if column < len(row) {
				cell = row[column]
			}
			if rowIndex == 0 && header && !opts.Plain {
				cell = ansiStyle().Bold(true).Render(cell)
			}
			line.WriteString(" " + padRight(cell, widths[column]) + " │")
		}
		lines = append(lines, line.String())
		if rowIndex < len(rows)-1 {
			lines = append(lines, tableBorder("├", "┼", "┤", widths))
		}
	}
	lines = append(lines, tableBorder("└", "┴", "┘", widths))
	return lines
}

func renderStackedTable(rows [][]string, header bool, width int) []string {
	labels := make([]string, 0, len(rows[0]))
	start := 0
	if header {
		for _, cell := range rows[0] {
			labels = append(labels, strings.TrimSpace(ansi.Strip(cell)))
		}
		start = 1
	} else {
		for column := range len(rows[0]) {
			labels = append(labels, fmt.Sprintf("Column %d", column+1))
		}
	}

	var lines []string
	for rowIndex := start; rowIndex < len(rows); rowIndex++ {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		for column, value := range rows[rowIndex] {
			label := fmt.Sprintf("Column %d", column+1)
			if column < len(labels) && labels[column] != "" {
				label = labels[column]
			}
			prefix := label + ": "
			lines = append(lines, prefixFirst(wrap(value, width-ansi.StringWidth(prefix)), prefix, width)...)
		}
	}
	return lines
}

func tableBorder(left, middle, right string, widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		parts[i] = strings.Repeat("─", width+2)
	}
	return left + strings.Join(parts, middle) + right
}

func padRight(value string, width int) string {
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}

func renderList(list Node, opts Options) []string {
	start := intAttr(list.Attrs, "order", 1)
	if start < 1 {
		start = 1
	}

	var lines []string
	for i, item := range list.Content {
		prefix := "• "
		if list.Type == "orderedList" {
			prefix = fmt.Sprintf("%d. ", start+i)
		}
		prefixWidth := lipgloss.Width(prefix)
		itemOpts := Options{Width: opts.Width - prefixWidth, Plain: opts.Plain}

		var itemLines []string
		for contentIndex, child := range item.Content {
			childLines := renderBlock(child, itemOpts)
			if contentIndex == 0 {
				itemLines = append(itemLines, prefixFirst(childLines, prefix, opts.Width)...)
				continue
			}
			itemLines = append(itemLines, prefixEvery(childLines, strings.Repeat(" ", prefixWidth), opts.Width)...)
		}
		lines = append(lines, itemLines...)
	}
	return lines
}

// ExtractText recovers plain text from any node, known or not. This is the
// fallback that guarantees content is never silently dropped.
func ExtractText(n Node) string {
	switch n.Type {
	case "text":
		return n.Text
	case "hardBreak":
		return "\n"
	case "mention":
		return stringAttr(n.Attrs, "text")
	case "status":
		return "[" + strings.ToUpper(stringAttr(n.Attrs, "text")) + "]"
	case "emoji":
		if text := stringAttr(n.Attrs, "text"); text != "" {
			return text
		}
		return stringAttr(n.Attrs, "shortName")
	case "date":
		return renderDate(stringAttr(n.Attrs, "timestamp"))
	}
	var text strings.Builder
	for _, child := range n.Content {
		if text.Len() > 0 && isBlockNode(child.Type) && !strings.HasSuffix(text.String(), "\n") {
			text.WriteByte('\n')
		}
		text.WriteString(ExtractText(child))
	}
	return text.String()
}

func isBlockNode(nodeType string) bool {
	switch nodeType {
	case "blockquote", "bulletList", "codeBlock", "heading", "listItem",
		"orderedList", "panel", "paragraph", "rule", "table", "tableCell",
		"tableHeader", "tableRow":
		return true
	default:
		return false
	}
}

func renderInline(nodes []Node, plain bool, width int) string {
	var text strings.Builder
	for _, n := range nodes {
		value, placeholder := nodePlaceholder(n)
		if !placeholder {
			value = ExtractText(n)
		} else {
			value = atomicPlaceholder(value, width)
		}
		if n.Type == "text" {
			var link string
			style := ansiStyle()
			for _, mark := range n.Marks {
				switch mark.Type {
				case "strong":
					style = style.Bold(true)
				case "em":
					style = style.Italic(true)
				case "code":
					style = style.Reverse(true)
				case "strike":
					style = style.Strikethrough(true)
				case "link":
					style = style.Underline(true)
					link = stringAttr(mark.Attrs, "href")
				}
			}
			if !plain {
				value = style.Render(value)
			}
			if link != "" && strings.TrimSpace(n.Text) != link {
				value += " (" + link + ")"
			}
		} else if !plain {
			switch n.Type {
			case "mention":
				value = ansiStyle().Bold(true).Foreground(lipgloss.Color("39")).Render(value)
			case "status":
				value = ansiStyle().Bold(true).Reverse(true).Render(value)
			}
		}
		text.WriteString(value)
	}
	return text.String()
}

func atomicPlaceholder(placeholder string, width int) string {
	if width > 0 {
		placeholder = ansi.Truncate(placeholder, width, "…")
	}
	return strings.ReplaceAll(placeholder, " ", "\u00a0")
}

func nodePlaceholder(n Node) (string, bool) {
	switch n.Type {
	case "expand", "nestedExpand":
		title := stringAttr(n.Attrs, "title")
		if title == "" {
			title = "untitled"
		}
		return "[expand: " + title + "]", true
	case "inlineCard", "blockCard", "embedCard":
		url := stringAttr(n.Attrs, "url")
		if url == "" {
			url = "unavailable"
		}
		return "[card: " + url + "]", true
	case "media", "mediaInline":
		filename := mediaFilename(n)
		if stringAttr(n.Attrs, "type") == "external" {
			return "[external media: " + filename + "]", true
		}
		if isAttachment(n) {
			return "[attachment: " + filename + "]", true
		}
		return "[media: " + filename + "; Media Services access required]", true
	default:
		return "", false
	}
}

func placeholderLine(placeholder string, width int) []string {
	if width <= 0 {
		return []string{placeholder}
	}
	return []string{ansi.Truncate(placeholder, width, "…")}
}

func isAttachment(n Node) bool {
	return stringAttr(n.Attrs, "type") != "external" && stringAttr(n.Attrs, "collection") == ""
}

func mediaFilename(n Node) string {
	for _, name := range []string{"alt", "fileName", "filename"} {
		if filename := stringAttr(n.Attrs, name); filename != "" {
			return filename
		}
	}
	if id := stringAttr(n.Attrs, "id"); id != "" {
		return id
	}
	return "unnamed"
}

func ansiStyle() lipgloss.Style {
	return styledRenderer.NewStyle()
}

func renderDate(timestamp string) string {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return timestamp
	}
	if seconds > 100_000_000_000 {
		seconds /= 1000
	}
	return time.Unix(seconds, 0).UTC().Format(time.DateOnly)
}

func intAttr(attrs map[string]any, name string, fallback int) int {
	value, ok := attrs[name].(float64)
	if !ok {
		return fallback
	}
	return int(value)
}

func stringAttr(attrs map[string]any, name string) string {
	value, _ := attrs[name].(string)
	return value
}

func prefixFirst(lines []string, prefix string, width int) []string {
	if len(lines) == 0 {
		return nil
	}
	prefixWidth := lipgloss.Width(prefix)
	if width > 0 && prefixWidth >= width {
		return wrap(prefix+strings.Join(lines, "\n"), width)
	}
	continuation := strings.Repeat(" ", prefixWidth)
	for i := range lines {
		if i == 0 {
			lines[i] = prefix + lines[i]
		} else {
			lines[i] = continuation + lines[i]
		}
	}
	return lines
}

func prefixEvery(lines []string, prefix string, width int) []string {
	var prefixed []string
	for _, line := range lines {
		if line == "" {
			blank := strings.TrimRight(prefix, " ")
			if width > 0 {
				blank = ansi.Truncate(blank, width, "")
			}
			prefixed = append(prefixed, blank)
			continue
		}
		if width > 0 && lipgloss.Width(prefix) >= width {
			linePrefix := prefix
			if strings.TrimSpace(prefix) == "" {
				linePrefix = ""
			}
			prefixed = append(prefixed, wrap(linePrefix+line, width)...)
			continue
		}
		prefixed = append(prefixed, prefix+line)
	}
	return prefixed
}

func wrap(text string, width int) []string {
	if text == "" {
		return nil
	}
	if width <= 0 {
		return restorePlaceholderSpaces(strings.Split(text, "\n"))
	}
	wrapped := ansi.Wordwrap(text, width, " ")
	wrapped = ansi.Hardwrap(wrapped, width, false)
	return restorePlaceholderSpaces(containStyles(strings.Split(wrapped, "\n")))
}

func restorePlaceholderSpaces(lines []string) []string {
	for i := range lines {
		lines[i] = strings.ReplaceAll(lines[i], "\u00a0", " ")
	}
	return lines
}

func containStyles(lines []string) []string {
	active := ""
	for lineIndex, line := range lines {
		carry := active
		for offset := 0; offset < len(line); {
			start := strings.Index(line[offset:], "\x1b[")
			if start < 0 {
				break
			}
			start += offset
			end := strings.IndexByte(line[start:], 'm')
			if end < 0 {
				break
			}
			end += start
			sequence := line[start : end+1]
			if sequence == "\x1b[0m" || sequence == "\x1b[m" {
				active = ""
			} else {
				active += sequence
			}
			offset = end + 1
		}
		if carry != "" {
			line = carry + line
		}
		if active != "" {
			line += "\x1b[0m"
		}
		lines[lineIndex] = line
	}
	return lines
}

// Media describes an image or file referenced from a document. Ordinary
// attachments resolve through the attachment endpoint; items embedded via
// Atlassian's Media Services API need a separate token exchange and are
// reported as a known limitation instead of failing opaquely.
type Media struct {
	ID           string
	Filename     string
	IsAttachment bool
}

// MediaNodes lists the media referenced by a document, so the UI can offer
// them as focusable, openable items alongside attachments.
func MediaNodes(doc []byte) ([]Media, error) {
	root, err := decodeDocument(doc)
	if err != nil {
		return nil, err
	}

	var media []Media
	var walk func(Node)
	walk = func(n Node) {
		if n.Type == "media" || n.Type == "mediaInline" {
			media = append(media, Media{
				ID:           stringAttr(n.Attrs, "id"),
				Filename:     mediaFilename(n),
				IsAttachment: isAttachment(n),
			})
		}
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(root)
	return media, nil
}
