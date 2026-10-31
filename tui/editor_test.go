package tui

import (
	"strings"
	"testing"
)

// TestEditorInsert covers the three positions text can arrive at.
func TestEditorInsert(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *editor)
		want  string
	}{
		{
			name:  "empty",
			setup: func(e *editor) { e.insert("hello") },
			want:  "hello",
		},
		{
			name: "end",
			setup: func(e *editor) {
				e.setText("ab")
				e.cursorEnd()
				e.insert("cd")
			},
			want: "abcd",
		},
		{
			name: "middle",
			setup: func(e *editor) {
				e.setText("ac")
				e.cursorHome()
				e.moveRight()
				e.insert("b")
			},
			want: "abc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEditor(false)
			tc.setup(e)

			if got := e.text(); got != tc.want {
				t.Errorf("text() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEditorBackspace covers deletion inside a line, at position zero and across a
// line boundary, where the two lines have to join.
func TestEditorBackspace(t *testing.T) {
	e := newEditor(false)
	e.setText("abc\ndef")

	// setText leaves the cursor at the end, so the first backspace deletes "f".
	e.backspace()
	if got := e.text(); got != "abc\nde" {
		t.Fatalf("backspace at the end: %q", got)
	}

	// One more, mid-line: "de" with the cursor after "d" becomes "e".
	e.moveLeft()
	e.backspace()
	if got := e.text(); got != "abc\ne" {
		t.Fatalf("backspace mid-line: %q", got)
	}

	// At a line start, backspace joins the two lines.
	e.cursorHome()
	e.backspace()
	if got := e.text(); got != "abce" {
		t.Errorf("backspace at line start did not join the lines: %q", got)
	}
	if e.row != 0 {
		t.Errorf("row = %d, want 0 after joining", e.row)
	}

	e.cursorStart()
	e.backspace()
	if got := e.text(); got != "abce" {
		t.Errorf("backspace at position zero changed the text: %q", got)
	}
}

// TestEditorNewlineSplits proves Enter splits a line and leaves the cursor at the start
// of the new one.
func TestEditorNewlineSplits(t *testing.T) {
	e := newEditor(false)
	e.setText("abcd")
	e.cursorHome()
	e.moveRight()
	e.moveRight()
	e.insert("\n")

	if got := e.text(); got != "ab\ncd" {
		t.Fatalf("text() = %q, want %q", got, "ab\ncd")
	}
	if e.row != 1 || e.col != 0 {
		t.Errorf("cursor = (%d,%d), want (1,0)", e.row, e.col)
	}
}

// TestEditorSingleLineIgnoresNewlines proves a header field cannot grow a second line.
func TestEditorSingleLineIgnoresNewlines(t *testing.T) {
	e := newEditor(true)
	e.insert("one\ntwo")

	if got := e.text(); got != "one two" {
		t.Errorf("text() = %q, want the newline flattened", got)
	}
	if e.row != 0 {
		t.Errorf("row = %d, want 0 in single-line mode", e.row)
	}

	e.moveDown()
	if e.row != 0 {
		t.Errorf("moveDown moved to row %d in single-line mode", e.row)
	}
}

// TestEditorVerticalMoveClamps proves up/down keep the column where they can and clamp
// at the shorter line.
func TestEditorVerticalMoveClamps(t *testing.T) {
	e := newEditor(false)
	e.setText("long line\nshort")
	e.cursorEnd()

	e.moveUp()
	if e.row != 0 || e.col != len([]rune("short")) {
		t.Errorf("cursor = (%d,%d), want (0,%d)", e.row, e.col, len([]rune("short")))
	}

	e.moveUp()
	if e.row != 0 {
		t.Errorf("moveUp left the first line: row %d", e.row)
	}

	e.moveDown()
	e.moveDown()
	if e.row != 1 {
		t.Errorf("moveDown went past the last line: row %d", e.row)
	}
}

// TestEditorHomeEnd proves home moves to the start of the line and end to its end, not
// to the start and end of the buffer.
func TestEditorHomeEnd(t *testing.T) {
	e := newEditor(false)
	e.setText("first\nsecond\nthird")
	e.row = 1
	e.col = 3

	e.cursorHome()
	if e.col != 0 || e.row != 1 {
		t.Errorf("home moved to (%d,%d), want (1,0)", e.row, e.col)
	}

	e.cursorLineEnd()
	if e.col != len([]rune("second")) || e.row != 1 {
		t.Errorf("end moved to (%d,%d), want (1,%d)", e.row, e.col, len([]rune("second")))
	}
}

// TestEditorDisplayWrapsAndPlacesCursor pins the rendering contract the composer needs:
// rows wrap at the given width, and the cursor is reported as a row and column pair
// inside those rows.
func TestEditorDisplayWrapsAndPlacesCursor(t *testing.T) {
	e := newEditor(false)
	e.setText("abcdefghijklm")

	rows, row, col := e.display(10)
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want 2 wrapped rows", rows)
	}
	if rows[0] != "abcdefghij" || rows[1] != "klm" {
		t.Errorf("rows = %v", rows)
	}
	if row != 1 || col != 3 {
		t.Errorf("cursor = (%d,%d), want (1,3) at the end of the text", row, col)
	}

	e.row = 0
	e.col = 3
	if _, row, col = e.display(10); row != 0 || col != 3 {
		t.Errorf("cursor = (%d,%d), want (0,3)", row, col)
	}
}

// TestEditorDisplayEmpty proves an empty buffer still reports one row and a cursor, so
// the composer has something to draw.
func TestEditorDisplayEmpty(t *testing.T) {
	e := newEditor(false)

	rows, row, col := e.display(10)
	if len(rows) != 1 || rows[0] != "" {
		t.Errorf("rows = %v, want one empty row", rows)
	}
	if row != 0 || col != 0 {
		t.Errorf("cursor = (%d,%d), want (0,0)", row, col)
	}
}

// TestEditorTextRoundTrip proves setText and text agree, including a trailing newline.
func TestEditorTextRoundTrip(t *testing.T) {
	for _, want := range []string{"one", "one\ntwo", "one\n\ntwo\n", ""} {
		e := newEditor(false)
		e.setText(want)

		if got := e.text(); got != want {
			t.Errorf("round trip: got %q, want %q", got, want)
		}
	}
}

// TestEditorCursorStartsWhereAsked proves the composer can open a draft with the cursor
// at the top or the bottom.
func TestEditorCursorStartsWhereAsked(t *testing.T) {
	e := newEditor(false)
	e.setText("line one\nline two")

	e.cursorStart()
	if e.row != 0 || e.col != 0 {
		t.Errorf("cursorStart moved to (%d,%d), want (0,0)", e.row, e.col)
	}

	e.cursorEnd()
	if got := e.text(); !strings.HasSuffix(got, "line two") {
		t.Fatalf("text changed: %q", got)
	}
	if e.row != 1 || e.col != len([]rune("line two")) {
		t.Errorf("cursorEnd moved to (%d,%d)", e.row, e.col)
	}
}

// TestEditorCursorCrossesLineBoundaries proves left and right walk out of one line into
// the neighbouring one, which is what makes editing a paragraph feel normal.
func TestEditorCursorCrossesLineBoundaries(t *testing.T) {
	e := newEditor(false)
	e.setText("ab\ncd")
	e.row, e.col = 0, 2

	e.moveRight()
	if e.row != 1 || e.col != 0 {
		t.Errorf("right at a line end moved to (%d,%d), want (1,0)", e.row, e.col)
	}

	e.moveLeft()
	if e.row != 0 || e.col != 2 {
		t.Errorf("left at a line start moved to (%d,%d), want (0,2)", e.row, e.col)
	}

	e.row, e.col = 1, 2
	e.moveRight()
	if e.row != 1 || e.col != 2 {
		t.Errorf("right at the end of the buffer moved to (%d,%d)", e.row, e.col)
	}
}
