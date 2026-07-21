package mailer

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

// The MIME is assembled by hand, so only a parser proves it. A malformed
// message is not rejected loudly — the recipient gets an unopenable attachment
// or a subject full of mojibake, and nothing errors anywhere.
func TestBuildProducesParseableMailWithIntactAttachment(t *testing.T) {
	pdf := bytes.Repeat([]byte{0x25, 0x50, 0x44, 0x46, 0xff, 0x00, 0xfe}, 5000) // binary, not text
	msg := Message{
		To:      []string{"ops@karlo.id", "manajemen@karlo.id"},
		Subject: "Laporan Mingguan — Armada",
		Body:    "Ringkasan minggu ini.\r\nLampiran terlampir.",
		Attachments: []Attachment{{
			Filename:    "Laporan-Mingguan.pdf",
			ContentType: "application/pdf",
			Body:        pdf,
		}},
	}

	raw, err := Build("reports@fms.mail.karlo.id", msg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("not a parseable message: %v", err)
	}

	// Non-ASCII subjects must be encoded, or servers mangle the header.
	if strings.Contains(m.Header.Get("Subject"), "—") {
		t.Error("subject was not encoded; an 8-bit header will be mangled in transit")
	}
	dec, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil || dec != msg.Subject {
		t.Errorf("subject round-trip failed: %q -> %q (%v)", msg.Subject, dec, err)
	}
	if to := m.Header.Get("To"); !strings.Contains(to, "ops@karlo.id") || !strings.Contains(to, "manajemen@karlo.id") {
		t.Errorf("To = %q, want both recipients", to)
	}

	mt, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mt, "multipart/") {
		t.Fatalf("content type = %q (%v)", mt, err)
	}

	mr := multipart.NewReader(m.Body, params["boundary"])
	var sawText bool
	var got []byte
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading parts: %v", err)
		}
		body, _ := io.ReadAll(p)
		switch {
		case strings.HasPrefix(p.Header.Get("Content-Type"), "text/plain"):
			sawText = true
		case p.Header.Get("Content-Transfer-Encoding") == "base64":
			// Every line must be within the RFC 2045 limit; over-long lines are
			// silently truncated by some servers, corrupting the file.
			for _, line := range strings.Split(strings.TrimSpace(string(body)), "\r\n") {
				if len(line) > 76 {
					t.Fatalf("base64 line of %d chars exceeds the 76-char limit", len(line))
				}
			}
			got, err = base64.StdEncoding.DecodeString(strings.ReplaceAll(string(body), "\r\n", ""))
			if err != nil {
				t.Fatalf("attachment is not valid base64: %v", err)
			}
		}
	}

	if !sawText {
		t.Error("no text part; the email would be a bare attachment")
	}
	if !bytes.Equal(got, pdf) {
		t.Errorf("attachment corrupted: %d bytes out, %d in", len(got), len(pdf))
	}
}

func TestBuildRejectsNoRecipients(t *testing.T) {
	if _, err := Build("a@b.c", Message{Subject: "x"}); err == nil {
		t.Error("a message with no recipients should not be built")
	}
}

func TestNewSESValidatesFromAddress(t *testing.T) {
	for _, from := range []string{"", "not-an-address", "a@b.c\nBcc: x@y.z"} {
		if _, err := NewSES(t.Context(), "ap-southeast-1", from); err == nil {
			t.Errorf("from=%q was accepted", from)
		}
	}
}
