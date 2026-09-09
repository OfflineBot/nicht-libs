package email

// Additional EWS operations: attachment download, move, search,
// mark-all-read, drafts, image proxy.

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
)

// ── XML types ─────────────────────────────────────────────────────────────────

type getAttachmentResponse struct {
	Messages []getAttachmentMsg `xml:"ResponseMessages>GetAttachmentResponseMessage"`
}
type getAttachmentMsg struct {
	ResponseClass string          `xml:"ResponseClass,attr"`
	MessageText   string          `xml:"MessageText"`
	Attachments   []ewsFullAttach `xml:"Attachments>FileAttachment"`
}
type ewsFullAttach struct {
	Name        string `xml:"Name"`
	ContentType string `xml:"ContentType"`
	Content     string `xml:"Content"` // base64-encoded by Exchange
}

type moveItemResponse struct {
	Messages []moveItemMsg `xml:"ResponseMessages>MoveItemResponseMessage"`
}
type moveItemMsg struct {
	ResponseClass string        `xml:"ResponseClass,attr"`
	MessageText   string        `xml:"MessageText"`
	Items         []ewsMovedMsg `xml:"Items>Message"`
}
type ewsMovedMsg struct {
	ItemID ewsItemID `xml:"ItemId"`
}

type createAttachmentResponse struct {
	Messages []createAttachmentMsg `xml:"ResponseMessages>CreateAttachmentResponseMessage"`
}
type createAttachmentMsg struct {
	ResponseClass string             `xml:"ResponseClass,attr"`
	MessageText   string             `xml:"MessageText"`
	Attachments   []ewsCreatedAttach `xml:"Attachments>FileAttachment"`
}
type ewsCreatedAttach struct {
	AttachmentID ewsCreatedAttachID `xml:"AttachmentId"`
}
type ewsCreatedAttachID struct {
	RootItemChangeKey string `xml:"RootItemChangeKey,attr"`
}

type sendItemResponse struct {
	Messages []sendItemMsg `xml:"ResponseMessages>SendItemResponseMessage"`
}
type sendItemMsg struct {
	ResponseClass string `xml:"ResponseClass,attr"`
	MessageText   string `xml:"MessageText"`
}

// OutboundAttachment is a file to be attached to an outgoing email.
//
// ContentID and Inline are for images that belong *in* the message body: the
// HTML references them as <img src="cid:the-id">, and the client renders them
// in place instead of listing them at the bottom. Without both, an image can
// only travel as a normal attachment — a data: URI in the body is stripped by
// Outlook and most webmail, so that is not an alternative.
type OutboundAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
	// ContentID is the value the body refers to with cid:. Empty for a
	// normal attachment.
	ContentID string
	// Inline marks the attachment as part of the body rather than a file
	// hanging off it.
	Inline bool
}

// attachmentXML builds the <t:Attachments> children for a set of outgoing
// attachments. Split out so the element order — which EWS is strict about —
// can be checked without a server.
func attachmentXML(attachments []OutboundAttachment) string {
	var b strings.Builder
	for _, a := range attachments {
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		b.WriteString(einAnhangXML(a, ct))
	}
	return b.String()
}

func einAnhangXML(a OutboundAttachment, ct string) string {
	// Element order matters: the EWS schema declares AttachmentType as a
	// sequence, so ContentId and IsInline must sit between ContentType and
	// Content. Out of order, Exchange rejects the whole request.
	extra := ""
	if a.ContentID != "" {
		extra += fmt.Sprintf("\n      <t:ContentId>%s</t:ContentId>", xmlEscape(a.ContentID))
	}
	if a.Inline {
		extra += "\n      <t:IsInline>true</t:IsInline>"
	}
	return fmt.Sprintf(`
    <t:FileAttachment>
      <t:Name>%s</t:Name>
      <t:ContentType>%s</t:ContentType>%s
      <t:Content>%s</t:Content>
    </t:FileAttachment>`, xmlEscape(a.Filename), xmlEscape(ct), extra,
		base64.StdEncoding.EncodeToString(a.Data))
}

// ── Public API ────────────────────────────────────────────────────────────────

// GetAttachment downloads a file attachment by its EWS AttachmentId.
// attachmentID must be the raw (decoded) EWS attachment ID.
func GetAttachment(server, username, password, attachmentID string) (*AttachmentContent, error) {
	c := newEWSClient(server, username, password)
	return c.getAttachment(attachmentID)
}

// MoveEmail moves an email to another folder and returns the new (encoded) item ID.
// Exchange assigns a new ItemId after a move.
func MoveEmail(server, username, password, itemID, folder string) (string, error) {
	c := newEWSClient(server, username, password)
	newID, err := c.moveItem(itemID, ewsFolderID(folder))
	if err != nil {
		return "", err
	}
	return EncodeItemID(newID), nil
}

// SearchMessages searches Subject, From and Body in the given folder.
// Returns the same InboxResult structure as GetMessages.
func SearchMessages(server, username, password, query, folder string, limit int) (*InboxResult, error) {
	folderEWS := ewsFolderID(folder)
	c := newEWSClient(server, username, password)
	items, err := c.searchItems(query, folderEWS, limit)
	if err != nil {
		return nil, err
	}
	emails := make([]EmailSummary, 0, len(items))
	for _, item := range items {
		emails = append(emails, summaryFromEWS(item))
	}
	return &InboxResult{Emails: emails, Total: len(emails)}, nil
}

// MarkFolderRead marks all unread messages in a folder as read.
// Returns the number of messages that were marked.
func MarkFolderRead(server, username, password, folder string) (int, error) {
	c := newEWSClient(server, username, password)
	return c.markFolderAllRead(ewsFolderID(folder))
}

// SaveDraft creates a new draft in the Drafts folder.
// Returns the base64url-encoded EWS ItemId of the new draft.
func SaveDraft(server, username, password string, to, cc []string, subject, body string, html bool) (string, error) {
	c := newEWSClient(server, username, password)
	rawID, err := c.saveDraft(to, cc, subject, body, bodyTypeStr(html))
	if err != nil {
		return "", err
	}
	return EncodeItemID(rawID), nil
}

// UpdateDraft updates an existing draft (subject, body, recipients).
func UpdateDraft(server, username, password, itemID string, to, cc []string, subject, body string, html bool) error {
	c := newEWSClient(server, username, password)
	return c.updateDraft(itemID, to, cc, subject, body, bodyTypeStr(html))
}

// SendEmailWithAttachments sends an email with file attachments via EWS.
// Uses a 3-step flow: CreateItem(SaveOnly) → CreateAttachment → SendItem.
func SendEmailWithAttachments(server, username, password string, to, cc, bcc []string, subject, body string, html bool, attachments []OutboundAttachment) error {
	c := newEWSClient(server, username, password)
	return c.sendEmailWithAttachments(to, cc, bcc, subject, body, bodyTypeStr(html), attachments)
}

// MarkUnreadByID marks an email as unread by EWS ItemID string.
func MarkUnreadByID(server, username, password, itemID string) error {
	c := newEWSClient(server, username, password)
	return c.markUnread(itemID)
}

// ArchiveEmail moves an email to the archive folder.
func ArchiveEmail(server, username, password, itemID string) (string, error) {
	c := newEWSClient(server, username, password)
	newID, err := c.moveItem(itemID, "archive")
	if err != nil {
		return "", err
	}
	return EncodeItemID(newID), nil
}

// ReplyEmailFull sends a reply with optional cc, bcc, subject override, read receipt and attachments.
func ReplyEmailFull(server, username, password, itemID, body string, html, all bool, cc, bcc []string, subject string, readReceipt bool, attachments []OutboundAttachment) error {
	c := newEWSClient(server, username, password)
	return c.replyItemFull(itemID, body, bodyTypeStr(html), all, cc, bcc, subject, readReceipt, attachments)
}

// ForwardEmailFull forwards an email with optional cc, bcc, read receipt and attachments.
func ForwardEmailFull(server, username, password, itemID string, to []string, body string, html bool, cc, bcc []string, readReceipt bool, attachments []OutboundAttachment) error {
	c := newEWSClient(server, username, password)
	return c.forwardItemFull(itemID, to, body, bodyTypeStr(html), cc, bcc, readReceipt, attachments)
}

// GetVacationResponder returns the current Out-of-Office settings.
func GetVacationResponder(server, username, password string) (*VacationSettings, error) {
	c := newEWSClient(server, username, password)
	oof, err := c.getUserOof(username)
	if err != nil {
		return nil, err
	}
	v := &VacationSettings{
		Enabled:          oof.OofState != "Disabled",
		State:            oof.OofState,
		ExternalAudience: oof.ExternalAudience,
		StartTime:        oof.Duration.StartTime,
		EndTime:          oof.Duration.EndTime,
		InternalMessage:  oof.InternalReply.Message,
		ExternalMessage:  oof.ExternalReply.Message,
	}
	return v, nil
}

// SetVacationResponder updates the Out-of-Office settings.
func SetVacationResponder(server, username, password string, settings VacationSettings) error {
	c := newEWSClient(server, username, password)
	return c.setUserOof(username, settings)
}

// ProxyImage fetches an image URL using stored NTLM credentials.
// The imageURL must belong to the same host as the EWS server to prevent SSRF.
func ProxyImage(server, username, password, imageURL string) ([]byte, string, error) {
	// Security: only proxy URLs on the same host as the EWS server.
	if !sameHost(server, imageURL) {
		return nil, "", fmt.Errorf("image URL host does not match EWS server host")
	}
	c := newEWSClient(server, username, password)
	resp, err := c.client.Get(imageURL)
	if err != nil {
		return nil, "", fmt.Errorf("image proxy request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("image proxy: server returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("image proxy: read body: %w", err)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// ── EWS operations ────────────────────────────────────────────────────────────

func (c *ewsClient) sendEmailWithAttachments(to, cc, bcc []string, subject, body, bodyType string, attachments []OutboundAttachment) error {
	slog.Debug("mail: sendEmailWithAttachments start",
		"to", to, "cc", cc, "bcc", bcc,
		"subject", subject,
		"attachments", len(attachments),
	)
	// Step 1: CreateItem(SaveOnly) to create a draft and get its ItemId + ChangeKey.
	ccBlock := ""
	if len(cc) > 0 {
		ccBlock = fmt.Sprintf(`<t:CcRecipients>%s</t:CcRecipients>`, buildMailboxList(cc))
	}
	bccBlock := ""
	if len(bcc) > 0 {
		bccBlock = fmt.Sprintf(`<t:BccRecipients>%s</t:BccRecipients>`, buildMailboxList(bcc))
	}
	resp, err := c.do(fmt.Sprintf(`
<m:CreateItem MessageDisposition="SaveOnly">
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
		return fmt.Errorf("CreateItem (save): %w", err)
	}

	var env ewsEnvelope
	if err := xml.Unmarshal(resp, &env); err != nil {
		return fmt.Errorf("parse CreateItem response: %w", err)
	}
	if env.Body.CreateItemResponse == nil || len(env.Body.CreateItemResponse.Messages) == 0 {
		return fmt.Errorf("CreateItem returned empty response")
	}
	msg := env.Body.CreateItemResponse.Messages[0]
	if msg.ResponseClass != "Success" || len(msg.Items) == 0 {
		return fmt.Errorf("CreateItem failed: %s", msg.MessageText)
	}
	itemID := msg.Items[0].ItemID.ID
	changeKey := msg.Items[0].ItemID.ChangeKey
	slog.Debug("mail: sendEmailWithAttachments — draft created", "item_id_len", len(itemID))

	// Step 2: CreateAttachment — attach all files in a single call.
	var attachXML strings.Builder
	for _, a := range attachments {
		slog.Debug("mail: sendEmailWithAttachments — attaching file",
			"filename", a.Filename,
			"content_type", a.ContentType,
			"size_bytes", len(a.Data),
		)
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		attachXML.WriteString(einAnhangXML(a, ct))
	}
	attachResp, err := c.do(fmt.Sprintf(`
<m:CreateAttachment>
  <m:ParentItemId Id="%s" ChangeKey="%s"/>
  <m:Attachments>%s
  </m:Attachments>
</m:CreateAttachment>`, xmlEscape(itemID), xmlEscape(changeKey), attachXML.String()), "CreateAttachment")
	if err != nil {
		return fmt.Errorf("CreateAttachment: %w", err)
	}

	var attachEnv ewsEnvelope
	if err := xml.Unmarshal(attachResp, &attachEnv); err != nil {
		return fmt.Errorf("parse CreateAttachment response: %w", err)
	}
	if attachEnv.Body.CreateAttachmentResponse == nil || len(attachEnv.Body.CreateAttachmentResponse.Messages) == 0 {
		return fmt.Errorf("CreateAttachment returned empty response")
	}
	attachMsg := attachEnv.Body.CreateAttachmentResponse.Messages[0]
	if attachMsg.ResponseClass != "Success" {
		slog.Debug("mail: sendEmailWithAttachments — CreateAttachment failed", "response_class", attachMsg.ResponseClass, "message", attachMsg.MessageText)
		return fmt.Errorf("CreateAttachment failed: %s", attachMsg.MessageText)
	}
	// Exchange updates the ChangeKey after adding attachments; use the new one for SendItem.
	if len(attachMsg.Attachments) > 0 {
		if ck := attachMsg.Attachments[len(attachMsg.Attachments)-1].AttachmentID.RootItemChangeKey; ck != "" {
			changeKey = ck
		}
	}
	slog.Debug("mail: sendEmailWithAttachments — attachments created", "count", len(attachMsg.Attachments))

	// Step 3: SendItem — send the draft with its attachments.
	sendResp, err := c.do(fmt.Sprintf(`
<m:SendItem SaveItemToFolder="true">
  <m:ItemIds>
    <t:ItemId Id="%s" ChangeKey="%s"/>
  </m:ItemIds>
  <m:SavedItemFolderId>
    <t:DistinguishedFolderId Id="sentitems"/>
  </m:SavedItemFolderId>
</m:SendItem>`, xmlEscape(itemID), xmlEscape(changeKey)), "SendItem")
	if err != nil {
		return fmt.Errorf("SendItem: %w", err)
	}

	var sendEnv ewsEnvelope
	if err := xml.Unmarshal(sendResp, &sendEnv); err != nil {
		return fmt.Errorf("parse SendItem response: %w", err)
	}
	if sendEnv.Body.SendItemResponse != nil && len(sendEnv.Body.SendItemResponse.Messages) > 0 {
		m := sendEnv.Body.SendItemResponse.Messages[0]
		if m.ResponseClass != "Success" {
			slog.Debug("mail: sendEmailWithAttachments — SendItem failed", "response_class", m.ResponseClass, "message", m.MessageText)
			return fmt.Errorf("SendItem failed: %s", m.MessageText)
		}
	}
	slog.Debug("mail: sendEmailWithAttachments — sent successfully")
	return nil
}

func (c *ewsClient) getAttachment(attachmentID string) (*AttachmentContent, error) {
	slog.Debug("mail: getAttachment — requesting from EWS", "attachment_id_len", len(attachmentID))
	body, err := c.do(fmt.Sprintf(`
<m:GetAttachment>
  <m:AttachmentIds>
    <t:RequestAttachmentId Id="%s"/>
  </m:AttachmentIds>
</m:GetAttachment>`, xmlEscape(attachmentID)), "GetAttachment")
	if err != nil {
		return nil, err
	}

	var env ewsEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse GetAttachment response: %w", err)
	}
	if env.Body.GetAttachmentResponse == nil || len(env.Body.GetAttachmentResponse.Messages) == 0 {
		return nil, fmt.Errorf("attachment not found")
	}
	msg := env.Body.GetAttachmentResponse.Messages[0]
	slog.Debug("mail: getAttachment — EWS response", "response_class", msg.ResponseClass, "attachments_in_response", len(msg.Attachments))
	if msg.ResponseClass != "Success" {
		return nil, fmt.Errorf("GetAttachment failed: %s", msg.MessageText)
	}
	if len(msg.Attachments) == 0 {
		return nil, fmt.Errorf("attachment not found")
	}
	a := msg.Attachments[0]
	slog.Debug("mail: getAttachment — decoding content",
		"filename", a.Name,
		"content_type", a.ContentType,
		"content_len_b64", len(a.Content),
	)

	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(a.Content), ""))
	if err != nil {
		slog.Debug("mail: getAttachment — base64 decode failed", "filename", a.Name, "err", err)
		return nil, fmt.Errorf("decode attachment content: %w", err)
	}
	ct := a.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	slog.Debug("mail: getAttachment — done", "filename", a.Name, "size_bytes", len(data))
	return &AttachmentContent{Filename: a.Name, ContentType: ct, Data: data}, nil
}

func (c *ewsClient) moveItem(itemID, targetFolderEWS string) (string, error) {
	body, err := c.do(fmt.Sprintf(`
<m:MoveItem>
  <m:ToFolderId>
    <t:DistinguishedFolderId Id="%s"/>
  </m:ToFolderId>
  <m:ItemIds>
    <t:ItemId Id="%s"/>
  </m:ItemIds>
</m:MoveItem>`, targetFolderEWS, xmlEscape(itemID)), "MoveItem")
	if err != nil {
		return "", err
	}

	var env ewsEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return "", fmt.Errorf("parse MoveItem response: %w", err)
	}
	if env.Body.MoveItemResponse == nil || len(env.Body.MoveItemResponse.Messages) == 0 {
		return "", fmt.Errorf("MoveItem returned empty response")
	}
	msg := env.Body.MoveItemResponse.Messages[0]
	if msg.ResponseClass != "Success" {
		return "", fmt.Errorf("MoveItem failed: %s", msg.MessageText)
	}
	if len(msg.Items) == 0 {
		return "", fmt.Errorf("MoveItem did not return new ItemId")
	}
	return msg.Items[0].ItemID.ID, nil
}

func (c *ewsClient) searchItems(query, folderEWS string, limit int) ([]ewsItem, error) {
	escaped := xmlEscape(query)
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
    </t:AdditionalProperties>
  </m:ItemShape>
  <m:IndexedPageItemView MaxEntriesReturned="%d" Offset="0" BasePoint="Beginning"/>
  <m:Restriction>
    <t:Or>
      <t:Contains ContainmentMode="Substring" ContainmentComparison="IgnoreCase">
        <t:FieldURI FieldURI="item:Subject"/>
        <t:Constant Value="%s"/>
      </t:Contains>
      <t:Contains ContainmentMode="Substring" ContainmentComparison="IgnoreCase">
        <t:FieldURI FieldURI="message:From"/>
        <t:Constant Value="%s"/>
      </t:Contains>
      <t:Contains ContainmentMode="Substring" ContainmentComparison="IgnoreCase">
        <t:FieldURI FieldURI="item:Body"/>
        <t:Constant Value="%s"/>
      </t:Contains>
    </t:Or>
  </m:Restriction>
  <m:SortOrder>
    <t:FieldOrder Order="Descending">
      <t:FieldURI FieldURI="item:DateTimeSent"/>
    </t:FieldOrder>
  </m:SortOrder>
  <m:ParentFolderIds>
    <t:DistinguishedFolderId Id="%s"/>
  </m:ParentFolderIds>
</m:FindItem>`, limit, escaped, escaped, escaped, folderEWS), "FindItem")
	if err != nil {
		return nil, err
	}

	var env ewsEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse FindItem (search) response: %w", err)
	}
	if env.Body.FindItemResponse == nil || len(env.Body.FindItemResponse.Messages) == 0 {
		return nil, nil
	}
	msg := env.Body.FindItemResponse.Messages[0]
	if msg.ResponseClass != "Success" {
		return nil, fmt.Errorf("FindItem (search) failed: %s", msg.MessageText)
	}
	return msg.RootFolder.Items, nil
}

func (c *ewsClient) markFolderAllRead(folderEWS string) (int, error) {
	const batchSize = 100
	total := 0
	for offset := 0; ; offset += batchSize {
		ids, err := c.findUnreadIDs(folderEWS, batchSize, offset)
		if err != nil {
			return total, err
		}
		if len(ids) == 0 {
			break
		}
		if err := c.markItemsRead(ids); err != nil {
			return total, err
		}
		total += len(ids)
		if len(ids) < batchSize {
			break
		}
	}
	return total, nil
}

func (c *ewsClient) findUnreadIDs(folderEWS string, limit, offset int) ([]string, error) {
	body, err := c.do(fmt.Sprintf(`
<m:FindItem Traversal="Shallow">
  <m:ItemShape>
    <t:BaseShape>IdOnly</t:BaseShape>
  </m:ItemShape>
  <m:IndexedPageItemView MaxEntriesReturned="%d" Offset="%d" BasePoint="Beginning"/>
  <m:Restriction>
    <t:IsEqualTo>
      <t:FieldURI FieldURI="message:IsRead"/>
      <t:FieldURIOrConstant><t:Constant Value="false"/></t:FieldURIOrConstant>
    </t:IsEqualTo>
  </m:Restriction>
  <m:ParentFolderIds>
    <t:DistinguishedFolderId Id="%s"/>
  </m:ParentFolderIds>
</m:FindItem>`, limit, offset, folderEWS), "FindItem")
	if err != nil {
		return nil, err
	}

	var env ewsEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse FindItem (unread) response: %w", err)
	}
	if env.Body.FindItemResponse == nil || len(env.Body.FindItemResponse.Messages) == 0 {
		return nil, nil
	}
	msg := env.Body.FindItemResponse.Messages[0]
	if msg.ResponseClass != "Success" {
		return nil, fmt.Errorf("FindItem (unread) failed: %s", msg.MessageText)
	}
	ids := make([]string, 0, len(msg.RootFolder.Items))
	for _, item := range msg.RootFolder.Items {
		ids = append(ids, item.ItemID.ID)
	}
	return ids, nil
}

func (c *ewsClient) markItemsRead(ids []string) error {
	var changes strings.Builder
	for _, id := range ids {
		changes.WriteString(fmt.Sprintf(`
    <t:ItemChange>
      <t:ItemId Id="%s"/>
      <t:Updates>
        <t:SetItemField>
          <t:FieldURI FieldURI="message:IsRead"/>
          <t:Message><t:IsRead>true</t:IsRead></t:Message>
        </t:SetItemField>
      </t:Updates>
    </t:ItemChange>`, xmlEscape(id)))
	}
	_, err := c.do(fmt.Sprintf(`
<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite">
  <m:ItemChanges>%s
  </m:ItemChanges>
</m:UpdateItem>`, changes.String()), "UpdateItem")
	return err
}

func (c *ewsClient) saveDraft(to, cc []string, subject, body, bodyType string) (string, error) {
	ccBlock := ""
	if len(cc) > 0 {
		ccBlock = fmt.Sprintf(`<t:CcRecipients>%s</t:CcRecipients>`, buildMailboxList(cc))
	}
	toBlock := ""
	if len(to) > 0 {
		toBlock = fmt.Sprintf(`<t:ToRecipients>%s</t:ToRecipients>`, buildMailboxList(to))
	}

	resp, err := c.do(fmt.Sprintf(`
<m:CreateItem MessageDisposition="SaveOnly">
  <m:SavedItemFolderId>
    <t:DistinguishedFolderId Id="drafts"/>
  </m:SavedItemFolderId>
  <m:Items>
    <t:Message>
      <t:Subject>%s</t:Subject>
      <t:Body BodyType="%s">%s</t:Body>
      %s
      %s
    </t:Message>
  </m:Items>
</m:CreateItem>`, xmlEscape(subject), bodyType, xmlEscape(body), toBlock, ccBlock), "CreateItem")
	if err != nil {
		return "", err
	}

	var env ewsEnvelope
	if err := xml.Unmarshal(resp, &env); err != nil {
		return "", fmt.Errorf("parse CreateItem (draft) response: %w", err)
	}
	if env.Body.CreateItemResponse == nil || len(env.Body.CreateItemResponse.Messages) == 0 {
		return "", fmt.Errorf("CreateItem (draft) returned empty response")
	}
	msg := env.Body.CreateItemResponse.Messages[0]
	if msg.ResponseClass != "Success" {
		return "", fmt.Errorf("CreateItem (draft) failed: %s", msg.MessageText)
	}
	if len(msg.Items) == 0 {
		return "", fmt.Errorf("CreateItem (draft) did not return ItemId")
	}
	return msg.Items[0].ItemID.ID, nil
}

func (c *ewsClient) updateDraft(itemID string, to, cc []string, subject, body, bodyType string) error {
	var updates strings.Builder

	updates.WriteString(fmt.Sprintf(`
    <t:SetItemField>
      <t:FieldURI FieldURI="item:Subject"/>
      <t:Message><t:Subject>%s</t:Subject></t:Message>
    </t:SetItemField>
    <t:SetItemField>
      <t:FieldURI FieldURI="item:Body"/>
      <t:Message><t:Body BodyType="%s">%s</t:Body></t:Message>
    </t:SetItemField>`, xmlEscape(subject), bodyType, xmlEscape(body)))

	if len(to) > 0 {
		updates.WriteString(fmt.Sprintf(`
    <t:SetItemField>
      <t:FieldURI FieldURI="message:ToRecipients"/>
      <t:Message><t:ToRecipients>%s</t:ToRecipients></t:Message>
    </t:SetItemField>`, buildMailboxList(to)))
	} else {
		updates.WriteString(`
    <t:DeleteItemField>
      <t:FieldURI FieldURI="message:ToRecipients"/>
    </t:DeleteItemField>`)
	}

	if len(cc) > 0 {
		updates.WriteString(fmt.Sprintf(`
    <t:SetItemField>
      <t:FieldURI FieldURI="message:CcRecipients"/>
      <t:Message><t:CcRecipients>%s</t:CcRecipients></t:Message>
    </t:SetItemField>`, buildMailboxList(cc)))
	} else {
		updates.WriteString(`
    <t:DeleteItemField>
      <t:FieldURI FieldURI="message:CcRecipients"/>
    </t:DeleteItemField>`)
	}

	_, err := c.do(fmt.Sprintf(`
<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite">
  <m:ItemChanges>
    <t:ItemChange>
      <t:ItemId Id="%s"/>
      <t:Updates>%s
      </t:Updates>
    </t:ItemChange>
  </m:ItemChanges>
</m:UpdateItem>`, xmlEscape(itemID), updates.String()), "UpdateItem")
	return err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// sameHost returns true if imageURL has the same hostname as the EWS server.
func sameHost(server, imageURL string) bool {
	serverHost := ewsServerHost(server)
	u, err := url.Parse(imageURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), serverHost)
}

func ewsServerHost(server string) string {
	server = strings.TrimRight(server, "/")
	if strings.HasPrefix(server, "http") {
		u, err := url.Parse(server)
		if err != nil {
			return server
		}
		return u.Hostname()
	}
	// bare hostname, possibly with port
	host := server
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return host
}
