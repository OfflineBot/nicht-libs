# dualis

Scraper für das Dualis-Notenportal der DHBW. Loggt sich mit
Studierendenzugang ein und liest Semester, Module, Klausurversuche
und Teilkomponenten aus.

```
go get github.com/OfflineBot/nicht-libs/dualis
```

## Beispiel

```go
sess, err := dualis.Login("user@dhbw.de", "passwort")
if err != nil {
    log.Fatal(err)
}

all, err := dualis.GetAllGrades(sess)
for _, mod := range all.Current.Modules {
    fmt.Printf("%-40s  %s\n", mod.Title, dualis.FormatGrade(mod.Grade))
}
```

## API

| Funktion | Zweck |
|---|---|
| `Login(user, pass)` | Session aufbauen, gibt `*Session` zurück |
| `GetSemesters(s)` | Liste aller belegten Semester |
| `GetGradesBySemester(s, semID)` | Module + Noten für ein einzelnes Semester |
| `GetAllGrades(s)` | Alle Semester auf einmal (nutzt mehrere Requests) |
| `GetGradeDetails(s, args)` | Einzelne Klausurversuche eines Moduls |
| `GetStudentName(s)` | Vor- und Nachname aus dem Login-Profil |
| `FormatGrade(g)` | `1.7` → `"1,7"` (deutsche Schreibweise, leer bei 0.0) |
| `PercentToGrade(p)` | DHBW-Notenskala, z.B. `87.5` → `1.7` |
| `ComponentsPreliminaryGrade(comps)` | Vorläufige Note aus Teilleistungen rechnen |

`ErrAuthFailed` wird zurückgegeben wenn `Login` mit falschen Daten aufgerufen wird —
ansonsten kommen Fehler von der Dualis-Seite (HTTP-Fehler, geänderte HTML-Struktur etc.)
durch.

## Hinweise

- Session-Cookies werden im `Session`-Objekt gehalten und nach ca. 30min ungültig.
  Bei längeren Läufen einfach neu einloggen.
- Dualis liefert die Login-Seite über mehrere Redirects — `Login` folgt diesen
  automatisch.
