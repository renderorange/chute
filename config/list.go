package config

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Column names accepted in [list] columns. Order is data, not an enum position: the
// renderer walks the configured slice, so these are only the vocabulary.
const (
	ListColumnFrom    = "from"
	ListColumnSubject = "subject"
	ListColumnDate    = "date"
)

// ListDateFormat is the shipped date column layout (a Go reference layout).
const ListDateFormat = "Jan 02"

// ListMaxDateCells caps how wide the rendered date column may be. "Mon, 02 Jan 2006"
// is the longest ordinary layout at 16 cells; the renderer truncates to the same
// limit in display cells, because this check counts runes and a wide-rune literal is
// twice as wide as it is long.
const ListMaxDateCells = 16

// listReferenceTime is the moment a configured layout is rendered against when the
// config is validated: the Go reference date, so every verb in the layout is
// exercised. It is fixed rather than "now" so a load error cannot depend on the day.
var listReferenceTime = time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)

// List configures the message-list row: which columns appear and in what order, and
// how the date column is formatted.
type List struct {
	// Columns is the order to draw. Omitting "date" hides the date column; "subject"
	// is required because it is the column that absorbs the leftover width.
	Columns []string `toml:"columns"`

	// DateFormat is a Go reference layout. Empty means the shipped "Jan 02".
	DateFormat string `toml:"date_format"`
}

// DefaultListColumns returns the shipped column order.
func DefaultListColumns() []string {
	return []string{ListColumnFrom, ListColumnSubject, ListColumnDate}
}

// ColumnOrder is the effective order: the configured list, or the shipped one when
// [list] is absent. The result is a copy, so a caller cannot write through to the
// config.
func (l List) ColumnOrder() []string {
	if len(l.Columns) == 0 {
		return DefaultListColumns()
	}

	return append([]string(nil), l.Columns...)
}

// FormatDate renders t in the configured layout. The fallback matters because tests
// and any hand-built config skip applyDefaults.
func (l List) FormatDate(t time.Time) string {
	format := l.DateFormat
	if format == "" {
		format = ListDateFormat
	}

	return t.Format(format)
}

// validate normalizes the column names and checks the date layout. The names are
// lowercased and trimmed rather than rejected, since they are not keys: there is no
// case-sensitive meaning to preserve.
func (l *List) validate() error {
	seen := make(map[string]bool, len(l.Columns))
	clean := make([]string, 0, len(l.Columns))

	for i, name := range l.Columns {
		name = strings.ToLower(strings.TrimSpace(name))

		switch name {
		case ListColumnFrom, ListColumnSubject, ListColumnDate:
		default:
			return fmt.Errorf("config: [list] columns[%d] is %q, want %q, %q or %q",
				i, name, ListColumnFrom, ListColumnSubject, ListColumnDate)
		}

		if seen[name] {
			return fmt.Errorf("config: [list] %q is listed twice", name)
		}

		seen[name] = true
		clean = append(clean, name)
	}

	l.Columns = clean

	// subject takes the width the other columns leave, so a row without it cannot
	// fill the pane.
	if len(l.Columns) > 0 && !seen[ListColumnSubject] {
		return fmt.Errorf("config: [list] columns must include %q, which takes the leftover width",
			ListColumnSubject)
	}

	rendered := listReferenceTime.Format(l.DateFormat)

	for _, r := range rendered {
		if unicode.IsControl(r) {
			return fmt.Errorf("config: [list] date_format %q renders a control character", l.DateFormat)
		}
	}

	if width := utf8.RuneCountInString(rendered); width > ListMaxDateCells {
		return fmt.Errorf("config: [list] date_format %q renders %d cells, want at most %d",
			l.DateFormat, width, ListMaxDateCells)
	}

	return nil
}
