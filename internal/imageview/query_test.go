package imageview_test

import (
	"strings"
	"testing"

	"github.com/nikhil-sharma-b/jira-tui/internal/imageview"
)

func TestReadAnswersReadsUpToTheDeviceAttributes(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  imageview.Answers
	}{
		{"kitty answers", "\x1b_Gi=31;OK\x1b\\\x1b[?62;22c", imageview.Answers{Kitty: true}},
		{"graphics error", "\x1b_Gi=31;ENOTSUPPORTED:no\x1b\\\x1b[?62c", imageview.Answers{}},
		{"a terminal with no graphics", "\x1b[?1;2c", imageview.Answers{}},
		{"sixel with its colour registers", "\x1b[?1;0;1024S\x1b[?62;4;22c", imageview.Answers{Sixel: true, SixelColors: 1024}},
		{"sixel without saying how many colours", "\x1b[?64;4c", imageview.Answers{Sixel: true}},
		{"colour registers refused", "\x1b[?1;3;0S\x1b[?62;4c", imageview.Answers{Sixel: true}},
		{"a 4 that is not a parameter of its own", "\x1b[?62;44c", imageview.Answers{}},
		{"colour registers but no sixel", "\x1b[?1;0;256S\x1b[?62;22c", imageview.Answers{SixelColors: 256}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.reply + "typed after")
			got, err := imageview.ReadAnswers(r)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("ReadAnswers = %+v, want %+v", got, tt.want)
			}
			if rest := r.Len(); rest != len("typed after") {
				t.Errorf("read %d bytes past the device attributes", len("typed after")-rest)
			}
		})
	}
}

func TestQueryAsksForGraphicsThenColoursThenDeviceAttributes(t *testing.T) {
	q := imageview.Query(true)
	seq, rest, ok := strings.Cut(q, "\x1b\\")
	if !ok || rest != "\x1b[?1;1;0S\x1b[c" {
		t.Fatalf("query = %q, want a graphics command, XTSMGRAPHICS then DA1", q)
	}
	keys, _ := apc(t, seq+"\x1b\\")
	if keys["a"] != "q" || keys["i"] != "31" {
		t.Errorf("query keys = %v", keys)
	}
}

func TestQueryWithoutKittyAsksNothingTmuxWouldNotAnswer(t *testing.T) {
	if q := imageview.Query(false); q != "\x1b[?1;1;0S\x1b[c" {
		t.Errorf("query = %q, want XTSMGRAPHICS then DA1 only", q)
	}
}
