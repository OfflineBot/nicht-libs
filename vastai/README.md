# vastai

Client für [Vast.ai](https://vast.ai) — GPU-Marktplatz zum Mieten von
GPU-Instanzen pro Stunde. Die Lib startet Instanzen mit einem Ollama-Image,
wartet bis sie warm sind, und bietet ein simples Lock-Pattern damit nicht
zwei Embeddings-Jobs gleichzeitig auf derselben Maschine laufen.

```
go get github.com/OfflineBot/nicht-libs/vastai
```

## Voraussetzung

Vast-API-Key in `~/.config/vastai/vast_api_key` ablegen oder als
`VAST_API_KEY` Env-Var setzen.

## Beispiel — Komplett-Flow für Embeddings

```go
err := vastai.RunEmbedding("nomic-embed-text", func() error {
    // hier ist die Instanz hochgefahren, Ollama läuft, Modell geladen
    // → Embeddings rufen via http://<ollama-url>/api/embeddings
    return nil
})
// nach Rückkehr wird die Instanz automatisch zerstört
```

`RunEmbedding` macht alles drum herum:

1. Globales Mutex setzen (`IsEmbeddingRunning()` → true)
2. Günstigste passende GPU finden
3. Instanz mieten + bootstrappen
4. Auf Ollama warten
5. User-Callback ausführen
6. Instanz zerstören (auch bei Panik)
7. Mutex freigeben

## Beispiel — Manueller Flow

```go
offer, _ := vastai.FindCheapGPU()
id, _ := vastai.RentInstance(offer.ID, "nomic-embed-text")
url, _ := vastai.WaitForOllama(id, "nomic-embed-text", 5*time.Minute)
// ... arbeite mit url ...
defer vastai.DestroyInstance(id)
```

## API — High-Level

| Funktion | Zweck |
|---|---|
| `RunEmbedding(model, fn)` | Komplett-Lifecycle, mit Lock und Cleanup |
| `IsEmbeddingRunning()` | Globaler Lock-Status |
| `HasAPIKey()` | Pre-Check ob die Lib überhaupt nutzbar ist |

## API — Low-Level

| Funktion | Zweck |
|---|---|
| `FindCheapGPU()` | Bestes Preis/Performance-Angebot suchen |
| `AcquireEmbedGPU(model)` | Find + Rent + Wait in einem |
| `RentInstance(offerID, model)` | Konkretes Angebot mieten + Bootstrap-Script setzen |
| `GetInstance(id)` / `GetInstances()` | Status abfragen |
| `WaitForOllama(id, model, timeout)` | Polling bis Ollama-Endpoint erreichbar |
| `OllamaURL(inst)` | URL aus den Instance-Ports zusammenbauen |
| `DestroyInstance(id)` | Instanz beenden (kostet sonst weiter) |

## Datenmodell

```go
type Offer struct {
    ID        int64
    GPUName   string
    NumGPUs   int
    DPHTotal  float64 // Dollar pro Stunde
    Reliability float64
}

type Instance struct {
    ID            int64
    Status        string // "loading", "running", "exited"
    PublicIPAddr  string
    Ports         []Port
    // ...
}
```

## Hinweise

- `RunEmbedding` ist **opinionated**: erwartet dass Ollama auf Port 8080 läuft
  und das angegebene Modell automatisch gepullt wird. Für andere Workloads
  besser die Low-Level-Funktionen direkt nutzen.
- Vast.ai berechnet sekundengenau — vergessene Instanzen kosten Geld. Die Lib
  löscht im `defer`, aber bei Force-Kill des Prozesses bleibt die Instanz oben.
  Also: nicht vergessen `vastcli show instances` regelmäßig anzuschauen, oder
  Watchdog im eigenen Code.
- Der Lock ist prozesslokal — wenn du mehrere Server-Instanzen parallel laufen
  hast, brauchst du einen externen Lock (z.B. DB-Row).
