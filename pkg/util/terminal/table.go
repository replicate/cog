package terminal

import (
	"github.com/olekukonko/tablewriter"
)

// Passed to UI.Table to provide a nicely formatted table.
type Table struct {
	Headers []string
	Rows    [][]TableEntry
}

// Table creates a new Table structure that can be used with UI.Table.
func NewTable(headers ...string) *Table {
	return &Table{
		Headers: headers,
	}
}

// TableEntry is a single entry for a table.
type TableEntry struct {
	Value string
	Color string
}

// Rich adds a row to the table.
func (t *Table) Rich(cols []string, colors []string) {
	var row []TableEntry

	for i, col := range cols {
		if i < len(colors) {
			row = append(row, TableEntry{Value: col, Color: colors[i]})
		} else {
			row = append(row, TableEntry{Value: col})
		}
	}

	t.Rows = append(t.Rows, row)
}

const (
	Yellow = "yellow"
	Green  = "green"
	Red    = "red"
)

var colorMapping = map[string]int{
	Green:  tablewriter.FgGreenColor,
	Yellow: tablewriter.FgYellowColor,
	Red:    tablewriter.FgRedColor,
}
