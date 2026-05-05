# mensa

Scraper für Speisepläne der Seezeit-Mensen (Bodensee-Region: Konstanz, Friedrichshafen,
Weingarten, Ravensburg, etc.). Parsed das HTML der seezeit.com-Tagesansichten.

```
go get github.com/OfflineBot/nicht-libs/mensa
```

## Beispiel

```go
days, err := mensa.FetchWeekMenus("ravensburg")
if err != nil {
    log.Fatal(err)
}

for _, day := range days {
    fmt.Println(day.Date)
    for _, meal := range day.Meals {
        fmt.Printf("  %s — %s (%s)\n",
            meal.Category, meal.Name, meal.Prices.Students)
    }
}
```

## API

| Funktion | Zweck |
|---|---|
| `FetchWeekMenus(mensaID)` | Aktuelle Woche + Folgewoche, je `DayMenu` pro Tag |
| `ParseMensaDate(label)` | `"Mo. 30.03."` → `"2026-03-30"` (mit Jahres-Roll-over) |

`mensaID` ist der URL-Slug in `seezeit.com/essen/speiseplaene/mensa-<id>/` —
also z.B. `"ravensburg"`, `"friedrichshafen"`, `"konstanz"`.

## Datenmodell

```go
type DayMenu struct {
    Date  string  // "Mo. 30.03." (Original-Label)
    Meals []Meal
}

type Meal struct {
    Name     string
    Category string   // "Gericht 1", "Vegetarisch", ...
    Prices   Prices   // Students/Staff/Guests
    Types    []string // "vegan", "vegetarisch", "schwein", "rind", ...
}
```

## Hinweise

- Keine Caching-Logik in der Lib — bei Bedarf vom Konsument selbst (z.B. mit TTL pro Tag).
- Allergen-Codes in Klammern (z.B. `(25a,26)`) werden aus dem Gerichtsnamen entfernt.
- Wenn keine Diät-Symbole erkennbar sind, wird `["fleisch"]` als Default gesetzt.
