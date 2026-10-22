# Suchen

Das Suchfeld in der oberen Leiste findet die Seiten des geöffneten Projekts, die deine Wörter enthalten.

## So funktioniert es

- Die Suche nutzt einen Wortindex, der im Hintergrund neu aufgebaut wird, sobald sich das Projekt ändert, sodass das Tippen nie langsam ist. Solange der Index aufgebaut wird, sagt das Feld das, und die Suche wird wiederholt, bis er bereit ist.
- Jeder Suchbegriff passt auf Wörter, die damit beginnen. `edit` findet „editor“.
- Eine Seite passt, wenn sie alle Begriffe enthält. Groß- und Kleinschreibung wird standardmäßig ignoriert.
- Einzelne Buchstaben und Ziffern werden ignoriert, weil sie fast alles treffen würden.
- Setze Wörter in einfache oder doppelte Anführungszeichen, um nach einer Wortfolge zu suchen: `"logging in"`. Die Wörter müssen in dieser Reihenfolge direkt nebeneinander stehen. Beim Tippen passt das letzte Wort einer Wortfolge noch als Präfix; sobald du das Anführungszeichen schließt, muss es vollständig passen.
- Ein Bindestrich zwischen zwei Buchstaben gehört zum Wort: `semi-wide` wird durch `semi-wide` oder `semi` gefunden, nicht durch `wide`.
- Markdown- und YAML-Symbole gehören nicht zu Wörtern. Wörter in jeder Schrift sowie Emoji lassen sich suchen.
- Durchsucht werden nur Seiten, die im [Seitenbaum](tree.md) angezeigt werden.

## Groß-/Kleinschreibung beachten

Die Schaltfläche `Aa` neben dem Feld macht die Suche zeichengenau bei Groß- und Kleinschreibung, einschließlich der Hervorhebung im Editor. Sie wird gemerkt.

## Ergebnisse

Solange eine Suche aktiv ist, ersetzt eine Liste der Seiten mit Treffern den Baum, in der Reihenfolge des Baums. Jeder Eintrag zeigt Titel, Pfad und Trefferzahl. Klicke auf einen, um ihn zu öffnen.

- Der Baum bleibt darunter erhalten. Beendest du die Suche, kommt er mit seinen offenen Zweigen, der Auswahl und der Scrollposition zurück.
- Die Liste bleibt, während du bearbeitest oder Seiten wechselst. Die Suchanfrage wird über Sitzungen hinweg gemerkt.
- Drücke Enter im Feld, um erneut zu suchen und das Projekt frisch einzulesen.

## Hervorhebungen

Die Treffer deiner Suchanfrage werden im [Editor](editor.md) hervorgehoben. Beim Öffnen einer Seite wird zum ersten Treffer gescrollt.

## Löschen

Solange das Feld eine Suchanfrage enthält, löscht das `×` an seinem rechten Ende sie, ebenso Escape im Feld. Löschen beendet die Suche, und der Baum kehrt zurück.

Zurück zum [Inhalt](index.md).
