package jira_test

import (
	"context"
	"testing"
)

func TestBoardsListsAProjectsBoards(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{
		"/rest/agile/1.0/board": "boards.200",
	})
	c := newClient(t, srv.URL)

	boards, err := c.Boards(context.Background(), "ENG")
	if err != nil {
		t.Fatalf("Boards: %v", err)
	}
	if len(boards) != 2 {
		t.Fatalf("got %d boards, want 2: %#v", len(boards), boards)
	}
	if b := boards[1]; b.ID != 92 || b.Name != "ENG board" || b.Type != "kanban" {
		t.Errorf("second board = %+v, want the ENG kanban board, 92", b)
	}
	if got := srv.Requests[0].URL.Query().Get("projectKeyOrId"); got != "ENG" {
		t.Errorf("projectKeyOrId = %q, want ENG", got)
	}
}

func TestBoardReadsTheColumnsAndTheirStatuses(t *testing.T) {
	srv := newFixtureServer(t, map[string]string{
		"/rest/agile/1.0/board/92/configuration": "boardconfig.200",
	})
	c := newClient(t, srv.URL)

	board, err := c.Board(context.Background(), 92)
	if err != nil {
		t.Fatalf("Board: %v", err)
	}
	if board.ID != 92 || board.Name != "ENG board" {
		t.Errorf("board = %d %q, want 92 ENG board", board.ID, board.Name)
	}
	var names []string
	for _, c := range board.Columns {
		names = append(names, c.Name)
	}
	if got := len(names); got != 4 || names[0] != "To Do" || names[3] != "Archive" {
		t.Fatalf("columns = %q, want To Do, In Progress, Done, Archive in order", names)
	}
	// A column can hold several statuses, and none: an empty column is still
	// one the board draws.
	if got := board.Columns[1].Statuses; len(got) != 2 || got[0] != "3" || got[1] != "10002" {
		t.Errorf("In Progress holds %q, want statuses 3 and 10002", got)
	}
	if got := board.Columns[3].Statuses; len(got) != 0 {
		t.Errorf("Archive holds %q, want nothing", got)
	}
}
