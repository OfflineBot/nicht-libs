# moodle

Client für Moodle-Webservices (DHBW-Lernplattform). Token-basierter API-Zugriff
für Kurse, Aktivitäten, Dateien, Nachrichten, Kalender, Bewertungen — plus
Web-Scraping für Sachen die's per API nicht gibt (Anwesenheits-Sessions).

```
go get github.com/OfflineBot/nicht-libs/moodle
```

## Beispiel

```go
tok, err := moodle.GetToken("https://moodle.dhbw.de", user, pass)
if err != nil { log.Fatal(err) }

courses, err := moodle.GetCourses("https://moodle.dhbw.de", tok.Token)
for _, c := range courses {
    fmt.Println(c.ID, c.Fullname)
}

files, _ := moodle.GetCourseFiles("https://moodle.dhbw.de", tok.Token, courses[0].ID)
```

## API — Auth

| Funktion | Zweck |
|---|---|
| `GetToken(baseURL, user, pass)` | API-Token holen (klassischer Login) |
| `WebLogin(baseURL, user, pass)` | Cookie-Session für Web-Scraping |
| `CallAPI(baseURL, token, fn, params)` | Roher Webservice-Call falls was hier fehlt |

`ErrAuthFailed` bei ungültigem Token, `ErrWrongPassword` bei Self-Service-Anwesenheit.

## API — Kurse & Inhalte

| Funktion | Zweck |
|---|---|
| `GetCourses(baseURL, token)` | Alle eingeschriebenen Kurse |
| `GetCourseFiles(baseURL, token, courseID)` | Material-Dateien |
| `GetCourseImage(baseURL, token, courseID)` | Kursbild als Bytes |
| `GetCourseImageURL(baseURL, token, courseID)` | Direkte URL (falls man's selber laden will) |
| `GetCompletionStatus(baseURL, token, courseID)` | Abschluss-Tracking |
| `SetCompletion(baseURL, token, cmID, completed)` | Modul als erledigt markieren |
| `SetFavourite(baseURL, token, courseID, fav)` | Stern setzen/entfernen |
| `SetHidden(baseURL, token, courseID, hidden)` | Kurs ein-/ausblenden |
| `Search(baseURL, token, query, courseID)` | Volltextsuche |

## API — Aktivitäten

| Funktion | Zweck |
|---|---|
| `GetAssignments(baseURL, token, courseID)` | Abgaben |
| `GetAssignmentSubmissionStatus(baseURL, token, assignID)` | Eigener Abgabestatus |
| `GetQuizzes(baseURL, token, courseID)` | Quizzes im Kurs |
| `GetQuizAttempts(baseURL, token, quizID)` | Eigene Versuche |
| `GetGrades(baseURL, token, courseID)` | Notenbuch |
| `GetParticipants(baseURL, token, courseID)` | Teilnehmerliste |
| `GetRecentActivities(baseURL, token)` | "Zuletzt zugegriffen" |

## API — Anwesenheit

| Funktion | Zweck |
|---|---|
| `GetCoursesWithAttendance(baseURL, token)` | Kurse die ein Anwesenheits-Modul haben |
| `GetAttendanceUserData(baseURL, token, cmID)` | Persönliche Anwesenheits-Historie |
| `GetOpenAttendanceSessions(client, baseURL, apiToken)` | Aktuell offene Sessions (per Web-Scrape) |
| `SubmitAttendanceWeb(client, baseURL, sessID, cmid, statusVal, password)` | Selbst-Eintragen |

## API — Kommunikation

| Funktion | Zweck |
|---|---|
| `GetMessages(baseURL, token, moodleUserID)` | Chats |
| `MarkMessageRead(baseURL, token, msgID)` | Lesestatus |
| `GetNotifications(baseURL, token, moodleUserID)` | Popup-Benachrichtigungen |
| `GetAnnouncements(baseURL, token, courseIDs)` | Forum-Announcements |
| `GetCalendarEvents(baseURL, token)` | Kalender |

## API — Files & Misc

| Funktion | Zweck |
|---|---|
| `GetPrivateFiles(baseURL, token, moodleUserID)` | Persönlicher Dateibereich |
| `DownloadFile(token, fileURL)` | Datei mit Auth herunterladen → `io.ReadCloser` |
| `FetchImageBytes(token, url)` | Inline-Bild laden |
| `DocTypeFromMime(mime)` | `"application/pdf"` → `"pdf"` |
| `DeriveSemester(unixTS)` / `DeriveSemesterNumber(catName, unixTS)` | DHBW-Semester aus Kursdaten ableiten |
| `SaveUserStatus(baseURL, token, sessID, statusID, statusSet, password)` | User-Status setzen |

## Hinweise

- Tokens sind pro User. Speichern, nicht jedes Mal neu generieren.
- DHBW-Moodle nutzt `https://moodle.dhbw-<standort>.de` — der Konsument bestimmt
  die Base-URL.
- `WebLogin` und `GetToken` laufen über unterschiedliche Endpoints; manche
  Funktionen brauchen den HTTP-Client (Cookie-Session), andere das API-Token.
- `CallAPI` ist als Notausgang exportiert — wenn ein Webservice fehlt, kann der
  Konsument ihn direkt rufen, ohne die Lib forken zu müssen.
