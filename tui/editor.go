package tui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// editor is a small text buffer with a cursor, written by hand because the project has
// no textarea dependency and the composer needs to know where the cursor is so it can
// draw it. A single-line editor backs each header field; a multi-line one backs the
// body.
type editor struct {
	lines  []string
	row    int
	col    int
	single bool
}

// newEditor returns an empty editor. A single-line editor never grows a second line.
func newEditor(single bool) *editor {
	return &editor{lines: []string{""}, single: single}
}

// setText replaces the buffer and leaves the cursor at the end.
func (e *editor) setText(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	if e.single {
		text = strings.ReplaceAll(text, "\n", " ")
	}

	e.lines = strings.Split(text, "\n")
	if len(e.lines) == 0 {
		e.lines = []string{""}
	}

	e.row = len(e.lines) - 1
	e.col = len([]rune(e.lines[e.row]))
}

// text returns the buffer with lines joined by newlines.
func (e *editor) text() string {
	return strings.Join(e.lines, "\n")
}

// insert writes text at the cursor, splitting lines on newlines in multi-line mode.
func (e *editor) insert(text string) {
	text = strings.ReplaceAll(text, "\r", "")
	if e.single {
		text = strings.ReplaceAll(text, "\n", " ")
	}

	parts := strings.Split(text, "\n")
	line := []rune(e.lines[e.row])
	head := string(line[:e.col])
	tail := string(line[e.col:])

	if len(parts) == 1 {
		e.lines[e.row] = head + parts[0] + tail
		e.col += len([]rune(parts[0]))

		return
	}

	replacement := make([]string, 0, len(parts))
	replacement = append(replacement, head+parts[0])
	replacement = append(replacement, parts[1:len(parts)-1]...)
	replacement = append(replacement, parts[len(parts)-1]+tail)

	rest := append([]string(nil), e.lines[e.row+1:]...)
	e.lines = append(append(e.lines[:e.row], replacement...), rest...)
	e.row += len(parts) - 1
	e.col = len([]rune(parts[len(parts)-1]))
}

// backspace deletes the character before the cursor, joining lines at a line start.
func (e *editor) backspace() {
	if e.col > 0 {
		line := []rune(e.lines[e.row])
		e.lines[e.row] = string(line[:e.col-1]) + string(line[e.col:])
		e.col--

		return
	}

	if e.row == 0 {
		return
	}

	previous := e.lines[e.row-1]
	current := e.lines[e.row]
	e.lines[e.row-1] = previous + current
	e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
	e.row--
	e.col = len([]rune(previous))
}

// moveLeft and moveRight walk the buffer, crossing line boundaries.
func (e *editor) moveLeft() {
	if e.col > 0 {
		e.col--

		return
	}
	if e.row > 0 {
		e.row--
		e.col = len([]rune(e.lines[e.row]))
	}
}

func (e *editor) moveRight() {
	if e.col < len([]rune(e.lines[e.row])) {
		e.col++

		return
	}
	if e.row < len(e.lines)-1 {
		e.row++
		e.col = 0
	}
}

// moveUp and moveDown keep the column where they can and clamp at the shorter line.
func (e *editor) moveUp() {
	if e.single || e.row == 0 {
		return
	}

	e.row--
	if limit := len([]rune(e.lines[e.row])); e.col > limit {
		e.col = limit
	}
}

func (e *editor) moveDown() {
	if e.single || e.row >= len(e.lines)-1 {
		return
	}

	e.row++
	if limit := len([]rune(e.lines[e.row])); e.col > limit {
		e.col = limit
	}
}

// cursorHome and cursorLineEnd move within the current line.
func (e *editor) cursorHome() {
	e.col = 0
}

func (e *editor) cursorLineEnd() {
	e.col = len([]rune(e.lines[e.row]))
}

// cursorStart and cursorEnd move to the ends of the whole buffer: a top-posted reply
// starts at the beginning, a bottom-posted one at the end.
func (e *editor) cursorStart() {
	e.row = 0
	e.col = 0
}

func (e *editor) cursorEnd() {
	e.row = len(e.lines) - 1
	e.col = len([]rune(e.lines[e.row]))
}

// display wraps the buffer to width and reports the cursor as a row and column inside
// the wrapped rows, which is what the renderer needs to draw it.
func (e *editor) display(width int) (rows []string, cursorRow, cursorCol int) {
	if width < 1 {
		width = 1
	}

	cursorRow, cursorCol = 0, 0
	placed := false

	for i, line := range e.lines {
		segments := wrapCells(line, width)

		rows = append(rows, segments...)

		if !placed && i == e.row {
			segment, column := locateCursor(line, e.col, width)
			cursorRow = len(rows) - len(segments) + segment
			cursorCol = column
			placed = true
		}
	}

	return rows, cursorRow, cursorCol
}

// wrapCells splits one logical line into segments of at most width cells. It breaks at
// the exact width rather than at a word: the composer is a text field, not a renderer
// of prose, and a soft wrap would make the cursor mapping lie.
func wrapCells(line string, width int) []string {
	if line == "" {
		return []string{""}
	}

	segments := []string{}
	var current strings.Builder
	cells := 0

	for _, r := range line {
		cellWidth := runewidth.RuneWidth(r)
		if cellWidth < 1 {
			cellWidth = 1
		}

		if cells+cellWidth > width {
			segments = append(segments, current.String())
			current.Reset()
			cells = 0
		}

		current.WriteRune(r)
		cells += cellWidth
	}

	segments = append(segments, current.String())

	return segments
}

// locateCursor maps a rune offset inside a line onto the wrapped segment and column.
func locateCursor(line string, col, width int) (segment, column int) {
	runes := []rune(line)
	if col > len(runes) {
		col = len(runes)
	}

	cells := 0

	for i := 0; i < col; i++ {
		cellWidth := runewidth.RuneWidth(runes[i])
		if cellWidth < 1 {
			cellWidth = 1
		}

		if cells+cellWidth > width {
			segment++
			cells = 0
		}

		cells += cellWidth
	}

	return segment, cells
}
