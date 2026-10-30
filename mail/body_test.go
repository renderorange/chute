package mail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mimeText joins lines with CRLF, the line ending MIME requires.
func mimeText(lines ...string) []byte {
	return []byte(strings.Join(lines, "\r\n"))
}

func TestParseBodyPlain(t *testing.T) {
	raw := mimeText(
		"From: sender@example.org",
		"Subject: hello",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"first line",
		"second line",
	)

	body, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody returned an error: %v", err)
	}
	if !strings.Contains(body.Text, "first line") || !strings.Contains(body.Text, "second line") {
		t.Errorf("Text = %q, want both lines", body.Text)
	}
	if len(body.Attachments) != 0 {
		t.Errorf("attachments = %d, want 0", len(body.Attachments))
	}
}

func TestParseBodyMultipartWithAttachments(t *testing.T) {
	raw := mimeText(
		`Content-Type: multipart/mixed; boundary="b1"`,
		"",
		"--b1",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"the text",
		"--b1",
		"Content-Type: application/pdf",
		`Content-Disposition: attachment; filename="report.pdf"`,
		"",
		"PDFDATA",
		"--b1",
		"Content-Type: image/png",
		`Content-Disposition: attachment; filename="chart.png"`,
		"",
		"PNGDATA",
		"--b1--",
		"",
	)

	body, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody returned an error: %v", err)
	}
	if !strings.Contains(body.Text, "the text") {
		t.Errorf("Text = %q, want the plain part", body.Text)
	}
	if len(body.Attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(body.Attachments))
	}
	if got, want := body.Attachments[0].Filename, "report.pdf"; got != want {
		t.Errorf("attachments[0] = %q, want %q", got, want)
	}
	if got := strings.TrimSpace(string(body.Attachments[0].Data)); got != "PDFDATA" {
		t.Errorf("attachments[0] data = %q, want PDFDATA", got)
	}
	if got, want := body.Attachments[1].Filename, "chart.png"; got != want {
		t.Errorf("attachments[1] = %q, want %q", got, want)
	}
}

func TestParseBodyNestedMultipart(t *testing.T) {
	raw := mimeText(
		`Content-Type: multipart/mixed; boundary="outer"`,
		"",
		"--outer",
		`Content-Type: multipart/alternative; boundary="inner"`,
		"",
		"--inner",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>nested html</p>",
		"--inner",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"nested plain",
		"--inner--",
		"--outer",
		"Content-Type: text/plain",
		`Content-Disposition: attachment; filename="notes.txt"`,
		"",
		"attached notes",
		"--outer--",
		"",
	)

	body, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody returned an error: %v", err)
	}
	if !strings.Contains(body.Text, "nested plain") {
		t.Errorf("Text = %q, want the nested plain part", body.Text)
	}
	if strings.Contains(body.Text, "nested html") {
		t.Errorf("Text = %q, want text/plain preferred over text/html", body.Text)
	}
	if len(body.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(body.Attachments))
	}
	if got, want := body.Attachments[0].Filename, "notes.txt"; got != want {
		t.Errorf("attachment = %q, want %q", got, want)
	}
}

func TestParseBodyHTMLOnly(t *testing.T) {
	raw := mimeText(
		"Content-Type: text/html; charset=utf-8",
		"",
		"<html><head><style>p{color:red}</style></head><body>"+
			"<p>Hello &amp; welcome</p><script>alert(1)</script><div>Second line</div>"+
			"</body></html>",
	)

	body, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody returned an error: %v", err)
	}
	for _, want := range []string{"Hello & welcome", "Second line"} {
		if !strings.Contains(body.Text, want) {
			t.Errorf("Text = %q, missing %q", body.Text, want)
		}
	}
	for _, unwanted := range []string{"color", "alert", "<p>", "<div>"} {
		if strings.Contains(body.Text, unwanted) {
			t.Errorf("Text = %q, still contains %q", body.Text, unwanted)
		}
	}
}

func TestParseBodyAttachmentWithoutFilename(t *testing.T) {
	raw := mimeText(
		`Content-Type: multipart/mixed; boundary="b1"`,
		"",
		"--b1",
		"Content-Type: application/octet-stream",
		"Content-Disposition: attachment",
		"",
		"RAW",
		"--b1--",
		"",
	)

	body, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody returned an error: %v", err)
	}
	if len(body.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(body.Attachments))
	}
	if got, want := body.Attachments[0].Filename, "attachment-1"; got != want {
		t.Errorf("filename = %q, want %q", got, want)
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tags removed", "<p>hello</p>", "hello"},
		{"breaks become newlines", "one<br>two", "one\ntwo"},
		{"entities decoded", "a &amp; b", "a & b"},
		{"script dropped", "<script>bad()</script>good", "good"},
		{"style dropped", "<style>x{}</style>good", "good"},
		{"crlf normalized", "a\r\nb", "a\nb"},
		{"blank runs collapsed", "a\n\n\n\nb", "a\n\nb"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripHTML(tc.in); got != tc.want {
				t.Errorf("stripHTML(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSafeFilename(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "passwd"},
		{"/tmp/x/../../y.txt", "y.txt"},
		{`..\..\evil.txt`, "evil.txt"},
		{"a/b/c.pdf", "c.pdf"},
		{"", "attachment"},
		{"...", "attachment"},
	}

	for _, tc := range tests {
		if got := safeFilename(tc.in); got != tc.want {
			t.Errorf("safeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSaveAttachment(t *testing.T) {
	dir := t.TempDir()
	attachment := Attachment{Filename: "report.pdf", Data: []byte("pdf bytes")}

	path, err := SaveAttachment(dir, attachment)
	if err != nil {
		t.Fatalf("SaveAttachment returned an error: %v", err)
	}
	if got, want := filepath.Dir(path), dir; got != want {
		t.Errorf("saved into %q, want %q", got, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the saved file: %v", err)
	}
	if got, want := string(data), "pdf bytes"; got != want {
		t.Errorf("saved contents = %q, want %q", got, want)
	}

	second, err := SaveAttachment(dir, attachment)
	if err != nil {
		t.Fatalf("second SaveAttachment returned an error: %v", err)
	}
	if second == path {
		t.Error("second save overwrote the first file")
	}
	if got, want := filepath.Base(second), "report-1.pdf"; got != want {
		t.Errorf("second filename = %q, want %q", got, want)
	}
}

func TestSaveAttachmentEmptyName(t *testing.T) {
	dir := t.TempDir()

	path, err := SaveAttachment(dir, Attachment{Data: []byte("x")})
	if err != nil {
		t.Fatalf("SaveAttachment returned an error: %v", err)
	}
	if got, want := filepath.Base(path), "attachment"; got != want {
		t.Errorf("filename = %q, want %q", got, want)
	}
}

func TestBodySectionUsesPeek(t *testing.T) {
	section := bodySection()

	if !section.Peek {
		t.Error("body section does not set Peek, opening a message would mark it seen")
	}
	if section.Specifier != "" {
		t.Errorf("Specifier = %v, want the whole message", section.Specifier)
	}
	if len(section.Part) != 0 {
		t.Errorf("Part = %v, want the whole message", section.Part)
	}
}
