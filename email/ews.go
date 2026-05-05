// Package email provides access to Exchange mailboxes via EWS (Exchange Web Services).
//
// DHBW Ravensburg runs on-premise Exchange (EXMAIL02-W2K16) behind a reverse proxy.
// The EWS endpoint is https://webmail.dhbw-ravensburg.de/EWS/Exchange.asmx and
// requires NTLM authentication.
//
// Required env (optional overrides):
//
//	EWS_URL — base EWS URL (default derived from imap_server field, e.g. https://webmail.dhbw-ravensburg.de/EWS/Exchange.asmx)
package email

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/go-ntlmssp"
)

// ErrAuthFailed is returned when EWS rejects credentials (HTTP 401).
// Callers should clear the stored password when they see this error.
var ErrAuthFailed = errors.New("EWS authentication failed")

// ewsClient holds connection parameters for one EWS session.
type ewsClient struct {
	endpoint string
	client   *http.Client
}

func newEWSClient(server, username, password string) *ewsClient {
	endpoint := ewsEndpoint(server)
	transport := ntlmssp.Negotiator{
		RoundTripper: &http.Transport{},
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &ntlmAuthTransport{
			base:     transport,
			username: username,
			password: password,
		},
	}
	return &ewsClient{endpoint: endpoint, client: client}
}

// ntlmAuthTransport injects credentials into every request for NTLM negotiation.
type ntlmAuthTransport struct {
	base     ntlmssp.Negotiator
	username string
	password string
}

func (t *ntlmAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(t.username, t.password)
	return t.base.RoundTrip(req)
}

func ewsEndpoint(server string) string {
	server = strings.TrimRight(server, "/")
	if strings.HasPrefix(server, "http") {
		return server + "/EWS/Exchange.asmx"
	}
	return "https://" + server + "/EWS/Exchange.asmx"
}

// ewsFolderID maps frontend-friendly folder names to EWS DistinguishedFolderId values.
func ewsFolderID(folder string) string {
	switch strings.ToLower(folder) {
	case "sent", "sentitems":
		return "sentitems"
	case "drafts":
		return "drafts"
	case "deleted", "deleteditems", "trash":
		return "deleteditems"
	case "junk", "spam", "junkemail":
		return "junkemail"
	default:
		return "inbox"
	}
}

// buildMailboxList renders a slice of email addresses as EWS <t:Mailbox> elements.
func buildMailboxList(addrs []string) string {
	var b strings.Builder
	for _, addr := range addrs {
		b.WriteString(fmt.Sprintf(`<t:Mailbox><t:EmailAddress>%s</t:EmailAddress></t:Mailbox>`, xmlEscape(addr)))
	}
	return b.String()
}

func (c *ewsClient) do(soapBody, operation string) ([]byte, error) {
	envelope := `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages">
  <soap:Header>
    <t:RequestServerVersion Version="Exchange2010_SP2"/>
  </soap:Header>
  <soap:Body>` + soapBody + `</soap:Body>
</soap:Envelope>`

	req, err := http.NewRequest("POST", c.endpoint, bytes.NewBufferString(envelope))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", `"http://schemas.microsoft.com/exchange/services/2006/messages/`+operation+`"`)

	slog.Debug("ews: sending request", "endpoint", c.endpoint)

	resp, err := c.client.Do(req)
	if err != nil {
		slog.Error("ews: HTTP request failed", "endpoint", c.endpoint, "err", err)
		return nil, fmt.Errorf("EWS request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read EWS response: %w", err)
	}

	slog.Debug("ews: response received", "status", resp.StatusCode, "bytes", len(body))

	if resp.StatusCode == 401 {
		slog.Warn("ews: authentication failed (401)", "endpoint", c.endpoint)
		return nil, fmt.Errorf("authentication failed (NTLM): %w", ErrAuthFailed)
	}
	if resp.StatusCode != 200 {
		preview := string(body)
		if len(preview) > 4000 {
			preview = preview[:4000]
		}
		slog.Warn("ews: unexpected HTTP status", "status", resp.StatusCode, "body_preview", preview)
		return nil, fmt.Errorf("EWS returned HTTP %d", resp.StatusCode)
	}
	return body, nil
}

// ── XML response types ────────────────────────────────────────────────────────

type ewsEnvelope struct {
	Body ewsBody `xml:"Body"`
}

type ewsBody struct {
	FindItemResponse            *findItemResponse            `xml:"FindItemResponse"`
	GetItemResponse             *getItemResponse             `xml:"GetItemResponse"`
	GetFolderResponse           *getFolderResponse           `xml:"GetFolderResponse"`
	UpdateItemResponse          *updateItemResponse          `xml:"UpdateItemResponse"`
	DeleteItemResponse          *deleteItemResponse          `xml:"DeleteItemResponse"`
	CreateItemResponse          *createItemResponse          `xml:"CreateItemResponse"`
	GetAttachmentResponse       *getAttachmentResponse       `xml:"GetAttachmentResponse"`
	MoveItemResponse            *moveItemResponse            `xml:"MoveItemResponse"`
	CreateAttachmentResponse    *createAttachmentResponse    `xml:"CreateAttachmentResponse"`
	SendItemResponse            *sendItemResponse            `xml:"SendItemResponse"`
	GetUserOofSettingsResponse  *getUserOofSettingsResponse  `xml:"GetUserOofSettingsResponse"`
	SetUserOofSettingsResponse  *setUserOofSettingsResponse  `xml:"SetUserOofSettingsResponse"`
}

type findItemResponse struct {
	Messages []findItemResponseMessage `xml:"ResponseMessages>FindItemResponseMessage"`
}

type findItemResponseMessage struct {
	ResponseClass string     `xml:"ResponseClass,attr"`
	MessageText   string     `xml:"MessageText"`
	RootFolder    rootFolder `xml:"RootFolder"`
}

type rootFolder struct {
	TotalItemsInView int       `xml:"TotalItemsInView,attr"`
	Items            []ewsItem `xml:"Items>Message"`
}

type ewsConversationID struct {
	ID string `xml:"Id,attr"`
}

type ewsItem struct {
	ItemID         ewsItemID          `xml:"ItemId"`
	ConversationID ewsConversationID  `xml:"ConversationId"`
	Subject        string             `xml:"Subject"`
	From           ewsMailbox         `xml:"From>Mailbox"`
	ToRecipients   []ewsMailbox       `xml:"ToRecipients>Mailbox"`
	DateTimeSent   string             `xml:"DateTimeSent"`
	IsRead         bool               `xml:"IsRead"`
	HasAttachments bool               `xml:"HasAttachments"`
}

type ewsItemID struct {
	ID        string `xml:"Id,attr"`
	ChangeKey string `xml:"ChangeKey,attr"`
}

type ewsMailbox struct {
	Name         string `xml:"Name"`
	EmailAddress string `xml:"EmailAddress"`
}

type getFolderResponse struct {
	Messages []getFolderResponseMessage `xml:"ResponseMessages>GetFolderResponseMessage"`
}

type getFolderResponseMessage struct {
	Folders []ewsFolder `xml:"Folders>Folder"`
}

type ewsFolder struct {
	TotalCount  int `xml:"TotalCount"`
	UnreadCount int `xml:"UnreadCount"`
}

type getItemResponse struct {
	Messages []getItemResponseMessage `xml:"ResponseMessages>GetItemResponseMessage"`
}

type getItemResponseMessage struct {
	ResponseClass string        `xml:"ResponseClass,attr"`
	MessageText   string        `xml:"MessageText"`
	Items         []ewsFullItem `xml:"Items>Message"`
}

type ewsFullItem struct {
	ewsItem
	Body                   ewsBodyContent `xml:"Body"`
	MIMEContent            string         `xml:"MimeContent"` // base64-encoded full MIME message
	Attachments            []ewsAttach    `xml:"Attachments>FileAttachment"`
	IsReadReceiptRequested bool           `xml:"IsReadReceiptRequested"`
}

type ewsBodyContent struct {
	BodyType string `xml:"BodyType,attr"`
	Content  string `xml:",chardata"`
}

type ewsAttach struct {
	AttachmentID ewsAttachID `xml:"AttachmentId"`
	Name         string      `xml:"Name"`
	ContentType  string      `xml:"ContentType"`
	Size         int         `xml:"Size"`
	ContentID    string      `xml:"ContentId"`
	IsInline     bool        `xml:"IsInline"`
}

type ewsAttachID struct {
	ID string `xml:"Id,attr"`
}

// ── OOF (Vacation responder) types ───────────────────────────────────────────

type getUserOofSettingsResponse struct {
	ResponseMessage struct {
		ResponseClass string `xml:"ResponseClass,attr"`
		MessageText   string `xml:"MessageText"`
	} `xml:"ResponseMessage"`
	OofSettings ewsOofSettings `xml:"OofSettings"`
}

type setUserOofSettingsResponse struct {
	ResponseMessage struct {
		ResponseClass string `xml:"ResponseClass,attr"`
		MessageText   string `xml:"MessageText"`
	} `xml:"ResponseMessage"`
}

type ewsOofSettings struct {
	OofState         string `xml:"OofState"`
	ExternalAudience string `xml:"ExternalAudience"`
	Duration         struct {
		StartTime string `xml:"StartTime"`
		EndTime   string `xml:"EndTime"`
	} `xml:"Duration"`
	InternalReply struct{ Message string `xml:"Message"` } `xml:"InternalReply"`
	ExternalReply struct{ Message string `xml:"Message"` } `xml:"ExternalReply"`
}

type createItemResponse struct {
	Messages []createItemResponseMessage `xml:"ResponseMessages>CreateItemResponseMessage"`
}

type createItemResponseMessage struct {
	ResponseClass string          `xml:"ResponseClass,attr"`
	MessageText   string          `xml:"MessageText"`
	Items         []ewsCreatedMsg `xml:"Items>Message"`
}

type ewsCreatedMsg struct {
	ItemID ewsItemID `xml:"ItemId"`
}

type updateItemResponse struct {
	Messages []updateItemMsg `xml:"ResponseMessages>UpdateItemResponseMessage"`
}
type updateItemMsg struct {
	ResponseClass string       `xml:"ResponseClass,attr"`
	MessageText   string       `xml:"MessageText"`
	Items         []ewsItemRef `xml:"Items>Message"`
}
type ewsItemRef struct {
	ItemID ewsItemID `xml:"ItemId"`
}
type deleteItemResponse struct{}

// ── Public API ────────────────────────────────────────────────────────────────

// GetFolderUnreadCount returns the unread message count for a folder.
// This is a lightweight call (GetFolder only — no message listing).
func GetFolderUnreadCount(server, username, password, folder string) (int, error) {
	c := newEWSClient(server, username, password)
	_, unread, err := c.folderStats(ewsFolderID(folder))
	return unread, err
}

// TestConnection verifies EWS credentials and returns inbox stats.
func TestConnection(server, username, password string) (*TestResult, error) {
	slog.Debug("ews: testing connection", "server", server, "user", username)
	c := newEWSClient(server, username, password)

	total, unread, err := c.folderStats("inbox")
	if err != nil {
		slog.Warn("ews: connection test failed", "server", server, "user", username, "err", err)
		return nil, err
	}
	slog.Info("ews: connection ok", "server", server, "user", username, "total", total, "unread", unread)
	return &TestResult{
		Success:     true,
		Email:       username,
		InboxCount:  total,
		UnreadCount: unread,
	}, nil
}

// GetMessages returns a page of emails from the given folder, newest first.
// folder maps to EWS DistinguishedFolderId via ewsFolderID().
func GetMessages(server, username, password, folder string, limit, offset int) (*InboxResult, error) {
	folderEWS := ewsFolderID(folder)
	slog.Debug("ews: GetMessages", "server", server, "user", username, "folder", folderEWS, "limit", limit, "offset", offset)
	c := newEWSClient(server, username, password)

	total, unread, err := c.folderStats(folderEWS)
	if err != nil {
		slog.Warn("ews: GetMessages folderStats failed", "folder", folderEWS, "err", err)
		return nil, err
	}

	items, err := c.findItems(folderEWS, limit, offset)
	if err != nil {
		slog.Warn("ews: GetMessages findItems failed", "folder", folderEWS, "err", err)
		return nil, err
	}

	emails := make([]EmailSummary, 0, len(items))
	for _, item := range items {
		emails = append(emails, summaryFromEWS(item))
	}
	return &InboxResult{Emails: emails, Total: total, UnreadCount: unread}, nil
}

// GetEmailByID fetches a single email with full body by its EWS ItemID string.
func GetEmailByID(server, username, password, itemID string) (*EmailDetail, error) {
	c := newEWSClient(server, username, password)
	return c.getItem(itemID)
}

// MarkReadByID marks an email as read by EWS ItemID string.
func MarkReadByID(server, username, password, itemID string) error {
	c := newEWSClient(server, username, password)
	return c.markRead(itemID)
}

// DeleteEmailByID deletes an email by EWS ItemID string.
func DeleteEmailByID(server, username, password, itemID string) error {
	c := newEWSClient(server, username, password)
	return c.deleteItem(itemID)
}

// SendEmail composes and sends a new email via EWS CreateItem.
func SendEmail(server, username, password string, to, cc, bcc []string, subject, body string, html bool) error {
	slog.Debug("ews: SendEmail", "server", server, "user", username, "to", to, "subject", subject)
	c := newEWSClient(server, username, password)
	return c.sendNewEmail(to, cc, bcc, subject, body, bodyTypeStr(html))
}

// ReplyEmail sends a reply to an existing email.
// If all is true, replies to all recipients.
func ReplyEmail(server, username, password, itemID, body string, html, all bool) error {
	slog.Debug("ews: ReplyEmail", "server", server, "user", username, "all", all)
	c := newEWSClient(server, username, password)
	return c.replyItem(itemID, body, bodyTypeStr(html), all)
}

// ForwardEmail forwards an existing email to new recipients.
func ForwardEmail(server, username, password, itemID string, to []string, body string, html bool) error {
	slog.Debug("ews: ForwardEmail", "server", server, "user", username, "to", to)
	c := newEWSClient(server, username, password)
	return c.forwardItem(itemID, to, body, bodyTypeStr(html))
}

// ── EWS operations ────────────────────────────────────────────────────────────

func (c *ewsClient) folderStats(folderEWS string) (total, unread int, err error) {
	body, err := c.do(fmt.Sprintf(`
<m:GetFolder>
  <m:FolderShape>
    <t:BaseShape>AllProperties</t:BaseShape>
  </m:FolderShape>
  <m:FolderIds>
    <t:DistinguishedFolderId Id="%s"/>
  </m:FolderIds>
</m:GetFolder>`, folderEWS), "GetFolder")
	if err != nil {
		return 0, 0, err
	}

	var env ewsEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		slog.Error("ews: GetFolder XML parse failed", "err", err, "body_preview", truncate(string(body), 300))
		return 0, 0, fmt.Errorf("parse GetFolder response: %w", err)
	}
	if env.Body.GetFolderResponse == nil || len(env.Body.GetFolderResponse.Messages) == 0 {
		return 0, 0, fmt.Errorf("empty GetFolder response")
	}
	msgs := env.Body.GetFolderResponse.Messages[0]
	if len(msgs.Folders) == 0 {
		return 0, 0, nil
	}
	f := msgs.Folders[0]
	return f.TotalCount, f.UnreadCount, nil
}

func (c *ewsClient) findItems(folderEWS string, limit, offset int) ([]ewsItem, error) {
	body, err := c.do(fmt.Sprintf(`
<m:FindItem Traversal="Shallow">
  <m:ItemShape>
    <t:BaseShape>IdOnly</t:BaseShape>
    <t:AdditionalProperties>
      <t:FieldURI FieldURI="item:Subject"/>
      <t:FieldURI FieldURI="item:DateTimeSent"/>
      <t:FieldURI FieldURI="message:IsRead"/>
      <t:FieldURI FieldURI="item:HasAttachments"/>
      <t:FieldURI FieldURI="message:From"/>
      <t:FieldURI FieldURI="message:ToRecipients"/>
      <t:FieldURI FieldURI="item:ConversationId"/>
    </t:AdditionalProperties>
  </m:ItemShape>
  <m:IndexedPageItemView MaxEntriesReturned="%d" Offset="%d" BasePoint="Beginning"/>
  <m:SortOrder>
    <t:FieldOrder Order="Descending">
      <t:FieldURI FieldURI="item:DateTimeSent"/>
    </t:FieldOrder>
  </m:SortOrder>
  <m:ParentFolderIds>
    <t:DistinguishedFolderId Id="%s"/>
  </m:ParentFolderIds>
</m:FindItem>`, limit, offset, folderEWS), "FindItem")
	if err != nil {
		return nil, err
	}

	var env ewsEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse FindItem response: %w", err)
	}
	if env.Body.FindItemResponse == nil || len(env.Body.FindItemResponse.Messages) == 0 {
		return nil, nil
	}
	msg := env.Body.FindItemResponse.Messages[0]
	if msg.ResponseClass != "Success" {
		return nil, fmt.Errorf("FindItem failed: %s", msg.MessageText)
	}
	return msg.RootFolder.Items, nil
}

func (c *ewsClient) getFullItem(itemID string) (*ewsFullItem, error) {
	body, err := c.do(fmt.Sprintf(`
<m:GetItem>
  <m:ItemShape>
    <t:BaseShape>AllProperties</t:BaseShape>
    <t:IncludeMimeContent>true</t:IncludeMimeContent>
    <t:BodyType>HTML</t:BodyType>
  </m:ItemShape>
  <m:ItemIds>
    <t:ItemId Id="%s"/>
  </m:ItemIds>
</m:GetItem>`, xmlEscape(itemID)), "GetItem")
	if err != nil {
		return nil, err
	}

	var env ewsEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse GetItem response: %w", err)
	}
	if env.Body.GetItemResponse == nil || len(env.Body.GetItemResponse.Messages) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	msg := env.Body.GetItemResponse.Messages[0]
	if msg.ResponseClass != "Success" || len(msg.Items) == 0 {
		return nil, fmt.Errorf("message not found: %s", msg.MessageText)
	}
	item := msg.Items[0]
	return &item, nil
}

func (c *ewsClient) getItem(itemID string) (*EmailDetail, error) {
	item, err := c.getFullItem(itemID)
	if err != nil {
		return nil, err
	}
	return detailFromFullItem(item), nil
}

// detailFromFullItem converts an EWS full item response into our EmailDetail shape.
func detailFromFullItem(item *ewsFullItem) *EmailDetail {
	detail := &EmailDetail{
		EmailSummary:           summaryFromEWS(item.ewsItem),
		Attachments:            []Attachment{},
		IsReadReceiptRequested: item.IsReadReceiptRequested,
	}
	if item.Body.BodyType == "HTML" {
		detail.BodyHTML = sanitizeUTF8(item.Body.Content)
	} else {
		detail.Body = sanitizeUTF8(item.Body.Content)
	}
	// Replace cid: references with inline data URIs extracted from the full MIME.
	// This handles meeting invitations where GetAttachment fails for inline images.
	if item.MIMEContent != "" && strings.Contains(detail.BodyHTML, "cid:") {
		detail.BodyHTML = sanitizeUTF8(replaceCIDsWithDataURLs(detail.BodyHTML, item.MIMEContent))
	}
	if item.MIMEContent != "" {
		detail.UnsubscribeURL = ParseListUnsubscribe(item.MIMEContent)
		// EWS returns either HTML or plain depending on requested BodyType — pull
		// the plain alternative out of the MIME so we can store both forms.
		if detail.Body == "" {
			detail.Body = ExtractPlainTextFromMIME(item.MIMEContent)
		}
	}
	for _, a := range item.Attachments {
		detail.Attachments = append(detail.Attachments, Attachment{
			EWSID:       EncodeItemID(a.AttachmentID.ID),
			Filename:    a.Name,
			ContentType: a.ContentType,
			Size:        a.Size,
			ContentID:   a.ContentID,
			IsInline:    a.IsInline,
		})
		detail.HasAttachments = true
	}
	return detail
}

// getFullItemsBatch fetches multiple full items in a single GetItem request.
// rawIDs are decoded EWS ItemIds (not the base64url-encoded form).
func (c *ewsClient) getFullItemsBatch(rawIDs []string) ([]ewsFullItem, error) {
	if len(rawIDs) == 0 {
		return nil, nil
	}
	var ids strings.Builder
	for _, id := range rawIDs {
		fmt.Fprintf(&ids, `<t:ItemId Id="%s"/>`, xmlEscape(id))
	}
	body, err := c.do(fmt.Sprintf(`
<m:GetItem>
  <m:ItemShape>
    <t:BaseShape>AllProperties</t:BaseShape>
    <t:IncludeMimeContent>true</t:IncludeMimeContent>
    <t:BodyType>HTML</t:BodyType>
  </m:ItemShape>
  <m:ItemIds>%s</m:ItemIds>
</m:GetItem>`, ids.String()), "GetItem")
	if err != nil {
		return nil, err
	}

	var env ewsEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse GetItem batch response: %w", err)
	}
	if env.Body.GetItemResponse == nil {
		return nil, fmt.Errorf("empty GetItem batch response")
	}
	out := make([]ewsFullItem, 0, len(rawIDs))
	for _, msg := range env.Body.GetItemResponse.Messages {
		if msg.ResponseClass != "Success" || len(msg.Items) == 0 {
			continue
		}
		out = append(out, msg.Items[0])
	}
	return out, nil
}

// BatchFetchError describes one failure during a batched body fetch.
type BatchFetchError struct {
	BatchIndex int      // 0-based batch number within the run
	IDs        []string // raw EWS ItemIds in the failed batch (after splitting: 1 entry)
	Err        error
}

// BatchFetchResult is the outcome of GetEmailsByIDs.
// Succeeded maps base64url-encoded EWSID → fully-populated EmailDetail.
// FailedIDs are the raw EWS ItemIds that could not be fetched even after retry+split.
// Errors carries one entry per batch (or per single ID after split) that failed.
type BatchFetchResult struct {
	Succeeded map[string]*EmailDetail
	FailedIDs []string
	Errors    []BatchFetchError
}

// batchSleep is the pause between successive EWS batches to avoid throttling.
// Made variable so tests can shrink it.
var batchSleep = 250 * time.Millisecond

// batchRetryDelay is the wait before retrying a failed batch once.
var batchRetryDelay = 500 * time.Millisecond

// itemFetcher is the interface GetEmailsByIDs uses to call EWS — abstracted so
// tests can inject a fake without spinning up an HTTP server.
type itemFetcher interface {
	getFullItemsBatch(rawIDs []string) ([]ewsFullItem, error)
}

// GetEmailsByIDs fetches multiple emails with full body in batched EWS GetItem calls.
//
// Robustness: a batch failure does NOT abort the run. On error the batch is
// retried once after batchRetryDelay; if it still fails, each ItemId in the
// batch is fetched on its own so a single bad ID doesn't poison the rest.
// HTTP 503 / ErrorServerBusy triggers an extra exponential backoff between
// batches to ride out throttling.
func GetEmailsByIDs(server, username, password string, rawIDs []string) BatchFetchResult {
	res := BatchFetchResult{Succeeded: map[string]*EmailDetail{}}
	if len(rawIDs) == 0 {
		return res
	}
	c := newEWSClient(server, username, password)
	return fetchEmailsByIDs(c, rawIDs, &res)
}

// fetchEmailsByIDs is the testable core: takes any itemFetcher.
// `res` is mutated in place and also returned for convenience.
func fetchEmailsByIDs(c itemFetcher, rawIDs []string, res *BatchFetchResult) BatchFetchResult {
	const batchSize = 20
	throttleBackoff := time.Duration(0)
	batchIndex := 0

	for start := 0; start < len(rawIDs); start += batchSize {
		end := start + batchSize
		if end > len(rawIDs) {
			end = len(rawIDs)
		}
		batch := rawIDs[start:end]

		if start > 0 {
			time.Sleep(batchSleep + throttleBackoff)
		}

		items, err := c.getFullItemsBatch(batch)
		if err != nil {
			slog.Warn("mail: body batch error",
				"batch_index", batchIndex,
				"ids_count", len(batch),
				"first_id", firstID(batch),
				"err", err,
			)
			// Retry once after a short pause — covers transient timeouts.
			time.Sleep(batchRetryDelay)
			items, err = c.getFullItemsBatch(batch)
			if err != nil {
				slog.Warn("mail: body batch retry failed — splitting",
					"batch_index", batchIndex,
					"ids_count", len(batch),
					"err", err,
				)
				// Bump throttle backoff if Exchange is asking us to slow down.
				if isThrottleError(err) {
					if throttleBackoff == 0 {
						throttleBackoff = 500 * time.Millisecond
					} else if throttleBackoff < 8*time.Second {
						throttleBackoff *= 2
					}
				}
				// Last resort: split — fetch each ID individually so we isolate
				// the bad one(s) and still collect the good neighbours.
				for _, id := range batch {
					time.Sleep(batchSleep)
					singleItems, sErr := c.getFullItemsBatch([]string{id})
					if sErr != nil || len(singleItems) == 0 {
						res.FailedIDs = append(res.FailedIDs, id)
						res.Errors = append(res.Errors, BatchFetchError{
							BatchIndex: batchIndex,
							IDs:        []string{id},
							Err:        sErr,
						})
						continue
					}
					d := detailFromFullItem(&singleItems[0])
					res.Succeeded[d.EWSID] = d
				}
				batchIndex++
				continue
			}
			// Retry succeeded — fall through to consume `items`.
			res.Errors = append(res.Errors, BatchFetchError{
				BatchIndex: batchIndex,
				IDs:        append([]string(nil), batch...),
				Err:        nil, // recovered on retry
			})
		} else if throttleBackoff > 0 {
			// Successful batch after a throttled one — taper the backoff.
			throttleBackoff /= 2
		}

		for i := range items {
			d := detailFromFullItem(&items[i])
			res.Succeeded[d.EWSID] = d
		}
		batchIndex++
	}
	return *res
}

func firstID(batch []string) string {
	if len(batch) == 0 {
		return ""
	}
	id := batch[0]
	if len(id) > 32 {
		return id[:32] + "…"
	}
	return id
}

// isThrottleError reports whether the EWS error indicates we should back off.
// Matches HTTP 503 and the ErrorServerBusy SOAP fault.
func isThrottleError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 503") ||
		strings.Contains(msg, "ErrorServerBusy") ||
		strings.Contains(msg, "ServerBusy")
}

func (c *ewsClient) markRead(itemID string) error {
	_, err := c.do(fmt.Sprintf(`
<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite">
  <m:ItemChanges>
    <t:ItemChange>
      <t:ItemId Id="%s"/>
      <t:Updates>
        <t:SetItemField>
          <t:FieldURI FieldURI="message:IsRead"/>
          <t:Message><t:IsRead>true</t:IsRead></t:Message>
        </t:SetItemField>
      </t:Updates>
    </t:ItemChange>
  </m:ItemChanges>
</m:UpdateItem>`, xmlEscape(itemID)), "UpdateItem")
	return err
}

func (c *ewsClient) deleteItem(itemID string) error {
	_, err := c.do(fmt.Sprintf(`
<m:DeleteItem DeleteType="MoveToDeletedItems">
  <m:ItemIds>
    <t:ItemId Id="%s"/>
  </m:ItemIds>
</m:DeleteItem>`, xmlEscape(itemID)), "DeleteItem")
	return err
}

func (c *ewsClient) sendNewEmail(to, cc, bcc []string, subject, body, bodyType string) error {
	ccBlock := ""
	if len(cc) > 0 {
		ccBlock = fmt.Sprintf(`<t:CcRecipients>%s</t:CcRecipients>`, buildMailboxList(cc))
	}
	bccBlock := ""
	if len(bcc) > 0 {
		bccBlock = fmt.Sprintf(`<t:BccRecipients>%s</t:BccRecipients>`, buildMailboxList(bcc))
	}

	resp, err := c.do(fmt.Sprintf(`
<m:CreateItem MessageDisposition="SendAndSaveCopy">
  <m:SavedItemFolderId>
    <t:DistinguishedFolderId Id="sentitems"/>
  </m:SavedItemFolderId>
  <m:Items>
    <t:Message>
      <t:Subject>%s</t:Subject>
      <t:Body BodyType="%s">%s</t:Body>
      <t:ToRecipients>%s</t:ToRecipients>
      %s
      %s
    </t:Message>
  </m:Items>
</m:CreateItem>`, xmlEscape(subject), bodyType, xmlEscape(body), buildMailboxList(to), ccBlock, bccBlock), "CreateItem")
	if err != nil {
		return err
	}
	return checkCreateItemResponse(resp)
}

func (c *ewsClient) replyItem(itemID, body, bodyType string, all bool) error {
	original, err := c.getFullItem(itemID)
	if err != nil {
		return fmt.Errorf("get original for reply: %w", err)
	}

	element := "ReplyToItem"
	if all {
		element = "ReplyAllToItem"
	}

	resp, err := c.do(fmt.Sprintf(`
<m:CreateItem MessageDisposition="SendAndSaveCopy">
  <m:SavedItemFolderId>
    <t:DistinguishedFolderId Id="sentitems"/>
  </m:SavedItemFolderId>
  <m:Items>
    <t:%s>
      <t:ReferenceItemId Id="%s" ChangeKey="%s"/>
      <t:NewBodyContent BodyType="%s">%s</t:NewBodyContent>
    </t:%s>
  </m:Items>
</m:CreateItem>`, element, xmlEscape(original.ItemID.ID), xmlEscape(original.ItemID.ChangeKey), bodyType, xmlEscape(body), element), "CreateItem")
	if err != nil {
		return err
	}
	return checkCreateItemResponse(resp)
}

func (c *ewsClient) forwardItem(itemID string, to []string, body, bodyType string) error {
	original, err := c.getFullItem(itemID)
	if err != nil {
		return fmt.Errorf("get original for forward: %w", err)
	}

	resp, err := c.do(fmt.Sprintf(`
<m:CreateItem MessageDisposition="SendAndSaveCopy">
  <m:SavedItemFolderId>
    <t:DistinguishedFolderId Id="sentitems"/>
  </m:SavedItemFolderId>
  <m:Items>
    <t:ForwardItem>
      <t:ReferenceItemId Id="%s" ChangeKey="%s"/>
      <t:NewBodyContent BodyType="%s">%s</t:NewBodyContent>
      <t:ToRecipients>%s</t:ToRecipients>
    </t:ForwardItem>
  </m:Items>
</m:CreateItem>`, xmlEscape(original.ItemID.ID), xmlEscape(original.ItemID.ChangeKey), bodyType, xmlEscape(body), buildMailboxList(to)), "CreateItem")
	if err != nil {
		return err
	}
	return checkCreateItemResponse(resp)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func checkCreateItemResponse(data []byte) error {
	var env ewsEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("parse CreateItem response: %w", err)
	}
	if env.Body.CreateItemResponse == nil {
		return nil
	}
	msgs := env.Body.CreateItemResponse.Messages
	if len(msgs) > 0 && msgs[0].ResponseClass != "Success" {
		return fmt.Errorf("EWS CreateItem failed: %s", msgs[0].MessageText)
	}
	return nil
}

func summaryFromEWS(item ewsItem) EmailSummary {
	s := EmailSummary{
		UID:            0,
		EWSID:          EncodeItemID(item.ItemID.ID),
		Subject:        item.Subject,
		SenderName:     item.From.Name,
		SenderEmail:    item.From.EmailAddress,
		IsRead:         item.IsRead,
		HasAttachments: item.HasAttachments,
		ConversationID: item.ConversationID.ID,
	}
	if s.Subject == "" {
		s.Subject = "(Kein Betreff)"
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, item.DateTimeSent); err == nil {
			s.Received = t.UTC()
			break
		}
	}
	if len(item.ToRecipients) > 0 {
		s.ToRecipients = make([]string, 0, len(item.ToRecipients))
		for _, mb := range item.ToRecipients {
			if mb.EmailAddress != "" {
				s.ToRecipients = append(s.ToRecipients, mb.EmailAddress)
			} else if mb.Name != "" {
				s.ToRecipients = append(s.ToRecipients, mb.Name)
			}
		}
	}
	return s
}

func bodyTypeStr(html bool) string {
	if html {
		return "HTML"
	}
	return "Text"
}

func (c *ewsClient) markUnread(itemID string) error {
	_, err := c.do(fmt.Sprintf(`
<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite">
  <m:ItemChanges>
    <t:ItemChange>
      <t:ItemId Id="%s"/>
      <t:Updates>
        <t:SetItemField>
          <t:FieldURI FieldURI="message:IsRead"/>
          <t:Message><t:IsRead>false</t:IsRead></t:Message>
        </t:SetItemField>
      </t:Updates>
    </t:ItemChange>
  </m:ItemChanges>
</m:UpdateItem>`, xmlEscape(itemID)), "UpdateItem")
	return err
}

// updateDraftMeta updates cc/bcc/subject/read-receipt on a saved draft item.
// Returns the new ChangeKey (empty if unchanged or Exchange didn't return one).
func (c *ewsClient) updateDraftMeta(itemID string, cc, bcc []string, subject string, readReceipt bool) (string, error) {
	var updates strings.Builder
	if subject != "" {
		updates.WriteString(fmt.Sprintf(`
    <t:SetItemField>
      <t:FieldURI FieldURI="item:Subject"/>
      <t:Message><t:Subject>%s</t:Subject></t:Message>
    </t:SetItemField>`, xmlEscape(subject)))
	}
	if len(cc) > 0 {
		updates.WriteString(fmt.Sprintf(`
    <t:SetItemField>
      <t:FieldURI FieldURI="message:CcRecipients"/>
      <t:Message><t:CcRecipients>%s</t:CcRecipients></t:Message>
    </t:SetItemField>`, buildMailboxList(cc)))
	}
	if len(bcc) > 0 {
		updates.WriteString(fmt.Sprintf(`
    <t:SetItemField>
      <t:FieldURI FieldURI="message:BccRecipients"/>
      <t:Message><t:BccRecipients>%s</t:BccRecipients></t:Message>
    </t:SetItemField>`, buildMailboxList(bcc)))
	}
	if readReceipt {
		updates.WriteString(`
    <t:SetItemField>
      <t:FieldURI FieldURI="message:IsReadReceiptRequested"/>
      <t:Message><t:IsReadReceiptRequested>true</t:IsReadReceiptRequested></t:Message>
    </t:SetItemField>`)
	}
	if updates.Len() == 0 {
		return "", nil
	}
	resp, err := c.do(fmt.Sprintf(`
<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite">
  <m:ItemChanges>
    <t:ItemChange>
      <t:ItemId Id="%s"/>
      <t:Updates>%s
      </t:Updates>
    </t:ItemChange>
  </m:ItemChanges>
</m:UpdateItem>`, xmlEscape(itemID), updates.String()), "UpdateItem")
	if err != nil {
		return "", err
	}
	var env ewsEnvelope
	if err := xml.Unmarshal(resp, &env); err != nil {
		return "", nil // non-fatal: ChangeKey update optional
	}
	if env.Body.UpdateItemResponse != nil && len(env.Body.UpdateItemResponse.Messages) > 0 {
		m := env.Body.UpdateItemResponse.Messages[0]
		if m.ResponseClass != "Success" {
			return "", fmt.Errorf("UpdateItem failed: %s", m.MessageText)
		}
		if len(m.Items) > 0 {
			return m.Items[0].ItemID.ChangeKey, nil
		}
	}
	return "", nil
}

// sendDraftItem sends an already-created draft item.
func (c *ewsClient) sendDraftItem(itemID, changeKey string) error {
	resp, err := c.do(fmt.Sprintf(`
<m:SendItem SaveItemToFolder="true">
  <m:ItemIds>
    <t:ItemId Id="%s" ChangeKey="%s"/>
  </m:ItemIds>
  <m:SavedItemFolderId>
    <t:DistinguishedFolderId Id="sentitems"/>
  </m:SavedItemFolderId>
</m:SendItem>`, xmlEscape(itemID), xmlEscape(changeKey)), "SendItem")
	if err != nil {
		return err
	}
	var env ewsEnvelope
	if err := xml.Unmarshal(resp, &env); err != nil {
		return nil // Exchange sometimes returns empty body on success
	}
	if env.Body.SendItemResponse != nil && len(env.Body.SendItemResponse.Messages) > 0 {
		if m := env.Body.SendItemResponse.Messages[0]; m.ResponseClass != "Success" {
			return fmt.Errorf("SendItem failed: %s", m.MessageText)
		}
	}
	return nil
}

// replyItemFull creates a reply draft, optionally adds cc/bcc/subject/attachments, then sends.
func (c *ewsClient) replyItemFull(itemID, body, bodyType string, all bool, cc, bcc []string, subject string, readReceipt bool, attachments []OutboundAttachment) error {
	original, err := c.getFullItem(itemID)
	if err != nil {
		return fmt.Errorf("get original for reply: %w", err)
	}

	element := "ReplyToItem"
	if all {
		element = "ReplyAllToItem"
	}

	// Step 1: create reply draft (SaveOnly so we can modify it before sending)
	resp, err := c.do(fmt.Sprintf(`
<m:CreateItem MessageDisposition="SaveOnly">
  <m:SavedItemFolderId>
    <t:DistinguishedFolderId Id="drafts"/>
  </m:SavedItemFolderId>
  <m:Items>
    <t:%s>
      <t:ReferenceItemId Id="%s" ChangeKey="%s"/>
      <t:NewBodyContent BodyType="%s">%s</t:NewBodyContent>
    </t:%s>
  </m:Items>
</m:CreateItem>`, element, xmlEscape(original.ItemID.ID), xmlEscape(original.ItemID.ChangeKey), bodyType, xmlEscape(body), element), "CreateItem")
	if err != nil {
		return fmt.Errorf("create reply draft: %w", err)
	}
	var env ewsEnvelope
	if err := xml.Unmarshal(resp, &env); err != nil {
		return fmt.Errorf("parse reply draft response: %w", err)
	}
	if env.Body.CreateItemResponse == nil || len(env.Body.CreateItemResponse.Messages) == 0 {
		return fmt.Errorf("create reply draft: empty response")
	}
	msg := env.Body.CreateItemResponse.Messages[0]
	if msg.ResponseClass != "Success" || len(msg.Items) == 0 {
		return fmt.Errorf("create reply draft: %s", msg.MessageText)
	}
	draftID := msg.Items[0].ItemID.ID
	changeKey := msg.Items[0].ItemID.ChangeKey

	// Step 2: update cc/bcc/subject/read-receipt
	if newCK, err := c.updateDraftMeta(draftID, cc, bcc, subject, readReceipt); err != nil {
		return fmt.Errorf("update reply draft: %w", err)
	} else if newCK != "" {
		changeKey = newCK
	}

	// Step 3: attach files
	if len(attachments) > 0 {
		if newCK, err := c.addAttachmentsToDraft(draftID, changeKey, attachments); err != nil {
			return fmt.Errorf("attach files to reply: %w", err)
		} else if newCK != "" {
			changeKey = newCK
		}
	}

	return c.sendDraftItem(draftID, changeKey)
}

// forwardItemFull creates a forward draft, optionally adds cc/bcc/attachments, then sends.
func (c *ewsClient) forwardItemFull(itemID string, to []string, body, bodyType string, cc, bcc []string, readReceipt bool, attachments []OutboundAttachment) error {
	original, err := c.getFullItem(itemID)
	if err != nil {
		return fmt.Errorf("get original for forward: %w", err)
	}

	resp, err := c.do(fmt.Sprintf(`
<m:CreateItem MessageDisposition="SaveOnly">
  <m:SavedItemFolderId>
    <t:DistinguishedFolderId Id="drafts"/>
  </m:SavedItemFolderId>
  <m:Items>
    <t:ForwardItem>
      <t:ReferenceItemId Id="%s" ChangeKey="%s"/>
      <t:NewBodyContent BodyType="%s">%s</t:NewBodyContent>
      <t:ToRecipients>%s</t:ToRecipients>
    </t:ForwardItem>
  </m:Items>
</m:CreateItem>`, xmlEscape(original.ItemID.ID), xmlEscape(original.ItemID.ChangeKey), bodyType, xmlEscape(body), buildMailboxList(to)), "CreateItem")
	if err != nil {
		return fmt.Errorf("create forward draft: %w", err)
	}
	var env ewsEnvelope
	if err := xml.Unmarshal(resp, &env); err != nil {
		return fmt.Errorf("parse forward draft response: %w", err)
	}
	if env.Body.CreateItemResponse == nil || len(env.Body.CreateItemResponse.Messages) == 0 {
		return fmt.Errorf("create forward draft: empty response")
	}
	msg := env.Body.CreateItemResponse.Messages[0]
	if msg.ResponseClass != "Success" || len(msg.Items) == 0 {
		return fmt.Errorf("create forward draft: %s", msg.MessageText)
	}
	draftID := msg.Items[0].ItemID.ID
	changeKey := msg.Items[0].ItemID.ChangeKey

	if newCK, err := c.updateDraftMeta(draftID, cc, bcc, "", readReceipt); err != nil {
		return fmt.Errorf("update forward draft: %w", err)
	} else if newCK != "" {
		changeKey = newCK
	}

	if len(attachments) > 0 {
		if newCK, err := c.addAttachmentsToDraft(draftID, changeKey, attachments); err != nil {
			return fmt.Errorf("attach files to forward: %w", err)
		} else if newCK != "" {
			changeKey = newCK
		}
	}

	return c.sendDraftItem(draftID, changeKey)
}

// addAttachmentsToDraft adds file attachments to a draft item and returns the new ChangeKey.
func (c *ewsClient) addAttachmentsToDraft(itemID, changeKey string, attachments []OutboundAttachment) (string, error) {
	var attachXML strings.Builder
	for _, a := range attachments {
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		attachXML.WriteString(fmt.Sprintf(`
    <t:FileAttachment>
      <t:Name>%s</t:Name>
      <t:ContentType>%s</t:ContentType>
      <t:Content>%s</t:Content>
    </t:FileAttachment>`, xmlEscape(a.Filename), xmlEscape(ct), base64.StdEncoding.EncodeToString(a.Data)))
	}
	resp, err := c.do(fmt.Sprintf(`
<m:CreateAttachment>
  <m:ParentItemId Id="%s" ChangeKey="%s"/>
  <m:Attachments>%s
  </m:Attachments>
</m:CreateAttachment>`, xmlEscape(itemID), xmlEscape(changeKey), attachXML.String()), "CreateAttachment")
	if err != nil {
		return "", err
	}
	var env ewsEnvelope
	if err := xml.Unmarshal(resp, &env); err != nil {
		return "", nil
	}
	if env.Body.CreateAttachmentResponse != nil && len(env.Body.CreateAttachmentResponse.Messages) > 0 {
		m := env.Body.CreateAttachmentResponse.Messages[0]
		if m.ResponseClass != "Success" {
			return "", fmt.Errorf("CreateAttachment failed: %s", m.MessageText)
		}
		if len(m.Attachments) > 0 {
			return m.Attachments[len(m.Attachments)-1].AttachmentID.RootItemChangeKey, nil
		}
	}
	return "", nil
}

// getUserOof fetches the current Out-of-Office settings for the authenticated user.
func (c *ewsClient) getUserOof(username string) (*ewsOofSettings, error) {
	resp, err := c.do(fmt.Sprintf(`
<m:GetUserOofSettingsRequest>
  <t:Mailbox><t:Address>%s</t:Address></t:Mailbox>
</m:GetUserOofSettingsRequest>`, xmlEscape(username)), "GetUserOofSettings")
	if err != nil {
		return nil, err
	}
	var env ewsEnvelope
	if err := xml.Unmarshal(resp, &env); err != nil {
		return nil, fmt.Errorf("parse GetUserOofSettings response: %w", err)
	}
	if env.Body.GetUserOofSettingsResponse == nil {
		return nil, fmt.Errorf("GetUserOofSettings: empty response")
	}
	r := env.Body.GetUserOofSettingsResponse
	if r.ResponseMessage.ResponseClass != "Success" {
		return nil, fmt.Errorf("GetUserOofSettings failed: %s", r.ResponseMessage.MessageText)
	}
	oof := env.Body.GetUserOofSettingsResponse.OofSettings
	return &oof, nil
}

// setUserOof updates the Out-of-Office settings for the authenticated user.
func (c *ewsClient) setUserOof(username string, settings VacationSettings) error {
	state := "Disabled"
	if settings.Enabled {
		if settings.StartTime != "" && settings.EndTime != "" {
			state = "Scheduled"
		} else {
			state = "Enabled"
		}
	}
	audience := settings.ExternalAudience
	if audience == "" {
		audience = "None"
	}
	durationBlock := ""
	if settings.StartTime != "" && settings.EndTime != "" {
		durationBlock = fmt.Sprintf(`<t:Duration><t:StartTime>%s</t:StartTime><t:EndTime>%s</t:EndTime></t:Duration>`,
			xmlEscape(settings.StartTime), xmlEscape(settings.EndTime))
	}
	_, err := c.do(fmt.Sprintf(`
<m:SetUserOofSettingsRequest>
  <t:Mailbox><t:Address>%s</t:Address></t:Mailbox>
  <t:UserOofSettings>
    <t:OofState>%s</t:OofState>
    <t:ExternalAudience>%s</t:ExternalAudience>
    %s
    <t:InternalReply><t:Message>%s</t:Message></t:InternalReply>
    <t:ExternalReply><t:Message>%s</t:Message></t:ExternalReply>
  </t:UserOofSettings>
</m:SetUserOofSettingsRequest>`,
		xmlEscape(username), state, audience, durationBlock,
		xmlEscape(settings.InternalMessage), xmlEscape(settings.ExternalMessage)), "SetUserOofSettings")
	return err
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
