package email

// mime_inline.go — replaces cid: references in HTML bodies with data URIs
// extracted from the full MIME content of a message.
//
// Exchange returns ErrorInternalServerError for GetAttachment on inline images
// inside meeting invitations. Fetching the full MIME via IncludeMimeContent=true
// and parsing it locally is the only reliable alternative.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"

	"golang.org/x/net/html/charset"
)

// sanitizeUTF8 strips bytes that aren't valid UTF-8 so the result is safe to
// hand to Postgres (which rejects invalid sequences with SQLSTATE 22021).
// Returns "" for empty input.
func sanitizeUTF8(s string) string {
	if s == "" {
		return s
	}
	return strings.ToValidUTF8(s, "")
}

// decodeCharset converts raw bytes from `label` (a MIME charset label like
// "iso-8859-1", "windows-1252", "utf-16le") into a UTF-8 string.
// Falls back to sanitizeUTF8 when the label is unknown or the conversion fails.
func decodeCharset(raw []byte, label string) string {
	label = strings.TrimSpace(strings.ToLower(label))
	if label == "" || label == "utf-8" || label == "utf8" || label == "us-ascii" || label == "ascii" {
		return sanitizeUTF8(string(raw))
	}
	enc, _ := charset.Lookup(label)
	if enc == nil {
		slog.Warn("mail: charset decode fallback", "charset", label, "reason", "unknown label")
		return sanitizeUTF8(string(raw))
	}
	decoded, err := io.ReadAll(enc.NewDecoder().Reader(bytes.NewReader(raw)))
	if err != nil {
		slog.Warn("mail: charset decode fallback", "charset", label, "err", err)
		return sanitizeUTF8(string(raw))
	}
	return sanitizeUTF8(string(decoded))
}

// replaceCIDsWithDataURLs parses rawMIMEBase64 (a base64-encoded full RFC 2822
// MIME message as returned by Exchange), extracts every part that carries a
// Content-ID header, and replaces matching cid:... references in htmlBody with
// inline data URIs.  htmlBody is returned unchanged on any parse failure.
func replaceCIDsWithDataURLs(htmlBody, rawMIMEBase64 string) string {
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(rawMIMEBase64), ""))
	if err != nil {
		return htmlBody
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return htmlBody
	}

	cidMap := make(map[string]string) // contentID → "data:type;base64,..."
	walkMIMEParts(textproto.MIMEHeader(msg.Header), msg.Body, cidMap)

	slog.Debug("mail: replaceCIDsWithDataURLs", "cids_found", len(cidMap))
	if len(cidMap) == 0 {
		return htmlBody
	}

	result := htmlBody
	replaced := 0
	for cid, dataURL := range cidMap {
		before := result
		result = strings.ReplaceAll(result, "cid:"+cid, dataURL)
		if result != before {
			replaced++
		}
	}
	slog.Debug("mail: replaceCIDsWithDataURLs — done", "cids_replaced", replaced, "cids_found", len(cidMap))
	return result
}

// walkMIMEParts recurses into multipart bodies and collects Content-ID parts.
func walkMIMEParts(header textproto.MIMEHeader, body io.Reader, cidMap map[string]string) {
	ct := header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			walkMIMEParts(textproto.MIMEHeader(part.Header), part, cidMap)
		}
		return
	}

	// Only collect parts with a Content-ID (inline images/resources).
	contentID := strings.Trim(header.Get("Content-Id"), "<>")
	if contentID == "" {
		return
	}

	raw, err := io.ReadAll(body)
	if err != nil {
		return
	}

	var decoded []byte
	switch strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding"))) {
	case "base64":
		decoded, err = base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(raw)), ""))
		if err != nil {
			return
		}
	case "quoted-printable":
		decoded, err = io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw)))
		if err != nil {
			return
		}
	default:
		decoded = raw
	}

	cidMap[contentID] = fmt.Sprintf("data:%s;base64,%s",
		mediaType, base64.StdEncoding.EncodeToString(decoded))
}

// ExtractPlainTextFromMIME parses rawMIMEBase64 (a base64-encoded full RFC 2822
// MIME message as returned by Exchange) and returns the first text/plain part it
// finds, decoded according to its Content-Transfer-Encoding. Returns an empty
// string if no plain part is present or parsing fails.
func ExtractPlainTextFromMIME(rawMIMEBase64 string) string {
	if rawMIMEBase64 == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(rawMIMEBase64), ""))
	if err != nil {
		return ""
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	return findPlainPart(textproto.MIMEHeader(msg.Header), msg.Body)
}

// findPlainPart walks a MIME tree and returns the decoded body of the first
// text/plain part it encounters. Skips parts marked as attachments.
func findPlainPart(header textproto.MIMEHeader, body io.Reader) string {
	ct := header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return ""
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				return ""
			}
			if s := findPlainPart(textproto.MIMEHeader(part.Header), part); s != "" {
				return s
			}
		}
	}

	if mediaType != "text/plain" {
		return ""
	}
	disposition := strings.ToLower(header.Get("Content-Disposition"))
	if strings.HasPrefix(disposition, "attachment") {
		return ""
	}

	raw, err := io.ReadAll(body)
	if err != nil {
		return ""
	}
	var decoded []byte
	switch strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding"))) {
	case "base64":
		decoded, err = base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(raw)), ""))
		if err != nil {
			return ""
		}
	case "quoted-printable":
		decoded, err = io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw)))
		if err != nil {
			return ""
		}
	default:
		decoded = raw
	}
	return decodeCharset(decoded, params["charset"])
}

// reListUnsubHTTP matches an http(s) URL inside angle brackets in List-Unsubscribe.
var reListUnsubHTTP = regexp.MustCompile(`<(https?://[^>]+)>`)

// reListUnsubMailto matches a mailto: URL inside angle brackets.
var reListUnsubMailto = regexp.MustCompile(`<(mailto:[^>]+)>`)

// ParseListUnsubscribe extracts a List-Unsubscribe URL from a base64-encoded
// raw MIME message (as returned by Exchange). Returns empty string on failure.
// Prefers http(s) URLs over mailto: links.
func ParseListUnsubscribe(rawMIMEBase64 string) string {
	if rawMIMEBase64 == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(rawMIMEBase64), ""))
	if err != nil {
		return ""
	}
	// Only scan the headers section (before the first blank line).
	headers := string(raw)
	if idx := strings.Index(headers, "\r\n\r\n"); idx != -1 {
		headers = headers[:idx]
	} else if idx := strings.Index(headers, "\n\n"); idx != -1 {
		headers = headers[:idx]
	}

	// Unfold header continuation lines (RFC 2822 §2.2.3).
	unfolded := strings.ReplaceAll(headers, "\r\n ", " ")
	unfolded = strings.ReplaceAll(unfolded, "\r\n\t", " ")
	unfolded = strings.ReplaceAll(unfolded, "\n ", " ")
	unfolded = strings.ReplaceAll(unfolded, "\n\t", " ")

	var headerVal string
	for _, line := range strings.Split(unfolded, "\n") {
		if strings.HasPrefix(strings.ToLower(line), "list-unsubscribe:") {
			headerVal = strings.TrimSpace(line[len("list-unsubscribe:"):])
			break
		}
	}
	if headerVal == "" {
		return ""
	}
	if m := reListUnsubHTTP.FindStringSubmatch(headerVal); len(m) >= 2 {
		return m[1]
	}
	if m := reListUnsubMailto.FindStringSubmatch(headerVal); len(m) >= 2 {
		return m[1]
	}
	return ""
}
