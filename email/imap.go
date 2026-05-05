package email

import (
	"encoding/base64"
	"time"
)

// EncodeItemID encodes a raw EWS item ID to a URL-safe base64url string.
// Exchange item IDs are standard base64 and contain '/' and '+', which break
// URL path routing. All IDs returned by the API are encoded; callers must
// pass the encoded form back, and handlers decode before calling EWS.
func EncodeItemID(rawID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(rawID))
}

// DecodeItemID decodes a base64url-encoded EWS item ID back to the raw form.
func DecodeItemID(encoded string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EmailSummary is returned for folder listings.
type EmailSummary struct {
	UID            uint32     `json:"uid"`                      // 0 for EWS (use ewsid instead)
	EWSID          string     `json:"ewsid"`                    // EWS ItemId (string)
	Subject        string     `json:"subject"`
	SenderName     string     `json:"sender_name"`
	SenderEmail    string     `json:"sender_email"`
	ToRecipients   []string   `json:"to_recipients,omitempty"`
	Received       time.Time  `json:"received"`
	IsRead         bool       `json:"is_read"`
	HasAttachments bool       `json:"has_attachments"`
	IsFlagged      bool       `json:"is_flagged"`
	Categories     []string   `json:"categories,omitempty"`    // LLM-assigned category keys
	ConversationID string     `json:"conversation_id,omitempty"`
	IsArchived     bool       `json:"is_archived,omitempty"`
	SnoozedUntil  *time.Time `json:"snoozed_until,omitempty"`
	Labels         []string   `json:"labels,omitempty"`        // user-defined label keys
}

type Attachment struct {
	EWSID       string `json:"ewsid"`                  // base64url-encoded EWS AttachmentId
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	ContentID   string `json:"content_id,omitempty"`   // cid: reference for inline images
	IsInline    bool   `json:"is_inline,omitempty"`
}

// AttachmentContent is the binary payload returned by GetAttachment.
type AttachmentContent struct {
	Filename    string
	ContentType string
	Data        []byte
}

// DraftResult is returned when a draft is saved.
type DraftResult struct {
	EWSID string `json:"ewsid"` // base64url-encoded EWS ItemId of the new draft
}

type EmailDetail struct {
	EmailSummary
	Body                   string       `json:"body"`
	BodyHTML               string       `json:"body_html"`
	Attachments            []Attachment `json:"attachments"`
	UnsubscribeURL         string       `json:"unsubscribe_url,omitempty"`
	IsReadReceiptRequested bool         `json:"read_receipt_requested,omitempty"`
}

// VacationSettings represents the Exchange Out-Of-Office configuration.
type VacationSettings struct {
	Enabled          bool   `json:"enabled"`
	State            string `json:"state"`                      // Disabled|Enabled|Scheduled
	ExternalAudience string `json:"external_audience,omitempty"` // None|Known|All
	StartTime        string `json:"start_time,omitempty"`        // ISO8601
	EndTime          string `json:"end_time,omitempty"`          // ISO8601
	InternalMessage  string `json:"internal_message,omitempty"`
	ExternalMessage  string `json:"external_message,omitempty"`
}

// MailLabel is a user-defined label for organising emails.
type MailLabel struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt string `json:"created_at,omitempty"`
}

// Thread is a conversation thread (group of emails with same ConversationId).
type Thread struct {
	ConversationID string     `json:"conversation_id"`
	Subject        string     `json:"subject"`
	MessageCount   int        `json:"message_count"`
	HasUnread      bool       `json:"has_unread"`
	LatestReceived time.Time  `json:"latest_received"`
	LatestSender   string     `json:"latest_sender"`
}

type InboxResult struct {
	Emails      []EmailSummary `json:"emails"`
	Total       int            `json:"total"`
	UnreadCount int            `json:"unread_count"`
}

type TestResult struct {
	Success     bool   `json:"success"`
	Email       string `json:"email"`
	InboxCount  int    `json:"inbox_count"`
	UnreadCount int    `json:"unread_count"`
}
