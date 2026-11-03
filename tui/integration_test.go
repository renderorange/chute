package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/renderorange/chute/config"
	"github.com/renderorange/chute/mail"
)

// The rest of this package fakes at the loader interfaces, which is how a geometry
// defect reaches production unnoticed: a fake body is a string the test already owns.
// These tests run the real program — config, manager, client and model — against a real
// IMAP server, so the frame is measured on mail that came over the wire.

// mailServer is an in-process, memory-backed IMAP server on a loopback port.
type mailServer struct {
	addr string
	user *imapmemserver.User
}

// newMailServer starts a server with the mailboxes triage needs and closes it when the
// test ends.
func newMailServer(t *testing.T) *mailServer {
	t.Helper()

	backend := imapmemserver.New()
	user := imapmemserver.NewUser("tester", "secret")

	for _, name := range []string{"INBOX", "Trash", "Archive"} {
		if err := user.Create(name, nil); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	backend.AddUser(user)

	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return backend.NewSession(), nil, nil
		},
		Caps: imap.CapSet{
			imap.CapIMAP4rev1: {},
			imap.CapMove:      {},
			imap.CapUIDPlus:   {},
		},
		InsecureAuth: true,
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		server.Close()
		listener.Close()
	})

	return &mailServer{addr: listener.Addr().String(), user: user}
}

// appendMessage adds one message and returns its UID.
func (s *mailServer) appendMessage(t *testing.T, mailbox, subject, body string) imap.UID {
	t.Helper()

	raw := "From: Sender <sender@example.org>\r\n" +
		"To: tester@example.org\r\n" +
		"Subject: " + subject + "\r\n" +
		"Date: Mon, 02 Jan 2006 15:04:05 -0700\r\n" +
		"\r\n" +
		body + "\r\n"

	// imapmemserver dereferences the options pointer without a nil check.
	data, err := s.user.Append(mailbox, strings.NewReader(raw), &imap.AppendOptions{})
	if err != nil {
		t.Fatalf("append to %s: %v", mailbox, err)
	}

	return data.UID
}

// config builds the one-account config a client uses to reach this server.
func (s *mailServer) config(t *testing.T) *config.Config {
	t.Helper()

	host, portText, err := net.SplitHostPort(s.addr)
	if err != nil {
		t.Fatalf("split %q: %v", s.addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port %q: %v", portText, err)
	}

	return &config.Config{
		Accounts: []config.Account{{
			Name:    "memory",
			Default: true,
			IMAP: config.IMAP{
				Host:     host,
				Port:     port,
				Username: "tester",
				Password: "secret",
			},
		}},
	}
}

// longBodyText is n lines of prose, several times taller than any reader pane.
func longBodyText(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %03d of the body, with enough words that it has to wrap somewhere\n", i)
	}

	return b.String()
}

// driver walks a real model through a session: every command the model asks for is run
// synchronously and its message fed back, so no terminal and no timer is involved.
type driver struct {
	t *testing.T
	m Model
}

// newDriver builds the model the way main does and sizes it. The clock is disarmed so
// a test never waits on a real poll interval.
func newDriver(t *testing.T, width, height int, cfg *config.Config) *driver {
	t.Helper()

	m := New(cfg, "test", mail.WithPlaintextDial())
	m.clock = nil

	d := &driver{t: t, m: m}
	d.send(tea.WindowSizeMsg{Width: width, Height: height})
	d.drain(m.Init(), 0)

	return d
}

// send delivers one message and settles every command it produces.
func (d *driver) send(msg tea.Msg) {
	d.sendDepth(msg, 0)
}

// sendDepth bounds the chain, so a command that re-queues itself fails loudly instead
// of spinning.
func (d *driver) sendDepth(msg tea.Msg, depth int) {
	d.t.Helper()

	if depth > 50 {
		d.t.Fatal("the command chain did not settle")
	}

	next, cmd := d.m.Update(msg)
	d.m = next.(Model)
	d.drain(cmd, depth+1)
}

// drain runs a command and delivers whatever it produces, flattening a batch.
func (d *driver) drain(cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 50 {
		return
	}

	out := cmd()
	if out == nil {
		return
	}
	if batch, ok := out.(tea.BatchMsg); ok {
		for _, inner := range batch {
			d.drain(inner, depth+1)
		}

		return
	}

	d.sendDepth(out, depth+1)
}

// frame renders the model and asserts the geometry every frame must hold: exactly the
// terminal's height in lines, each exactly the terminal's width in cells. A frame that
// breaks either is what makes a terminal scroll and duplicate a row.
func (d *driver) frame(label string) []string {
	d.t.Helper()

	lines := strings.Split(d.m.View(), "\n")
	if len(lines) != d.m.height {
		d.t.Fatalf("%s: %d lines, want %d", label, len(lines), d.m.height)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != d.m.width {
			d.t.Fatalf("%s: line %d is %d cells, want %d", label, i, got, d.m.width)
		}
	}

	return lines
}

// listWindow is the message rows the list draws right now, exactly as rendered.
func (d *driver) listWindow() []string {
	d.t.Helper()

	width := d.m.layout().Messages.Width - (2 * PanelPaddingHorizontal)

	return d.m.messageLines(d.m.messageRows(), width)
}

// TestSessionAgainstInProcessServer drives the real program end to end: the start folder
// opens, a long message is fetched from the server and read, and going back to the list
// leaves the frame and the list window untouched.
//
// This is the harness issue #44 needs. The reported symptom is a list row moving and
// repeating when a long body is open, which no fake-loader test can be trusted about,
// because a fake body is a string the test chose.
func TestSessionAgainstInProcessServer(t *testing.T) {
	const width, height = 130, 40

	server := newMailServer(t)
	body := longBodyText(300)
	for i := 1; i <= 60; i++ {
		server.appendMessage(t, "INBOX", fmt.Sprintf("subject %02d", i), body)
	}

	d := newDriver(t, width, height, server.config(t))

	if d.m.openFolder != "INBOX" {
		t.Fatalf("start folder = %q, want INBOX opened at startup", d.m.openFolder)
	}
	if got := len(d.m.messages); got != 60 {
		t.Fatalf("loaded %d messages from the server, want 60", got)
	}
	d.frame("opened INBOX")

	// Walk down so the list itself has to scroll: the shift in #44 is reported while
	// the list is scrolled, not on a list that fits whole. Opening a folder already
	// focuses the list (openFolderByName), so no tab is needed here.
	if d.m.focus != focusMessages {
		t.Fatalf("focus = %v, want the message list after the start folder opened", d.m.focus)
	}
	for i := 0; i < 40; i++ {
		d.send(namedKey("down"))
	}
	d.frame("scrolled list")

	scrollBefore := d.m.messageScroll
	windowBefore := d.listWindow()
	if scrollBefore == 0 {
		t.Fatal("the list did not scroll, so this test cannot observe a shift")
	}

	d.send(namedKey("enter"))
	if d.m.focus != focusReader {
		t.Fatalf("focus = %v, want the reader after enter", d.m.focus)
	}
	view := strings.Join(d.frame("reader open"), "\n")
	if !strings.Contains(view, "line 001 of the body") {
		t.Error("the reader is not showing the body that came from the server")
	}
	if _, ok := d.m.bodies[d.m.selectedBodyKey()]; !ok {
		t.Fatal("the body was not cached, so enter fetched nothing")
	}

	d.send(namedKey("esc"))
	d.frame("back in the list")

	if d.m.focus != focusMessages {
		t.Errorf("focus = %v, want the message list after esc", d.m.focus)
	}
	if got := d.m.messageScroll; got != scrollBefore {
		t.Errorf("message scroll = %d after esc, want %d: the list moved under the user", got, scrollBefore)
	}

	after := d.listWindow()
	if len(after) != len(windowBefore) {
		t.Fatalf("the list draws %d rows after esc, want %d", len(after), len(windowBefore))
	}
	for i := range after {
		if after[i] != windowBefore[i] {
			t.Errorf("list row %d = %q after esc, want %q: the window moved under the user", i, after[i], windowBefore[i])

			break
		}
	}

	seen := make(map[string]bool, len(after))
	for _, row := range after {
		if seen[row] {
			t.Errorf("list row %q is drawn twice", row)
		}
		seen[row] = true
	}
}
