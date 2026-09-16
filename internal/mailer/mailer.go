// Package mailer sends email with attachments.
//
// The interface exists so the rest of the system never learns which transport
// is behind it. SES is what runs today; SMTP is a config change rather than a
// rewrite, and tests get a recorder instead of a network.
//
// MIME is assembled by hand rather than pulled from a library: a report email
// is one text part and one attachment, which is a small enough shape that a
// dependency would cost more than it saves.
package mailer

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	qp "mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// Attachment is a file to send.
type Attachment struct {
	Filename    string
	ContentType string
	Body        []byte
}

// Message is what callers build.
type Message struct {
	To          []string
	Subject     string
	Body        string // plain text
	Attachments []Attachment
}

// Sender delivers a message. Implementations must be safe for concurrent use.
type Sender interface {
	Send(ctx context.Context, m Message) error
	// From is the address messages will appear to come from, so a caller can
	// show it without knowing the transport.
	From() string
}

// maxMessageBytes guards the SES v2 limit (40 MB after base64). Base64 inflates
// by about a third, so the raw cap sits below it with room for headers.
//
// Caught here rather than at the API: a rejected 41 MB send costs a full report
// render first, and the error SES returns does not say which attachment.
const maxMessageBytes = 28 << 20

// ---- SES --------------------------------------------------------------------

type sesSender struct {
	client *sesv2.Client
	from   string
}

// NewSES builds an SES sender. A missing region or credentials returns an
// error rather than a client that fails on first send, so a misconfiguration
// shows up at startup.
func NewSES(ctx context.Context, region, from string) (Sender, error) {
	if strings.TrimSpace(from) == "" {
		return nil, fmt.Errorf("mailer: from address is required")
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("mailer: invalid from address %q: %w", from, err)
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("mailer: aws config: %w", err)
	}
	return &sesSender{client: sesv2.NewFromConfig(cfg), from: from}, nil
}

func (s *sesSender) From() string { return s.from }

func (s *sesSender) Send(ctx context.Context, m Message) error {
	raw, err := Build(s.from, m)
	if err != nil {
		return err
	}
	if len(raw) > maxMessageBytes {
		return fmt.Errorf("message is %d MB, over the %d MB limit — try CSV, a shorter period, or fewer sections",
			len(raw)>>20, maxMessageBytes>>20)
	}
	_, err = s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(s.from),
		Destination:      &sestypes.Destination{ToAddresses: m.To},
		Content:          &sestypes.EmailContent{Raw: &sestypes.RawMessage{Data: raw}},
	})
	if err != nil {
		return fmt.Errorf("ses: %w", err)
	}
	return nil
}

// ---- MIME -------------------------------------------------------------------

// Build assembles a raw RFC 5322 message. Exported so it can be tested without
// a network, and so an SMTP transport can reuse it verbatim.
func Build(from string, m Message) ([]byte, error) {
	if len(m.To) == 0 {
		return nil, fmt.Errorf("mailer: no recipients")
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	var head bytes.Buffer
	fmt.Fprintf(&head, "From: %s\r\n", from)
	fmt.Fprintf(&head, "To: %s\r\n", strings.Join(m.To, ", "))
	// Encode the subject: report names carry non-ASCII (Indonesian titles), and
	// a raw 8-bit subject line is mangled by some servers rather than rejected.
	fmt.Fprintf(&head, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", m.Subject))
	head.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&head, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", w.Boundary())

	textPart, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return nil, err
	}
	if _, err := textPart.Write(quotedPrintable(m.Body)); err != nil {
		return nil, err
	}

	for _, a := range m.Attachments {
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		part, err := w.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {ct},
			"Content-Transfer-Encoding": {"base64"},
			// The filename is encoded too — a report called "Laporan Mingguan"
			// is fine, but one with an em dash is not plain ASCII.
			"Content-Disposition": {fmt.Sprintf("attachment; filename=%q", mime.QEncoding.Encode("utf-8", a.Filename))},
		})
		if err != nil {
			return nil, err
		}
		if err := writeBase64Wrapped(part, a.Body); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return append(head.Bytes(), buf.Bytes()...), nil
}

// writeBase64Wrapped encodes at 76 characters per line.
//
// RFC 2045 caps a line at 76 characters, and some servers reject or silently
// truncate longer ones — which corrupts the attachment rather than failing the
// send, so the recipient gets an unopenable file and no error anywhere.
func writeBase64Wrapped(w io.Writer, body []byte) error {
	const lineLen = 76
	enc := base64.StdEncoding.EncodeToString(body)
	for i := 0; i < len(enc); i += lineLen {
		end := i + lineLen
		if end > len(enc) {
			end = len(enc)
		}
		if _, err := io.WriteString(w, enc[i:end]+"\r\n"); err != nil {
			return err
		}
	}
	return nil
}

// quotedPrintable encodes the text body.
func quotedPrintable(s string) []byte {
	var buf bytes.Buffer
	w := qp.NewWriter(&buf)
	_, _ = io.WriteString(w, s)
	_ = w.Close()
	return buf.Bytes()
}

// ---- recorder, for tests and for running without a transport ----------------
