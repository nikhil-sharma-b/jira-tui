// Package adf renders Atlassian Document Format into styled terminal lines.
//
// It is a pure function of its input: no I/O, no dependency on the Jira
// client, no TUI types. That makes it testable directly on documents captured
// from real tickets, which is the only credible source -- nobody hand-writes a
// convincing ADF table or a correctly nested mixed list.
package adf

import "encoding/json"

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

// Render walks doc and returns styled lines ready for a viewport.
func Render(doc []byte, opts Options) ([]string, error) {
	panic("not implemented")
}

// ExtractText recovers plain text from any node, known or not. This is the
// fallback that guarantees content is never silently dropped.
func ExtractText(n Node) string {
	panic("not implemented")
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
	panic("not implemented")
}
