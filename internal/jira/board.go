package jira

import (
	"context"
	"net/url"
	"strconv"
)

// boardsPageSize is how many boards one listing asks for. A project shown on
// more boards than this is a project whose board is chosen in config anyway.
const boardsPageSize = 50

// Boards lists the boards a project's work items appear on, in the order the
// site lists them.
func (c *REST) Boards(ctx context.Context, project string) ([]Board, error) {
	q := url.Values{}
	q.Set("projectKeyOrId", project)
	q.Set("maxResults", strconv.Itoa(boardsPageSize))
	var wire struct {
		Values []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"values"`
	}
	if err := c.get(ctx, "boards", apiAgile, "/board", q, &wire); err != nil {
		return nil, err
	}
	boards := make([]Board, 0, len(wire.Values))
	for _, v := range wire.Values {
		boards = append(boards, Board{ID: v.ID, Name: v.Name, Type: v.Type})
	}
	return boards, nil
}

// Board reads a board's configuration, which is where its columns and their
// statuses are; the board resource itself names only the board.
func (c *REST) Board(ctx context.Context, id int) (*Board, error) {
	var wire struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		Type         string `json:"type"`
		ColumnConfig struct {
			Columns []struct {
				Name     string `json:"name"`
				Statuses []struct {
					ID string `json:"id"`
				} `json:"statuses"`
			} `json:"columns"`
		} `json:"columnConfig"`
	}
	path := "/board/" + strconv.Itoa(id) + "/configuration"
	if err := c.get(ctx, "board", apiAgile, path, nil, &wire); err != nil {
		return nil, err
	}
	board := &Board{ID: wire.ID, Name: wire.Name, Type: wire.Type}
	for _, col := range wire.ColumnConfig.Columns {
		column := BoardColumn{Name: col.Name, Statuses: make([]string, 0, len(col.Statuses))}
		for _, s := range col.Statuses {
			column.Statuses = append(column.Statuses, s.ID)
		}
		board.Columns = append(board.Columns, column)
	}
	return board, nil
}
