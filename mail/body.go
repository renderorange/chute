package mail

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
)

// Attachment is one file carried by a message, decoded into memory.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Body is the readable part of a message: its text and its attachments.
type Body struct {
	Text        string
	Attachments []Attachment
}

// HTML stripping is deliberately crude: v1 renders no HTML, only its text.
var (
	scriptStylePattern = regexp.MustCompile(`(?is)<(script|style)\b[^>]*>.*?</(script|style)>`)
	blockTagPattern    = regexp.MustCompile(`(?i)<(?:br|/p|/div|/tr|/li|/h[1-6])\b[^>]*>`)
	tagPattern         = regexp.MustCompile(`(?s)<[^>]*>`)
	blankRunPattern    = regexp.MustCompile(`\n{3,}`)
)

// bodySection is the BODY.PEEK[] data item: the whole message, without setting
// the \Seen flag. Reading a message in M2b is not a triage action.
func bodySection() *imap.FetchItemBodySection {
	return &imap.FetchItemBodySection{Peek: true}
}

// FetchBody downloads one message and decodes its text and attachments. It opens
// the folder first, because M2b still uses a connection per operation.
func (c *Client) FetchBody(folder string, uid imap.UID) (*Body, error) {
	raw, err := c.fetchRaw(folder, uid)
	if err != nil {
		return nil, err
	}

	return parseBody(raw)
}

// fetchRaw downloads one message's bytes without setting \Seen.
func (c *Client) fetchRaw(folder string, uid imap.UID) ([]byte, error) {
	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("mail: account %q: opening %q: %w", c.account, folder, err)
	}

	section := bodySection()
	cmd := c.conn.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{section},
	})
	defer cmd.Close()

	buffers, err := cmd.Collect()
	if err != nil {
		return nil, fmt.Errorf("mail: account %q: fetching message %d from %q: %w", c.account, uid, folder, err)
	}
	if len(buffers) == 0 {
		return nil, fmt.Errorf("mail: account %q: message %d is not in %q", c.account, uid, folder)
	}

	raw := buffers[0].FindBodySection(section)
	if raw == nil {
		return nil, fmt.Errorf("mail: account %q: message %d in %q returned no body", c.account, uid, folder)
	}

	return raw, nil
}

// Append stores msg in folder with the given flags. It reports the new UID and whether
// the server supplied one: APPENDUID is a UIDPLUS extension, and without it the caller
// has no way to name the message it just wrote.
func (c *Client) Append(folder string, flags []imap.Flag, msg []byte) (imap.UID, bool, error) {
	cmd := c.conn.Append(folder, int64(len(msg)), &imap.AppendOptions{Flags: flags})

	if _, err := cmd.Write(msg); err != nil {
		cmd.Close()
		return 0, false, fmt.Errorf("mail: account %q: appending to %q: %w", c.account, folder, err)
	}
	if err := cmd.Close(); err != nil {
		return 0, false, fmt.Errorf("mail: account %q: appending to %q: %w", c.account, folder, err)
	}

	data, err := cmd.Wait()
	if err != nil {
		return 0, false, fmt.Errorf("mail: account %q: appending to %q: %w", c.account, folder, err)
	}

	return data.UID, data.UID != 0, nil
}

// parseBody walks a raw RFC 5322 message and keeps its text and attachments.
// It prefers text/plain and falls back to text/html tag-stripped to text.
func parseBody(raw []byte) (*Body, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, fmt.Errorf("mail: reading the message: %w", err)
	}
	if entity == nil {
		return nil, errors.New("mail: reading the message: no content")
	}

	body := &Body{}
	var htmlText string
	unnamed := 0

	err = entity.Walk(func(path []int, part *message.Entity, walkErr error) error {
		if walkErr != nil && !message.IsUnknownCharset(walkErr) && !message.IsUnknownEncoding(walkErr) {
			return walkErr
		}
		if part == nil || part.MultipartReader() != nil {
			return nil
		}

		contentType, params, _ := part.Header.ContentType()
		disposition, dispParams, _ := part.Header.ContentDisposition()

		filename := dispParams["filename"]
		if filename == "" {
			filename = params["name"]
		}

		if disposition == "attachment" || filename != "" {
			data, readErr := io.ReadAll(part.Body)
			if readErr != nil {
				return readErr
			}
			if filename == "" {
				unnamed++
				filename = fmt.Sprintf("attachment-%d", unnamed)
			}
			body.Attachments = append(body.Attachments, Attachment{
				Filename:    filename,
				ContentType: contentType,
				Data:        data,
			})

			return nil
		}

		switch contentType {
		case "text/plain":
			if body.Text != "" {
				return nil
			}
			data, readErr := io.ReadAll(part.Body)
			if readErr != nil {
				return readErr
			}
			body.Text = string(data)
		case "text/html":
			if htmlText != "" {
				return nil
			}
			data, readErr := io.ReadAll(part.Body)
			if readErr != nil {
				return readErr
			}
			htmlText = string(data)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("mail: parsing the message body: %w", err)
	}

	if strings.TrimSpace(body.Text) == "" && htmlText != "" {
		body.Text = stripHTML(htmlText)
	}

	return body, nil
}

// stripHTML turns HTML into crude plain text: scripts and styles are dropped,
// block boundaries become newlines, entities are decoded, and blank runs are
// collapsed.
func stripHTML(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = scriptStylePattern.ReplaceAllString(s, "")
	s = blockTagPattern.ReplaceAllString(s, "\n")
	s = tagPattern.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = blankRunPattern.ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}

// SaveAttachment writes one attachment into dir and returns its path. It never
// overwrites: a repeated name gains a -1, -2 suffix. The filename is reduced to
// a base name so a crafted attachment cannot write outside dir.
func SaveAttachment(dir string, attachment Attachment) (string, error) {
	if dir == "" {
		return "", errors.New("mail: no download directory configured")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mail: creating %s: %w", dir, err)
	}

	base := safeFilename(attachment.Filename)
	for attempt := 0; attempt < 1000; attempt++ {
		path := filepath.Join(dir, numberedName(base, attempt))

		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("mail: creating %s: %w", path, err)
		}

		if _, err := file.Write(attachment.Data); err != nil {
			file.Close()
			os.Remove(path)
			return "", fmt.Errorf("mail: writing %s: %w", path, err)
		}
		if err := file.Close(); err != nil {
			os.Remove(path)
			return "", fmt.Errorf("mail: writing %s: %w", path, err)
		}

		return path, nil
	}

	return "", fmt.Errorf("mail: saving %s: too many files with that name", base)
}

// numberedName adds a -N suffix before the extension for attempt > 0.
func numberedName(base string, attempt int) string {
	if attempt == 0 {
		return base
	}

	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	return fmt.Sprintf("%s-%d%s", stem, attempt, ext)
}

// safeFilename reduces a sender-supplied name to a single safe path element.
func safeFilename(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0:
			return '_'
		}

		return r
	}, name)
	name = strings.TrimLeft(name, ".")
	if name == "" {
		return "attachment"
	}

	return name
}
