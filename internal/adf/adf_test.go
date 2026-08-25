package adf_test

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nikhil-sharma-b/jira-tui/internal/adf"
)

func TestRenderParagraphWrapsAtRequestedWidth(t *testing.T) {
	doc := []byte(`{
		"version": 1,
		"type": "doc",
		"content": [{"type":"paragraph","content":[{"type":"text","text":"alpha beta gamma"}]}]
	}`)

	got, err := adf.Render(doc, adf.Options{Width: 10, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha beta", "gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Render() = %#v, want %#v", got, want)
	}

	got, err = adf.Render(doc, adf.Options{Width: 20, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"alpha beta gamma"}) {
		t.Fatalf("Render() after resize = %#v", got)
	}
}

func TestRenderDistinguishesBlockStructures(t *testing.T) {
	doc := []byte(`{
		"version":1,
		"type":"doc",
		"content":[
			{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Release notes"}]},
			{"type":"paragraph","content":[{"type":"text","text":"first"},{"type":"hardBreak"},{"type":"text","text":"second"}]},
			{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"for {\n  work()\n}"}]},
			{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted words"}]}]},
			{"type":"panel","attrs":{"panelType":"warning"},"content":[{"type":"paragraph","content":[{"type":"text","text":"take care"}]}]},
			{"type":"rule"}
		]
	}`)

	got, err := adf.Render(doc, adf.Options{Width: 20, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"## Release notes", "",
		"first", "second", "",
		"    for {", "      work()", "    }", "",
		"> quoted words", "",
		"[WARNING] take care", "",
		"────────────────────",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderMixedNestedLists(t *testing.T) {
	doc := []byte(`{
		"version":1,
		"type":"doc",
		"content":[{"type":"bulletList","content":[
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"alpha"}]}]},
			{"type":"listItem","content":[
				{"type":"paragraph","content":[{"type":"text","text":"beta item wraps here"}]},
				{"type":"orderedList","attrs":{"order":3},"content":[
					{"type":"listItem","content":[
						{"type":"paragraph","content":[{"type":"text","text":"nested"}]},
						{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"mixed"}]}]}]}
					]}
				]}
			]}
		]}]
	}`)

	got, err := adf.Render(doc, adf.Options{Width: 18, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"• alpha",
		"• beta item wraps",
		"  here",
		"  3. nested",
		"     • mixed",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Render() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderInlineNodesAndMarks(t *testing.T) {
	doc := []byte(`{
		"version":1,
		"type":"doc",
		"content":[{"type":"paragraph","content":[
			{"type":"text","text":"bold","marks":[{"type":"strong"}]},
			{"type":"text","text":" italic","marks":[{"type":"em"}]},
			{"type":"text","text":" code","marks":[{"type":"code"}]},
			{"type":"text","text":" gone","marks":[{"type":"strike"}]},
			{"type":"text","text":" link","marks":[{"type":"link","attrs":{"href":"https://example.test"}}]},
			{"type":"text","text":" "},
			{"type":"mention","attrs":{"id":"account-1","text":"@Ada"}},
			{"type":"text","text":" "},
			{"type":"status","attrs":{"text":"In progress","color":"yellow"}},
			{"type":"text","text":" "},
			{"type":"emoji","attrs":{"shortName":":grinning:","text":"😀"}},
			{"type":"text","text":" "},
			{"type":"date","attrs":{"timestamp":"1582152559"}}
		]}]
	}`)
	want := "bold italic code gone link (https://example.test) @Ada [IN PROGRESS] 😀 2020-02-19"

	plain, err := adf.Render(doc, adf.Options{Width: 100, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plain, []string{want}) {
		t.Fatalf("plain Render() = %q, want %q", plain, want)
	}

	styled, err := adf.Render(doc, adf.Options{Width: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(styled) != 1 || ansi.Strip(styled[0]) != want {
		t.Fatalf("styled Render() = %q", styled)
	}
	if !strings.Contains(styled[0], "\x1b[") {
		t.Fatalf("styled Render() contains no ANSI styling: %q", styled[0])
	}
}

func TestRenderPlaceholdersAndUnknownFallback(t *testing.T) {
	doc := []byte(`{
		"version":1,
		"type":"doc",
		"content":[
			{"type":"mystery","content":[{"type":"text","text":"preserved "},{"type":"mention","attrs":{"text":"@Ada"}}]},
			{"type":"expand","attrs":{"title":"Details"},"content":[{"type":"paragraph","content":[{"type":"text","text":"hidden body"}]}]},
			{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"101","type":"file","collection":"","alt":"diagram.png"}}]},
			{"type":"mediaGroup","content":[{"type":"media","attrs":{"id":"media-1","type":"file","collection":"jira-issue","alt":"remote.png"}}]},
			{"type":"mediaSingle","content":[{"type":"media","attrs":{"type":"external","url":"https://example.test/image.png","alt":"external.png"}}]},
			{"type":"paragraph","content":[{"type":"text","text":"See "},{"type":"inlineCard","attrs":{"url":"https://example.test/JT-4"}}]}
		]
	}`)

	got, err := adf.Render(doc, adf.Options{Width: 80, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"preserved @Ada", "",
		"[expand: Details]", "",
		"[attachment: diagram.png]", "",
		"[media: remote.png; Media Services access required]", "",
		"[external media: external.png]", "",
		"See [card: https://example.test/JT-4]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Render() =\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(strings.Join(got, "\n"), "unknown") || strings.Contains(strings.Join(got, "\n"), "error") {
		t.Fatalf("fallback rendered an error marker: %q", got)
	}
}

func TestRenderWrapsMediaContainerPlaceholders(t *testing.T) {
	doc := []byte(`{"version":1,"type":"doc","content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"type":"external","url":"https://example.test/image.png","alt":"external.png"}}]}]}`)

	lines, err := adf.Render(doc, adf.Options{Width: 20, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("media placeholder used %d lines, want 1: %q", len(lines), lines)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 20 {
			t.Fatalf("media placeholder exceeds width: %q", line)
		}
	}
}

func TestRenderKeepsInlineCardPlaceholderOnOneLine(t *testing.T) {
	doc := []byte(`{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"See "},{"type":"inlineCard","attrs":{"url":"https://example.test/a-long-card"}}]}]}`)

	lines, err := adf.Render(doc, adf.Options{Width: 20, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "See" || !strings.HasPrefix(lines[1], "[card: ") {
		t.Fatalf("inline card placeholder split across lines: %q", lines)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 20 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
}

func TestMediaNodesClassifiesAttachmentsAndMediaServiceItems(t *testing.T) {
	doc := []byte(`{
		"version":1,
		"type":"doc",
		"content":[
			{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"101","collection":"","alt":"diagram.png"}}]},
			{"type":"paragraph","content":[
				{"type":"mediaInline","attrs":{"id":"media-1","type":"file","collection":"jira-issue","fileName":"remote.png"}},
				{"type":"mediaInline","attrs":{"type":"external","url":"https://example.test/image.png","alt":"external.png"}}
			]}
		]
	}`)

	got, err := adf.MediaNodes(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := []adf.Media{
		{ID: "101", Filename: "diagram.png", IsAttachment: true},
		{ID: "media-1", Filename: "remote.png", IsAttachment: false},
		{Filename: "external.png", IsAttachment: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MediaNodes() = %#v, want %#v", got, want)
	}

	if _, err := adf.MediaNodes([]byte(`{"type":`)); err == nil {
		t.Fatal("MediaNodes() accepted malformed JSON")
	}
}

func TestExtractTextRecoversContentFromUnknownNodes(t *testing.T) {
	node := adf.Node{
		Type: "mystery",
		Content: []adf.Node{
			{Type: "text", Text: "before "},
			{Type: "anotherMystery", Content: []adf.Node{{Type: "text", Text: "inside"}}},
			{Type: "hardBreak"},
			{Type: "mention", Attrs: map[string]any{"text": "@Ada"}},
			{Type: "paragraph", Content: []adf.Node{{Type: "text", Text: "last"}}},
		},
	}

	if got := adf.ExtractText(node); got != "before inside\n@Ada\nlast" {
		t.Fatalf("ExtractText() = %q", got)
	}
}

func TestRenderTableReflowsWhenWidthChanges(t *testing.T) {
	doc := []byte(`{
		"version":1,
		"type":"doc",
		"content":[{"type":"table","content":[
			{"type":"tableRow","content":[
				{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Name"}]}]},
				{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"State"}]}]}
			]},
			{"type":"tableRow","content":[
				{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"API"}]}]},
				{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"In progress"}]}]}
			]},
			{"type":"tableRow","content":[
				{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"UI"}]}]},
				{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"Done"}]}]}
			]}
		]}]
	}`)

	wide, err := adf.Render(doc, adf.Options{Width: 30, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	wantWide := []string{
		"┌──────┬─────────────┐",
		"│ Name │ State       │",
		"├──────┼─────────────┤",
		"│ API  │ In progress │",
		"├──────┼─────────────┤",
		"│ UI   │ Done        │",
		"└──────┴─────────────┘",
	}
	if !reflect.DeepEqual(wide, wantWide) {
		t.Fatalf("wide Render() =\n%q\nwant\n%q", wide, wantWide)
	}

	narrow, err := adf.Render(doc, adf.Options{Width: 16, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	wantNarrow := []string{
		"Name: API",
		"State: In",
		"       progress",
		"",
		"Name: UI",
		"State: Done",
	}
	if !reflect.DeepEqual(narrow, wantNarrow) {
		t.Fatalf("narrow Render() =\n%q\nwant\n%q", narrow, wantNarrow)
	}
	for _, line := range narrow {
		if ansi.StringWidth(line) > 16 {
			t.Fatalf("narrow line exceeds width: %q", line)
		}
	}
}

func TestRenderEmptyAndMalformedDocuments(t *testing.T) {
	for _, test := range []struct {
		name string
		doc  []byte
	}{
		{name: "nil", doc: nil},
		{name: "zero length", doc: []byte{}},
		{name: "whitespace", doc: []byte(" \n\t")},
		{name: "null", doc: []byte("null")},
		{name: "empty document", doc: []byte(`{"version":1,"type":"doc","content":[]}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			lines, err := adf.Render(test.doc, adf.Options{Width: 20, Plain: true})
			if err != nil {
				t.Fatalf("Render(%q): %v", test.doc, err)
			}
			if len(lines) != 0 {
				t.Fatalf("Render(%q) = %q, want no lines", test.doc, lines)
			}
		})
	}

	if _, err := adf.Render([]byte(`{"type":`), adf.Options{}); err == nil {
		t.Fatal("Render() accepted malformed JSON")
	}
}

func TestRenderKeepsStructuralPrefixesWithinPositiveWidth(t *testing.T) {
	tests := []struct {
		name  string
		width int
		doc   string
	}{
		{name: "heading", width: 1, doc: `{"type":"doc","content":[{"type":"heading","attrs":{"level":6},"content":[{"type":"text","text":"heading"}]}]}`},
		{name: "blockquote", width: 1, doc: `{"type":"doc","content":[{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quote"}]}]}]}`},
		{name: "panel", width: 4, doc: `{"type":"doc","content":[{"type":"panel","attrs":{"panelType":"warning"},"content":[{"type":"paragraph","content":[{"type":"text","text":"panel"}]}]}]}`},
		{name: "list", width: 1, doc: `{"type":"doc","content":[{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"item"}]}]}]}]}`},
		{name: "stacked table", width: 4, doc: `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Long header"}]}]}]},{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"value"}]}]}]}]}]}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lines, err := adf.Render([]byte(test.doc), adf.Options{Width: test.width, Plain: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > test.width {
					t.Fatalf("line width %d exceeds %d: %q", ansi.StringWidth(line), test.width, line)
				}
			}
		})
	}
}

func TestRenderHardWrapsWideAndUnbrokenText(t *testing.T) {
	doc := []byte(`{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"😀😀😀 abcdef"}]}]}`)

	got, err := adf.Render(doc, adf.Options{Width: 4, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"😀😀", "😀", "abcd", "ef"}) {
		t.Fatalf("Render() = %q", got)
	}
	for _, line := range got {
		if ansi.StringWidth(line) > 4 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
}

func TestRenderKeepsStylingWithinEachWrappedLine(t *testing.T) {
	doc := []byte(`{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"alpha beta","marks":[{"type":"strong"}]}]}]}`)

	got, err := adf.Render(doc, adf.Options{Width: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || ansi.Strip(got[0]) != "alpha" || ansi.Strip(got[1]) != "beta" {
		t.Fatalf("Render() = %q", got)
	}
	for _, line := range got {
		if !strings.HasPrefix(line, "\x1b[") || !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatalf("wrapped style is not self-contained: %q", line)
		}
	}
}

func TestRenderCapturedDocumentGolden(t *testing.T) {
	doc, err := os.ReadFile("testdata/captured.adf.json")
	if err != nil {
		t.Fatal(err)
	}

	for _, width := range []int{80, 32} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			lines, err := adf.Render(doc, adf.Options{Width: width, Plain: true})
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Join(lines, "\n") + "\n"
			path := fmt.Sprintf("testdata/captured.%d.golden", width)
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("Render() differs from %s; run UPDATE_GOLDEN=1 go test ./internal/adf after reviewing the change", path)
			}
		})
	}
}

func TestMediaNodesCapturedDocument(t *testing.T) {
	doc, err := os.ReadFile("testdata/captured.adf.json")
	if err != nil {
		t.Fatal(err)
	}
	media, err := adf.MediaNodes(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(media) != 4 {
		t.Fatalf("MediaNodes() returned %d items, want 4", len(media))
	}
	attachments := 0
	for _, item := range media {
		if item.IsAttachment {
			attachments++
		}
	}
	if attachments != 3 {
		t.Fatalf("MediaNodes() classified %d attachments, want 3", attachments)
	}
}
