package config

import (
	"strings"
	"testing"
	"time"
)

// listConfig appends a [list] section to the shared valid config.
func listConfig(body string) string {
	return validConfig + "\n[list]\n" + body
}

// TestListDefaults proves an absent [list] table renders today's row: the shipped
// column order and the shipped date format.
func TestListDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig, 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	want := []string{ListColumnFrom, ListColumnSubject, ListColumnDate}
	if got := cfg.List.ColumnOrder(); !equalStrings(got, want) {
		t.Errorf("columns = %v, want %v", got, want)
	}
	if cfg.List.DateFormat != ListDateFormat {
		t.Errorf("date_format = %q, want %q", cfg.List.DateFormat, ListDateFormat)
	}
}

// TestListOrderRoundTrips proves the configured order is what the renderer sees.
func TestListOrderRoundTrips(t *testing.T) {
	cfg, err := Load(writeConfig(t, listConfig(`
columns = ["date", "subject"]
`), 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	want := []string{ListColumnDate, ListColumnSubject}
	if got := cfg.List.ColumnOrder(); !equalStrings(got, want) {
		t.Errorf("columns = %v, want %v", got, want)
	}
}

// TestListZeroValueUsesDefaults proves a hand-built config (every test in the tree)
// renders the shipped row without a [list] table.
func TestListZeroValueUsesDefaults(t *testing.T) {
	var l List

	want := []string{ListColumnFrom, ListColumnSubject, ListColumnDate}
	if got := l.ColumnOrder(); !equalStrings(got, want) {
		t.Errorf("columns = %v, want %v", got, want)
	}
}

// TestListValidation covers every rule the [list] table enforces.
func TestListValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown column",
			body: `columns = ["from", "subject", "size"]`,
			want: `"size"`,
		},
		{
			name: "duplicate column",
			body: `columns = ["from", "from", "subject"]`,
			want: "twice",
		},
		{
			name: "subject missing",
			body: `columns = ["from", "date"]`,
			want: "subject",
		},
		{
			name: "format too wide",
			body: `date_format = "Monday, 02 January 2006"`,
			want: "date_format",
		},
		{
			name: "format with a newline",
			body: `date_format = "Jan\n02"`,
			want: "date_format",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, listConfig(tc.body), 0o600))
			if err == nil {
				t.Fatal("Load accepted the config")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestListEmptyDateFormatFallsBack proves an explicit empty value is the same as
// absent: the shipped format, not an empty date column.
func TestListEmptyDateFormatFallsBack(t *testing.T) {
	cfg, err := Load(writeConfig(t, listConfig(`date_format = ""`), 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.List.DateFormat != ListDateFormat {
		t.Errorf("date_format = %q, want %q", cfg.List.DateFormat, ListDateFormat)
	}
}

// TestListNamesAreNormalized proves case and stray spaces in a column name are
// accepted rather than turned into a load error.
func TestListNamesAreNormalized(t *testing.T) {
	cfg, err := Load(writeConfig(t, listConfig(`columns = [" From ", "SUBJECT"]`), 0o600))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	want := []string{ListColumnFrom, ListColumnSubject}
	if got := cfg.List.ColumnOrder(); !equalStrings(got, want) {
		t.Errorf("columns = %v, want %v", got, want)
	}
}

// TestListFormatDate covers the layout fallback, so a hand-built config and a loaded
// one render the same date.
func TestListFormatDate(t *testing.T) {
	when := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name   string
		format string
		want   string
	}{
		{name: "shipped", want: "Oct 29"},
		{name: "configured", format: "2006-01-02", want: "2026-10-29"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (List{DateFormat: tc.format}).FormatDate(when); got != tc.want {
				t.Errorf("FormatDate = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestListColumnOrderIsACopy proves the caller cannot write through to the config.
func TestListColumnOrderIsACopy(t *testing.T) {
	l := List{Columns: []string{ListColumnFrom, ListColumnSubject}}

	order := l.ColumnOrder()
	order[0] = ListColumnDate

	if l.Columns[0] != ListColumnFrom {
		t.Errorf("ColumnOrder aliased the config: %q", l.Columns[0])
	}
}

// equalStrings compares two string slices element by element.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
