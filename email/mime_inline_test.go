package email

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExtractPlainTextFromMIME_Multipart(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"To: b@example.com\r\n" +
		"Subject: Test\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"BOUNDARY\"\r\n" +
		"\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"Hallo Welt =E2=98=83\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>Hallo Welt</p>\r\n" +
		"--BOUNDARY--\r\n"

	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	got := ExtractPlainTextFromMIME(encoded)
	if !strings.Contains(got, "Hallo Welt") {
		t.Fatalf("expected plain part to contain 'Hallo Welt', got %q", got)
	}
	if !strings.Contains(got, "☃") {
		t.Fatalf("expected quoted-printable to be decoded to ☃, got %q", got)
	}
}

func TestExtractPlainTextFromMIME_PlainOnly(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"einfach plain\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	got := ExtractPlainTextFromMIME(encoded)
	if !strings.Contains(got, "einfach plain") {
		t.Fatalf("expected 'einfach plain', got %q", got)
	}
}

func TestExtractPlainTextFromMIME_HTMLOnlyReturnsEmpty(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>x</p>\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	if got := ExtractPlainTextFromMIME(encoded); got != "" {
		t.Fatalf("expected empty string for html-only, got %q", got)
	}
}

func TestExtractPlainTextFromMIME_ISO8859_1_QuotedPrintable(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"Content-Type: text/plain; charset=\"iso-8859-1\"\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"Wir k=F6nnen Sie nicht erreichen\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	got := ExtractPlainTextFromMIME(encoded)
	if !utf8.ValidString(got) {
		t.Fatalf("result must be valid UTF-8, got %q", got)
	}
	if !strings.Contains(got, "können") {
		t.Fatalf("expected umlaut to decode to 'können', got %q", got)
	}
}

func TestExtractPlainTextFromMIME_Windows1252_Base64(t *testing.T) {
	// 0x46 0xFC 0x72 = "Für" in windows-1252.
	body := []byte{0x46, 0xFC, 0x72}
	b64 := base64.StdEncoding.EncodeToString(body)
	raw := "From: a@example.com\r\n" +
		"Content-Type: text/plain; charset=\"windows-1252\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" + b64 + "\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	got := ExtractPlainTextFromMIME(encoded)
	if !utf8.ValidString(got) {
		t.Fatalf("result must be valid UTF-8, got %q", got)
	}
	if !strings.Contains(got, "Für") {
		t.Fatalf("expected 'Für', got %q", got)
	}
}

func TestExtractPlainTextFromMIME_UTF16LE_BOM_Base64(t *testing.T) {
	// UTF-16LE with BOM, content "Hi ☃".
	bom := []byte{0xFF, 0xFE}
	body := []byte{
		'H', 0x00, 'i', 0x00, ' ', 0x00, // "Hi "
		0x03, 0x26, // ☃ (U+2603) little-endian
	}
	full := append(bom, body...)
	b64 := base64.StdEncoding.EncodeToString(full)
	raw := "From: a@example.com\r\n" +
		"Content-Type: text/plain; charset=\"utf-16le\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" + b64 + "\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	got := ExtractPlainTextFromMIME(encoded)
	if !utf8.ValidString(got) {
		t.Fatalf("result must be valid UTF-8, got %q", got)
	}
	if !strings.Contains(got, "Hi") || !strings.Contains(got, "☃") {
		t.Fatalf("expected 'Hi ☃', got %q", got)
	}
}

func TestExtractPlainTextFromMIME_NoCharset_DefaultsToUTF8(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Schöne Grüße\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	got := ExtractPlainTextFromMIME(encoded)
	if !utf8.ValidString(got) {
		t.Fatalf("result must be valid UTF-8, got %q", got)
	}
	if !strings.Contains(got, "Schöne Grüße") {
		t.Fatalf("expected pass-through 'Schöne Grüße', got %q", got)
	}
}

func TestExtractPlainTextFromMIME_UnknownCharset_FallbackSanitizes(t *testing.T) {
	// Bytes that aren't valid UTF-8: 0xFC ('ü' in 1252) embedded as raw.
	// Charset label "cp437" — Lookup may or may not return an encoding, but the
	// contract is: result must be valid UTF-8 either way.
	raw := "From: a@example.com\r\n" +
		"Content-Type: text/plain; charset=\"cp437\"\r\n" +
		"\r\n" +
		string([]byte{'F', 0xFC, 'r'}) + "\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	got := ExtractPlainTextFromMIME(encoded)
	if !utf8.ValidString(got) {
		t.Fatalf("fallback must produce valid UTF-8, got %q (%v)", got, []byte(got))
	}
}

func TestSanitizeUTF8_StripsInvalidBytes(t *testing.T) {
	in := string([]byte{'a', 0xFC, 'b'})
	got := sanitizeUTF8(in)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitizeUTF8 must produce valid UTF-8, got %q", got)
	}
	if got != "ab" {
		t.Fatalf("expected invalid byte stripped → \"ab\", got %q", got)
	}
}

func TestExtractPlainTextFromMIME_SkipsAttachment(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"B\"\r\n" +
		"\r\n" +
		"--B\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Disposition: attachment; filename=\"note.txt\"\r\n" +
		"\r\n" +
		"this is an attachment\r\n" +
		"--B\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"the real body\r\n" +
		"--B--\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	got := ExtractPlainTextFromMIME(encoded)
	if !strings.Contains(got, "the real body") {
		t.Fatalf("expected real body, got %q", got)
	}
	if strings.Contains(got, "attachment") {
		t.Fatalf("attachment text leaked into plain body: %q", got)
	}
}
