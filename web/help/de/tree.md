# Der Seitenbaum

Die linke Seite des Fensters zeigt die `.qmd`-Seiten des Projekts als Baum, in der Reihenfolge der Website und nach Ordnern verschachtelt. Klicke auf einen Titel, um die Seite im [Editor](editor.md) zu öffnen.

## Titel

Der Titel einer Seite stammt in dieser Reihenfolge aus:

- dem `title:` in ihrem Frontmatter,
- dem Text ihrer ersten Markdown-Überschrift,
- einem aus dem Dateinamen gebildeten Titel.

Der Attributblock einer Überschrift, etwa `{.unnumbered}`, wird aus dem Titel weggelassen. Ein Quarto-Span wie `[Polizei-]{.pol}` wird als das Symbol angezeigt, das seine Klasse im aktiven [eigenen Vorschau-Stylesheet](preview.md) trägt, gefolgt vom Text des Spans. Spans, deren Klasse kein Symbol hat, werden so angezeigt, wie sie geschrieben sind.

## Entwürfe

Eine Seite mit `draft: true` im Frontmatter wird ausgegraut. Sie gehört zum Projekt, aber nicht zu dem, was ein Rendering veröffentlicht. Du kannst sie weiterhin öffnen, verschieben und durchsuchen.

## Statusmarken

Eine Seite kann nach ihrem Titel Statusmarken tragen:

- 🚧 zu erledigen: `todo: true` unter `qm:` im Frontmatter, oder der Text `==TODO==` irgendwo in der Seite, auch in einem HTML-Kommentar.
- 👀 zu prüfen: `review: true` unter `qm:` im Frontmatter.

```yaml
---
title: Meine Seite
qm:
  todo: true
  review: true
---
```

Jede Marke erscheint einmal pro Seite, egal wie viele `==TODO==` die Seite enthält.

## Seiten ausblenden

Eine Seite mit `hide: true` unter `qm:` im Frontmatter erscheint nicht im Baum. Standard ist `false`. Eine ausgeblendete Abschnittsseite blendet ihren ganzen Zweig aus. Das Ausblenden betrifft nur den Baum: Die Seite wird weiterhin gerendert, und die [Suche](search.md) findet sie, sodass du sie dort öffnen und die Einstellung wieder entfernen kannst.

```yaml
---
title: Meine Seite
qm:
  hide: true
---
```

## Umordnen und Verschieben

Ziehe eine Seite am Griff `⠿`.

- Legst du sie zwischen ihren Geschwisterseiten ab, wird das `order`-Frontmatter der betroffenen Seiten neu geschrieben.
- Legst du sie unter einem anderen Elternelement ab, wird zusätzlich ihre Datei auf der Platte umbenannt, und die Markdown-Überschriftenebenen der verschobenen Seite werden an die neue Tiefe angepasst.
- Wird die geöffnete Seite verschoben, folgt ihr der Editor zum neuen Pfad. Dein ungespeicherter Text bleibt unberührt, und Autosave schreibt weiter in die richtige Datei.

## Seiten anlegen

- Tippe oben einen Dateinamen (ohne `.qmd`) und optional einen Titel und drücke dann `＋ Page`.
- Die Schaltfläche `＋` in einer Zeile fügt unter dieser Seite eine Unterseite hinzu. Du wirst nach dem Namen gefragt.

## Seiten löschen

Die Schaltfläche `🗑` in einer Zeile fragt nach Bestätigung und verschiebt die Seite dann in den Papierkorb.

## Auf- und Zuklappen

Klicke auf den kleinen Schalter in einer Zeile, um einen Zweig zu- oder aufzuklappen. Die Schaltflächen `⊞` und `⊟` unter dem Baum klappen alle Zweige auf oder zu.

## Änderungen von außen

Der Baum prüft das Projekt auf Änderungen außerhalb des Browsers und aktualisiert sich selbst, wenn sich etwas geändert hat.

Zurück zum [Inhalt](index.md).
