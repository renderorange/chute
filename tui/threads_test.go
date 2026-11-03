package tui

import (
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/renderorange/chute/mail"
)

// threadMessage builds one message whose date is derived from day, so ordering
// is readable in the tests below.
func threadMessage(uid imap.UID, subject, messageID string, day int, inReplyTo ...string) mail.Message {
	return mail.Message{
		UID: uid,
		Envelope: imap.Envelope{
			Subject: subject,
			Date:    time.Date(2026, 10, day, 9, 0, 0, 0, time.UTC),
		},
		MessageID: messageID,
		InReplyTo: inReplyTo,
	}
}

// rowUIDs lists the UIDs in row order.
func rowUIDs(rows []Row) []imap.UID {
	uids := make([]imap.UID, len(rows))
	for i, row := range rows {
		uids[i] = row.Message.UID
	}

	return uids
}

// rowDepths lists the indent depth in row order.
func rowDepths(rows []Row) []int {
	depths := make([]int, len(rows))
	for i, row := range rows {
		depths[i] = row.Depth
	}

	return depths
}

func assertUIDs(t *testing.T, rows []Row, want []imap.UID) {
	t.Helper()

	got := rowUIDs(rows)
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows = %v, want %v", got, want)
		}
	}
}

func assertDepths(t *testing.T, rows []Row, want []int) {
	t.Helper()

	got := rowDepths(rows)
	if len(got) != len(want) {
		t.Fatalf("depths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("depths = %v, want %v", got, want)
		}
	}
}

// TestBuildRowsChain proves a reply and a reply-to-reply nest under their parent,
// oldest first, regardless of the order the messages arrive in.
func TestBuildRowsChain(t *testing.T) {
	parent := threadMessage(1, "parent", "<p>", 1)
	reply := threadMessage(2, "reply", "<r1>", 2, "<p>")
	reply2 := threadMessage(3, "reply two", "<r2>", 3, "<r1>")

	rows := BuildRows([]mail.Message{reply2, parent, reply})

	assertUIDs(t, rows, []imap.UID{1, 2, 3})
	assertDepths(t, rows, []int{0, 1, 2})

	thread := rows[0].Thread
	for i, row := range rows {
		if row.Thread != thread {
			t.Errorf("row %d thread = %d, want %d for one conversation", i, row.Thread, thread)
		}
	}
}

// TestBuildRowsThreadsOnReferences is #70: a sender that sets only References still nests,
// under the last entry that names a message in this folder.
func TestBuildRowsThreadsOnReferences(t *testing.T) {
	// Bare ids, the shape the envelope and the parsed References header both have in
	// production (#71); a mixed form is the bug that made threading look wired.
	root := threadMessage(1, "root", "a", 1)
	middle := threadMessage(2, "middle", "b", 2, "a")

	reply := threadMessage(3, "reply", "c", 3)
	reply.References = []string{"a", "b"}

	rows := BuildRows([]mail.Message{reply, root, middle})

	assertUIDs(t, rows, []imap.UID{1, 2, 3})
	assertDepths(t, rows, []int{0, 1, 2})
}

// TestBuildRowsPrefersInReplyTo proves the fallback cannot override a real parent: when both
// headers point at different messages, the immediate parent wins.
func TestBuildRowsPrefersInReplyTo(t *testing.T) {
	root := threadMessage(1, "root", "a", 1)
	middle := threadMessage(2, "middle", "b", 2, "a")

	reply := threadMessage(3, "reply", "c", 3, "b")
	reply.References = []string{"a"}

	rows := BuildRows([]mail.Message{root, middle, reply})

	assertUIDs(t, rows, []imap.UID{1, 2, 3})
	assertDepths(t, rows, []int{0, 1, 2})
}

// TestBuildRowsReferencesOutsideTheFolder leaves the message at the root: a chain naming
// only unknown ids is the orphan case, not a thread.
func TestBuildRowsReferencesOutsideTheFolder(t *testing.T) {
	orphan := threadMessage(1, "orphan", "o", 2)
	orphan.References = []string{"gone-1", "gone-2"}

	rows := BuildRows([]mail.Message{orphan})

	assertUIDs(t, rows, []imap.UID{1})
	assertDepths(t, rows, []int{0})
}

// TestBuildRowsReferencesCycle terminates on References chains that point at each other,
// because the chain is attacker-controlled and longer than a single parent.
func TestBuildRowsReferencesCycle(t *testing.T) {
	a := threadMessage(1, "a", "a", 1)
	a.References = []string{"b"}
	b := threadMessage(2, "b", "b", 2)
	b.References = []string{"a"}

	rows := BuildRows([]mail.Message{a, b})

	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2: %v", len(rows), rowUIDs(rows))
	}
	for i, depth := range rowDepths(rows) {
		if depth < 0 || depth > MaxThreadDepth {
			t.Errorf("row %d depth = %d, want 0..%d", i, depth, MaxThreadDepth)
		}
	}
}

// TestBuildRowsMissingParent proves a reply whose parent is not in the folder is
// visible as a root rather than being hidden. This is the M4 gate.
func TestBuildRowsMissingParent(t *testing.T) {
	orphan := threadMessage(1, "orphan", "<o>", 2, "<not-in-this-folder>")

	rows := BuildRows([]mail.Message{orphan})

	assertUIDs(t, rows, []imap.UID{1})
	assertDepths(t, rows, []int{0})
}

// TestBuildRowsDuplicateMessageID proves a duplicated Message-ID cannot drop or
// duplicate a row: the first message with the id becomes the parent and the
// duplicate stands on its own.
func TestBuildRowsDuplicateMessageID(t *testing.T) {
	first := threadMessage(1, "first", "<dup>", 1)
	duplicate := threadMessage(2, "duplicate", "<dup>", 2)
	child := threadMessage(3, "child", "<c>", 3, "<dup>")

	rows := BuildRows([]mail.Message{first, duplicate, child})

	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3: %v", len(rows), rowUIDs(rows))
	}

	seen := map[imap.UID]int{}
	for _, row := range rows {
		seen[row.Message.UID]++
	}
	for uid, count := range seen {
		if count != 1 {
			t.Errorf("UID %d appears %d times in the list, want once", uid, count)
		}
	}

	assertDepths(t, rows, []int{0, 1, 0})
}

// TestBuildRowsCycle terminates on In-Reply-To headers that point at each other.
// The header is attacker-controlled, so this must not recurse forever.
func TestBuildRowsCycle(t *testing.T) {
	a := threadMessage(1, "a", "<a>", 1, "<b>")
	b := threadMessage(2, "b", "<b>", 2, "<a>")

	rows := BuildRows([]mail.Message{a, b})

	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2: %v", len(rows), rowUIDs(rows))
	}
	for i, depth := range rowDepths(rows) {
		if depth < 0 || depth > MaxThreadDepth {
			t.Errorf("row %d depth = %d, want 0..%d", i, depth, MaxThreadDepth)
		}
	}
}

// TestBuildRowsBlocksByNewest proves block ordering: conversations ordered by
// their newest message, oldest to newest inside each conversation.
func TestBuildRowsBlocksByNewest(t *testing.T) {
	aRoot := threadMessage(1, "a root", "<a1>", 1)
	aReply := threadMessage(2, "a reply", "<a2>", 5, "<a1>")
	bRoot := threadMessage(3, "b root", "<b1>", 3)
	bReply := threadMessage(4, "b reply", "<b2>", 4, "<b1>")
	lone := threadMessage(5, "lone", "<l>", 2)

	rows := BuildRows([]mail.Message{lone, bReply, aReply, bRoot, aRoot})

	assertUIDs(t, rows, []imap.UID{1, 2, 3, 4, 5})
	assertDepths(t, rows, []int{0, 1, 0, 1, 0})

	if rows[0].Thread == rows[2].Thread {
		t.Error("two different conversations share a thread id")
	}
	if rows[4].Thread == rows[0].Thread || rows[4].Thread == rows[2].Thread {
		t.Error("the lone message shares a thread id with a conversation")
	}
}

// TestBuildRowsDepthCap proves a deep chain stops indenting at the cap instead of
// eating the subject column.
func TestBuildRowsDepthCap(t *testing.T) {
	messages := make([]mail.Message, 0, 6)
	messages = append(messages, threadMessage(1, "m1", "<m1>", 1))
	for i := 2; i <= 6; i++ {
		messages = append(messages, threadMessage(
			imap.UID(i),
			"m",
			"<m"+string(rune('0'+i))+">",
			i,
			"<m"+string(rune('0'+i-1))+">",
		))
	}

	rows := BuildRows(messages)

	if len(rows) != 6 {
		t.Fatalf("rows = %d, want 6", len(rows))
	}
	assertDepths(t, rows, []int{0, 1, 2, MaxThreadDepth, MaxThreadDepth, MaxThreadDepth})
}
