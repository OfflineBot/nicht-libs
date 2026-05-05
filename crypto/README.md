# crypto

Argon2id-basierte Password-Hashes plus AES-GCM-Verschlüsselung für sensitive
Felder (Passwörter zu externen Diensten, persönliche Tokens). Zweck:
Server-side Hashing der Studenten-Passwörter und Verschlüsselung von
Drittanbieter-Credentials in der DB, sodass selbst bei DB-Leak nichts unmittelbar
nutzbar ist.

```
go get github.com/OfflineBot/nicht-libs/crypto
```

## Beispiel — Passwort-Hash

```go
salt, _ := crypto.DeriveArgonSalt("user@dhbw.de")
hash, _ := crypto.ServerArgon2id("geheim", "user@dhbw.de")
// hash in DB speichern
defer crypto.ZeroBytes(hash)
```

## Beispiel — Symmetrische Verschlüsselung

```go
ciphertext, _ := crypto.Encrypt("Moodle-Token-XYZ")
// in DB ablegen…

plaintext, _ := crypto.Decrypt(ciphertext)
defer crypto.ZeroString(plaintext)
```

`Encrypt`/`Decrypt` nutzen einen serverweiten Schlüssel aus der Env
(`CRYPTO_KEY`). Mit eigenem Schlüssel: `EncryptWithKey` / `DecryptWithKey`.

## API

| Funktion | Zweck |
|---|---|
| `DeriveArgonSalt(email)` | Deterministischer 16-Byte-Salt aus E-Mail + Server-Pepper |
| `ServerArgon2id(password, email)` | 32-Byte-Hash, deterministisch über (password, email) |
| `DeriveSecondHash(firstHash, randStr)` | Sekundäre Ableitung für Login-Challenges |
| `Encrypt(s)` / `Decrypt(s)` | AES-GCM mit Server-Key, gibt Hex-String zurück |
| `EncryptWithKey(s, key)` / `DecryptWithKey(s, key)` | Selbe Sache mit eigenem Key |
| `GenerateRandomString()` | 32-Byte CSPRNG, Hex-encoded |
| `DecodeHex32(s)` | 64-stelligen Hex-String → 32-Byte-Slice |
| `ZeroBytes(b)` / `ZeroString(s)` | Aktives Überschreiben sensibler Werte |

## Konfiguration

Env-Variablen (vom Konsumenten zu setzen):

- `SERVER_PEPPER` — Hex-String (mind. 32 Zeichen), wird in beide
  Salt-Ableitungs-Konstanten gemischt: `argon2-salt`, `crypto_key`
- `CRYPTO_KEY` — 64-stelliger Hex-String (32 Bytes), Master-Key für `Encrypt`/`Decrypt`

Ohne diese Variablen panicen die entsprechenden Funktionen mit klarer
Fehlermeldung beim ersten Aufruf — bewusst, damit Fehlkonfig nicht
silently durchläuft.

## Hinweise

- Argon2-Parameter sind hartcodiert: `time=2, memory=64MB, threads=4`. Wer
  härter parametrieren will, forkt. Default trifft die OWASP-Empfehlung
  (Stand 2024) und passt für übliche Server-Loads.
- Die Hashes sind **deterministisch** — gewollt, damit der Server bei jedem
  Login dasselbe Ergebnis kriegt ohne Salt aus der DB lesen zu müssen. Das
  ist ein Trade-off: bei DB-Leak sind Rainbow-Tables möglich falls der
  Server-Pepper auch leakt. Wenn das Bedrohungsmodell das nicht zulässt,
  ist die Lib falsch.
