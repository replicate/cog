package terminal

import (
	"github.com/fatih/color"
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

// Table implements UI
func (u *basicUI) Table(tbl *Table, opts ...Option) {
	// Build our config and set our options
	cfg := &config{Writer: color.Output}
	for _, opt := range opts {
		opt(cfg)
	}

	table := tablewriter.NewWriter(cfg.Writer)
	table.SetHeader(tbl.Headers)
	table.SetBorder(false)
	table.SetAutoWrapText(false)

	for _, row := range tbl.Rows {
		colors := make([]tablewriter.Colors, len(row))
		entries := make([]string, len(row))

		for i, ent := range row {
			entries[i] = ent.Value

			color, ok := colorMapping[ent.Color]
			if ok {
				colors[i] = tablewriter.Colors{color}
			}
		}

		table.Rich(entries, colors)
	}

	table.Render()
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
