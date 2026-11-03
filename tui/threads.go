package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// MaxThreadDepth is how deep nesting is indented. The messages panel is 40-48
// columns, which leaves about 15 subject cells: two cells per level and three
// levels keeps a subject readable, and deeper replies stop indenting rather than
// squeezing the subject out of the row.
const MaxThreadDepth = 3

// noParent marks a message that starts a conversation.
const noParent = -1

// Row is one message as the list draws it: the message itself, how deep it nests,
// and which conversation it belongs to. Hidden and HiddenUnread are only non-zero on a
// collapsed thread's root row: the replies folded into it.
type Row struct {
	Message      mail.Message
	Depth        int
	Thread       int
	Hidden       int
	HiddenUnread bool
}

// BuildRows groups messages into conversations and returns them in list order.
//
// Blocks are ordered by each conversation's newest message, descending; inside a
// conversation the messages run oldest to newest with each reply below the message
// it answers. The result depends only on the messages, never on the order they
// arrive in.
//
// A reply attaches to the last In-Reply-To entry naming a message in the same
// folder. With no such parent it starts its own conversation, so a missing parent
// makes a message visible rather than hidden.
func BuildRows(messages []mail.Message) []Row {
	n := len(messages)
	if n == 0 {
		return nil
	}

	byID := indexByMessageID(messages)
	parent := parents(messages, byID, n)

	children := make([][]int, n)
	roots := make([]int, 0, n)

	for i := range messages {
		if parent[i] == noParent {
			roots = append(roots, i)
			continue
		}
		children[parent[i]] = append(children[parent[i]], i)
	}

	for i := range children {
		sortChildren(messages, children[i])
	}
	sortRoots(messages, children, roots)

	rows := make([]Row, 0, n)
	for thread, root := range roots {
		rows = appendThread(rows, messages, children, root, thread)
	}

	return rows
}

// indexByMessageID maps a message id to the message that owns it. The first
// message with an id wins, so a duplicate id stands on its own instead of
// disappearing or becoming a second parent.
func indexByMessageID(messages []mail.Message) map[string]int {
	byID := make(map[string]int, len(messages))

	for i := range messages {
		id := strings.TrimSpace(messages[i].MessageID)
		if id == "" {
			continue
		}
		if _, taken := byID[id]; taken {
			continue
		}
		byID[id] = i
	}

	return byID
}

// parents resolves each message's parent index, or noParent. References are
// scanned from the last entry backwards, because the last one naming a message in
// this folder is the closest ancestor.
func parents(messages []mail.Message, byID map[string]int, n int) []int {
	parent := make([]int, n)
	for i := range parent {
		parent[i] = noParent
	}

	for i := range messages {
		refs := messages[i].InReplyTo
		for k := len(refs) - 1; k >= 0; k-- {
			id := strings.TrimSpace(refs[k])
			if id == "" {
				continue
			}
			j, ok := byID[id]
			if !ok || j == i || isAncestor(parent, j, i) {
				continue
			}
			parent[i] = j
			break
		}
	}

	return parent
}

// isAncestor reports whether ancestor sits above candidate in the parent chain
// built so far. A true answer means making ancestor the parent of candidate would
// close a cycle, which In-Reply-To headers can do.
func isAncestor(parent []int, ancestor, candidate int) bool {
	for steps := 0; steps <= len(parent); steps++ {
		if ancestor == noParent {
			return false
		}
		if ancestor == candidate {
			return true
		}
		ancestor = parent[ancestor]
	}

	// A chain longer than the message list can only be a cycle.
	return true
}

// sortChildren orders a parent's replies oldest first, with UID as the tiebreak.
func sortChildren(messages []mail.Message, indexes []int) {
	sort.Slice(indexes, func(a, b int) bool {
		left, right := messages[indexes[a]], messages[indexes[b]]

		if !left.Date().Equal(right.Date()) {
			return left.Date().Before(right.Date())
		}

		return left.UID < right.UID
	})
}

// sortRoots orders conversations by their newest message, descending. The list is
// therefore newest-conversation-first while each conversation still reads oldest to
// newest.
func sortRoots(messages []mail.Message, children [][]int, roots []int) {
	newestDate := make(map[int]time.Time, len(roots))
	newestUID := make(map[int]imap.UID, len(roots))

	for _, root := range roots {
		newestDate[root], newestUID[root] = newestInThread(messages, children, root)
	}

	sort.SliceStable(roots, func(a, b int) bool {
		left, right := roots[a], roots[b]

		if !newestDate[left].Equal(newestDate[right]) {
			return newestDate[left].After(newestDate[right])
		}
		if newestUID[left] != newestUID[right] {
			return newestUID[left] > newestUID[right]
		}

		return left < right
	})
}

// newestInThread finds the newest message date and UID in one conversation.
func newestInThread(messages []mail.Message, children [][]int, root int) (time.Time, imap.UID) {
	var newest time.Time
	var newestUID imap.UID

	stack := []int{root}
	for len(stack) > 0 {
		last := len(stack) - 1
		index := stack[last]
		stack = stack[:last]

		date := messages[index].Date()
		if date.After(newest) || (date.Equal(newest) && messages[index].UID > newestUID) {
			newest = date
			newestUID = messages[index].UID
		}

		stack = append(stack, children[index]...)
	}

	return newest, newestUID
}

// appendThread emits one conversation, parent before its replies. An explicit
// stack keeps a long or malformed thread from exhausting the goroutine stack.
func appendThread(rows []Row, messages []mail.Message, children [][]int, root, thread int) []Row {
	type item struct {
		index int
		depth int
	}

	stack := []item{{index: root}}
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]

		rows = append(rows, Row{
			Message: messages[current.index],
			Depth:   min(current.depth, MaxThreadDepth),
			Thread:  thread,
		})

		// Push in reverse so the earliest reply is emitted first.
		for c := len(children[current.index]) - 1; c >= 0; c-- {
			stack = append(stack, item{index: children[current.index][c], depth: current.depth + 1})
		}
	}

	return rows
}
