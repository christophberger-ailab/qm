# Die qm-Kommandozeile im Überblick

`qm web` ist einer von mehreren Befehlen. Sie arbeiten alle am selben Quarto-Projekt.

```
qm <object> <subcommand> [flags] [args]
```

## Globales Flag

| Flag | Umgebungsvariable | Standard | Bedeutung |
| --- | --- | --- | --- |
| `--project`, `-p` | `QM_PROJECT` | `.` | Pfad zum Quarto-Dokumentbaum |

## Befehle

| Befehl | Was er tut |
| --- | --- |
| `qm render [<topic>…]` | rendert die Matrix aus Thema × Format × Zielgruppe des Projekts |
| `qm flatten <topic>` | schreibt die Build-Dokumente eines Themas, ohne zu rendern |
| `qm prepare` | Pre-Render-Hook: prüft die Auswahl und baut das Thema |
| `qm finalize` | Post-Render-Hook: verschiebt die Ausgabe dorthin, wo die Profile es vorgeben |
| `qm lint [<topic>]` | prüft die `.qmd`-Quellen (derzeit: nicht geschlossene `:::`-Fences) |
| `qm chapters insert <folder>` | gibt einer neuen Seite ihr `order:` und nummeriert die Geschwisterseiten neu |
| `qm chapters move <folder> <old> <new>` | verschiebt ein Kapitel an eine andere `order`-Position |
| `qm chapters remove <folder> <order>` | entfernt ein Kapitel nach Rückfrage |
| `qm web [<path>]` | startet die lokale Oberfläche zum Sortieren, Bearbeiten und Rendern |

Jeder Befehl kennt `--help`, das seine Flags und Argumente auflistet.

## In dieser Hilfe

- [Projekte öffnen](projects.md) behandelt `qm web`.
- [Rendern](render.md) ist die Web-Variante von `qm render`.

Zurück zum [Inhalt](index.md).
