package adf

import (
	"fmt"
	"strings"
)

// WikiMarkup converts a v3 ADF description into Jira v2 wiki markup for
// editing. It preserves the document structures that jt renders instead of
// sending terminal glyphs and ANSI-oriented layout back to Jira.
func WikiMarkup(doc []byte) (string, error) {
	root, err := decodeDocument(doc)
	if err != nil {
		return "", err
	}
	return strings.Join(wikiBlocks(root.Content), "\n\n"), nil
}

func wikiBlocks(nodes []Node) []string {
	blocks := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if block := wikiBlock(node); block != "" {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func wikiBlock(node Node) string {
	if placeholder, ok := nodePlaceholder(node); ok {
		return placeholder
	}
	switch node.Type {
	case "doc":
		return strings.Join(wikiBlocks(node.Content), "\n\n")
	case "paragraph":
		return wikiInline(node.Content)
	case "heading":
		level := intAttr(node.Attrs, "level", 1)
		if level < 1 || level > 6 {
			level = 1
		}
		return fmt.Sprintf("h%d. %s", level, wikiInline(node.Content))
	case "bulletList":
		return wikiList(node, "")
	case "orderedList":
		return wikiList(node, "")
	case "codeBlock":
		language := stringAttr(node.Attrs, "language")
		opening := "{code}"
		if language != "" {
			opening = "{code:" + language + "}"
		}
		return opening + "\n" + ExtractText(node) + "\n{code}"
	case "blockquote":
		return "{quote}\n" + strings.Join(wikiBlocks(node.Content), "\n\n") + "\n{quote}"
	case "panel":
		return "{panel}\n" + strings.Join(wikiBlocks(node.Content), "\n\n") + "\n{panel}"
	case "rule":
		return "----"
	case "table":
		return wikiTable(node)
	default:
		return escapeWiki(ExtractText(node))
	}
}

func wikiList(list Node, parentPrefix string) string {
	marker := "*"
	if list.Type == "orderedList" {
		marker = "#"
	}
	prefix := parentPrefix + marker
	var lines []string
	for _, item := range list.Content {
		first := true
		for _, child := range item.Content {
			switch child.Type {
			case "bulletList", "orderedList":
				if nested := wikiList(child, prefix); nested != "" {
					lines = append(lines, nested)
				}
			default:
				value := wikiBlock(child)
				if value == "" {
					continue
				}
				if first {
					lines = append(lines, prefix+" "+value)
					first = false
				} else {
					lines = append(lines, prefix+" "+strings.ReplaceAll(value, "\n", "\\\\\n"))
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

func wikiTable(table Node) string {
	var lines []string
	for _, row := range table.Content {
		header := len(row.Content) > 0
		cells := make([]string, 0, len(row.Content))
		for _, cell := range row.Content {
			header = header && cell.Type == "tableHeader"
			cells = append(cells, strings.Join(wikiBlocks(cell.Content), "\\\\"))
		}
		if header {
			lines = append(lines, "||"+strings.Join(cells, "||")+"||")
		} else {
			lines = append(lines, "|"+strings.Join(cells, "|")+"|")
		}
	}
	return strings.Join(lines, "\n")
}

func wikiInline(nodes []Node) string {
	var text strings.Builder
	for _, node := range nodes {
		value := ""
		switch node.Type {
		case "text":
			value = escapeWiki(node.Text)
			for _, mark := range node.Marks {
				switch mark.Type {
				case "strong":
					value = "*" + value + "*"
				case "em":
					value = "_" + value + "_"
				case "code":
					value = "{{" + value + "}}"
				case "strike":
					value = "-" + value + "-"
				case "link":
					value = "[" + value + "|" + stringAttr(mark.Attrs, "href") + "]"
				}
			}
		case "hardBreak":
			value = "\\\\\n"
		case "mention":
			if id := stringAttr(node.Attrs, "id"); id != "" {
				value = "[~accountid:" + id + "]"
			} else {
				value = escapeWiki(stringAttr(node.Attrs, "text"))
			}
		case "status":
			value = escapeWiki("[" + stringAttr(node.Attrs, "text") + "]")
		case "emoji":
			value = escapeWiki(ExtractText(node))
		default:
			if placeholder, ok := nodePlaceholder(node); ok {
				value = escapeWiki(placeholder)
			} else {
				value = escapeWiki(ExtractText(node))
			}
		}
		text.WriteString(value)
	}
	return text.String()
}

func escapeWiki(text string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\", "{", "\\{", "}", "\\}",
		"[", "\\[", "]", "\\]", "|", "\\|",
		"*", "\\*", "_", "\\_", "-", "\\-", "!", "\\!",
		"+", "\\+", "^", "\\^", "~", "\\~", "#", "\\#", "?", "\\?",
	)
	return replacer.Replace(text)
}
