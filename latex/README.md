# latex

Markdown → LaTeX → PDF. Konvertiert Markdown (mit erweiterten HTML-Kommentaren
für Figuren/Tabellen) zu einem fertigen XeLaTeX-Dokument und rendert es via
`xelatex`-Aufruf zu PDF-Bytes.

```
go get github.com/OfflineBot/nicht-libs/latex
```

## Voraussetzung

`xelatex` muss im `PATH` sein. Auf Arch:

```
sudo pacman -S texlive-xetex texlive-latexextra
```

Außerdem werden DejaVuSans-Fonts unter `/usr/share/fonts/TTF/` erwartet.
Für anderen Fontpfad: Lib forken oder `RenderPDF` direkt aufrufen mit eigenem Preamble.

## Beispiel

```go
md := `# Übung 1

Definiere $f(x) = x^2$.

| Schritt | Ergebnis |
|---|---|
| 1 | 4 |
| 2 | 9 |
`

tex := latex.MarkdownToLatex("Mathematik HA1", md)
pdf, err := latex.RenderPDF([]byte(tex))
if err != nil {
    log.Fatal(err)
}
os.WriteFile("ha1.pdf", pdf, 0644)
```

## API

| Funktion | Zweck |
|---|---|
| `MarkdownToLatex(title, md)` | Markdown → kompletter XeLaTeX-Source mit Preamble |
| `RenderPDF(latexSrc)` | XeLaTeX-Source → PDF-Bytes (läuft `xelatex` zweimal) |

## Markdown-Erweiterungen

Die Lib versteht zusätzlich zu Standard-Markdown ein paar HTML-Kommentare:

```markdown
<!-- FIGURE: Schaltbild eines Operationsverstärkers -->
<!-- TABLE: Wahrheitstabelle XOR -->
<!-- SOLUTION -->
Lösungsweg hier...
<!-- /SOLUTION -->
```

→ wird zu nummerierten Abbildungen/Tabellen mit eigenem Style und
abgesetzten Lösungs-Boxen.

## Hinweise

- Temp-Verzeichnis wird unter `/tmp/zettel-latex-*` angelegt und nach Render
  wieder gelöscht.
- `xelatex` wird mit `-interaction=nonstopmode -halt-on-error` gerufen — bei
  LaTeX-Fehlern bekommt der Caller den Log als `error`-Message zurück.
- Zwei-Pass-Rendering ist hartcodiert (für korrekte Querverweise). Bei
  einfachem Content kannst du den zweiten Lauf nicht ausschalten.
