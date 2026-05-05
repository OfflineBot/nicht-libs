# email

Wrapper um Exchange Web Services (EWS) mit Fallback auf IMAP. Liest, sendet,
verschiebt und durchsucht Mails. Nutzt NTLM für DHBW-Mailboxen, Basic Auth für
generische Server.

```
go get github.com/OfflineBot/nicht-libs/email
```

## Beispiel

```go
inbox, err := email.GetMessages(server, user, pass, "Inbox", 50, 0)
for _, m := range inbox.Messages {
    fmt.Printf("[%s] %s — %s\n", m.From, m.Subject, m.Date)
}

msg, err := email.GetEmailByID(server, user, pass, m.ID)
fmt.Println(msg.BodyHTML)

email.SendEmail(server, user, pass,
    []string{"jemand@dhbw.de"}, nil, nil,
    "Betreff", "<p>Hallo</p>", true)
```

## API — Lesen

| Funktion | Zweck |
|---|---|
| `TestConnection(server, user, pass)` | Auth-Check + Server-Variante (EWS/IMAP) |
| `GetMessages(s, u, p, folder, limit, offset)` | Folder-Listing, paginiert |
| `SearchMessages(s, u, p, query, folder, limit)` | Volltextsuche |
| `GetEmailByID(s, u, p, id)` | Volle Mail mit Headern + HTML/Text |
| `GetEmailsByIDs(s, u, p, ids)` | Batch-Fetch mit Split-Fallback bei Throttling |
| `GetAttachment(s, u, p, attID)` | Anhang als Bytes |
| `GetFolderUnreadCount(s, u, p, folder)` | Counter ohne ganze Liste zu ziehen |
| `ProxyImage(s, u, p, url)` | Inline-Bild aus authentifizierter Session laden |

## API — Schreiben & Verwalten

| Funktion | Zweck |
|---|---|
| `SendEmail(...)` / `SendEmailWithAttachments(...)` | Versenden |
| `SaveDraft(...)` / `UpdateDraft(...)` | Entwürfe |
| `ReplyEmail(...)` / `ReplyEmailFull(...)` | Antworten (mit/ohne CC, BCC, Attachments) |
| `ForwardEmail(...)` / `ForwardEmailFull(...)` | Weiterleiten |
| `MarkReadByID` / `MarkUnreadByID` / `MarkFolderRead` | Lesestatus |
| `ArchiveEmail` / `MoveEmail` / `DeleteEmailByID` | Verschieben/Löschen |
| `GetVacationResponder` / `SetVacationResponder` | Abwesenheitsnotiz |

## API — MIME / IDs

| Funktion | Zweck |
|---|---|
| `ExtractPlainTextFromMIME(b64)` | Text aus rohem MIME, Charset-tolerant |
| `ParseListUnsubscribe(b64)` | `List-Unsubscribe`-URL aus Header |
| `EncodeItemID(raw)` / `DecodeItemID(s)` | URL-safe IDs für API-Routes |

## Hinweise

- Alle Auth-Daten werden pro Aufruf übergeben — die Lib hält keine Session.
  Wer pooling will, baut das im Konsument.
- IMAP- und EWS-Pfade haben dieselbe Funktions-Signatur; intern wird beim ersten
  `TestConnection` die richtige Implementierung erkannt.
- MIME-Charsets jenseits von UTF-8/ISO-8859-1/Windows-1252 werden best-effort
  dekodiert (siehe Tests).
