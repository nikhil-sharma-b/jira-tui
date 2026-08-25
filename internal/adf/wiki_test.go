package adf_test

import (
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/adf"
)

func TestWikiMarkupPreservesEditableDescriptionStructure(t *testing.T) {
	doc := []byte(`{"type":"doc","version":1,"content":[` +
		`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Diagnosis"}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"Read ","marks":[]},{"type":"text","text":"the guide","marks":[{"type":"strong"},{"type":"link","attrs":{"href":"https://example.com/guide"}}]}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"Use *literal*, _literal_, -literal-, ~literal~, # literal, and ??citation?? markers"}]},` +
		`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Replace the relay"}]}]}]},` +
		`{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Part"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"State"}]}]}]},{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"Relay"}]}]},{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"Broken"}]}]}]}]}` +
		`]}`)

	got, err := adf.WikiMarkup(doc)
	if err != nil {
		t.Fatalf("WikiMarkup: %v", err)
	}
	for _, want := range []string{
		"h2. Diagnosis", "Read [*the guide*|https://example.com/guide]",
		`Use \*literal\*, \_literal\_, \-literal\-, \~literal\~, \# literal, and \?\?citation\?\? markers`,
		"* Replace the relay", "||Part||State||", "|Relay|Broken|",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("wiki markup does not contain %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"## Diagnosis", "• Replace", "┌", "\x1b["} {
		if strings.Contains(got, unwanted) {
			t.Errorf("wiki markup contains terminal rendering %q:\n%s", unwanted, got)
		}
	}
}
