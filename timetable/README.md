# timetable

Stundenplan-Scraper für DHBW. Zwei Quellen:

- **Rapla** (`rapla.dhbw-<standort>.de`) — HTML-Tabelle, Standard-Quelle, kompletter Plan
- **api.dhbw.app** — JSON, Backup-Quelle, oft schneller aber unvollständiger

Die Lib kann beide ziehen und mergen.

```
go get github.com/OfflineBot/nicht-libs/timetable
```

## Beispiel

```go
week := timetable.WeekWithOffset("", 0) // aktuelle ISO-Woche, "2026-W18"

tt, err := timetable.FetchAndUpdate(raplaURL, week, raplaUser, raplaFile)
if err != nil {
    log.Fatal(err)
}

if extra, _ := timetable.FetchDhbwApp("RV-WDS125", week); extra != nil {
    tt = tt.MergeWith(extra)
}

for _, day := range tt.Days {
    for _, l := range day.Lessons {
        fmt.Printf("%s — %s @ %s\n", l.Start.Format("Mo 15:04"), l.Title, l.Room)
    }
}

// Für JSON-Export:
js := tt.ToJSON()
js = js.FilterByDate("2026-05-04") // nur Montag
```

## API — Fetcher

| Funktion | Zweck |
|---|---|
| `FetchAndUpdate(raplaURL, weekStr, user, file)` | Rapla-HTML für eine Woche scrapen |
| `FetchDhbwApp(courseID, weekStr)` | api.dhbw.app als Alternativquelle |
| `ScrapeRooms(raplaURL)` | Nur Räume aus Rapla (Map `RoomKey → "Raum"`) |

`weekStr` ist im ISO-Format `"2026-W18"`. Leer = aktuelle Woche.

## API — Wochen-Helper

| Funktion | Zweck |
|---|---|
| `CanonicalWeek(weekStr)` | Normalisiert Eingabe zu `"YYYY-Www"` |
| `WeekWithOffset(base, n)` | `base="" + n=1` → nächste Woche, `n=-1` → letzte Woche |
| `DateToWeek(dateStr)` | `"2026-05-04"` → `"2026-W19"` |

## API — Merge & Export

```go
(t *Timetable) MergeWith(other *Timetable) *Timetable
(t *Timetable) ToJSON() TimetableJSON
(tj TimetableJSON) FilterByDate(date string) TimetableJSON
```

`MergeWith` deduplziert auf Basis der Startzeit — d.h. Rapla bleibt
maßgeblich, dhbw.app füllt nur Lücken.

## Datenmodell

```go
type Timetable struct {
    ClassID int
    Days    [7]WeekDay
}

type Lesson struct {
    Start, End time.Time
    Title      string
    Room       string
    Teacher    string
    LessonType string  // "" oder "Prüfung"
    IsOnline   bool
    IsCancelled bool
}
```

`TimetableJSON` ist die flachere Variante zum JSON-Encoden (alle Lectures in einer Liste).

## Hinweise

- Rapla-URLs gibt's pro DHBW-Standort und pro Kurs in zwei Formaten (alt:
  `?page=calendar`, neu: `/calendar`). `FetchAndUpdate` erkennt beides
  automatisch und upgraded auf HTTPS.
- Die Lib cached **nicht**. Bei häufigen Aufrufen selbst Cache vorschalten.
- `ScrapeRooms` ist ein optimierter Pfad wenn man nur Raum-Belegung braucht
  (für Raumsuche-UIs), nicht den ganzen Plan.
