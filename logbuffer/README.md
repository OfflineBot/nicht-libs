# logbuffer

In-Memory Ring-Buffer für die letzten 1000 `slog`-Einträge. Klinkt sich als
`slog.Handler` ein, sodass jedes `slog.Info(...)` / `slog.Warn(...)` zusätzlich
zur normalen Ausgabe im RAM landet — abrufbar z.B. für ein Admin-Dashboard,
ohne sich SSH'en und Logfiles tailen zu müssen.

```
go get github.com/OfflineBot/nicht-libs/logbuffer
```

## Beispiel

```go
// Beim Startup einmal die Default-Logger ersetzen:
inner := slog.NewJSONHandler(os.Stderr, nil)
slog.SetDefault(slog.New(logbuffer.NewHandler(inner)))

// Irgendwo:
slog.Info("user logged in", "user_id", 42)

// Im Admin-Endpoint:
recent := logbuffer.GetRecent(100)
return c.JSON(recent)
```

## API

| Funktion | Zweck |
|---|---|
| `NewHandler(inner)` | Wrapper-Handler — schreibt in den Buffer **und** delegiert an `inner` |
| `GetRecent(n)` | Letzte `n` Einträge in chronologischer Reihenfolge |

## Datenmodell

```go
type Entry struct {
    Time    time.Time
    Level   string            // "INFO", "WARN", ...
    Message string
    Attrs   map[string]string // alle slog.Attr als String-Map
}
```

## Hinweise

- Buffer-Größe ist hartcodiert auf 1000 Einträge (Ring überschreibt die ältesten).
  Anpassbar nur via Fork — bewusst klein gehalten, kein Memory-Leak-Risiko.
- Thread-safe via `sync.RWMutex`.
- `Attrs` werden als String serialisiert — bei strukturierten Werten verlierst
  du Typinformation. Wenn das stört, eigenen Handler bauen statt diesen zu nutzen.
- Die Lib hält **globalen** State (ein Buffer pro Prozess). Mehrere
  Logger-Hierarchien teilen sich denselben Puffer.
