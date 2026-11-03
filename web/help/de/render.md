# Rendern

Die Schaltfläche `Render` in der oberen Leiste öffnet das Render-Panel. Es führt denselben Ablauf aus wie der Befehl `qm render`, beide erzeugen also identische Ausgabe (siehe [die Kommandozeile](commands.md)).

## Die Matrix

Ein Rendering wird durch drei Dinge bestimmt:

- **Topics** (Themen): was gerendert wird. Ein Kästchen pro Thema, entnommen aus den `topics` in der `qm.yml` des Projekts. Ohne `qm.yml` ist jeder Inhaltsordner auf oberster Ebene ein Thema.
- **Audiences** (Zielgruppen): für wen. Ein Thema mit zwei oder mehr Zielgruppen zeigt darunter je ein Kästchen. Ein Thema mit nur einer Zielgruppe zeigt keines und verwendet diese eine. Angeboten werden nur die Zielgruppen, die ein Thema deklariert.
- **Formats** (Formate): wie. Ein Kästchen pro Format, in der Gruppe `Formats`, entnommen aus den `formats` der `qm.yml` (eine Website, ein Word-Dokument, ein PDF oder beides). Ohne `qm.yml` gibt es zwei: `web` und `docx`.

Jedes angehakte Thema wird einmal für jede angehakte Zielgruppe und jedes angehakte Format gerendert. Die Zielgruppen-Kästchen eines Themas sind abgeblendet, solange das Thema selbst nicht angehakt ist, du kannst sie aber trotzdem schon vorab ändern. Ein Thema, das du noch nie eingerichtet hast, startet mit allen Zielgruppen angehakt.

Ein Website-Format ist die Ausnahme von „einmal pro Thema“: Es fasst alle angehakten Themen in einer Website zusammen und schreibt für jede angehakte Zielgruppe eine eigene. Ein Thema, das an einem angehakten Format nicht teilnimmt, wird übersprungen.

Deine Auswahl wird pro Projekt in der [Einstellungsdatei](settings.md) gemerkt.

Lässt sich die `qm.yml` nicht lesen, zeigt das Panel statt der Matrix den Fehler. Korrigiere die Datei und öffne das Panel erneut.

## Ausführen

1. Hake an, was du willst.
2. Drücke unten im Panel auf `Render`.

Das Rendering läuft im Hintergrund. Das Panel zeigt das Log und aktualisiert es jede Sekunde, solange es läuft. Am Ende steht dort `Done` oder `Render failed`.

- Es läuft immer nur ein Rendering gleichzeitig.
- Das Log folgt der neuesten Zeile. Scrolle nach oben, um das Folgen zu beenden und deine Position zu halten; scrolle wieder ganz nach unten oder starte ein neues Rendering, um wieder zu folgen.

## Verwandtes

- Wohin die Ausgabe geht, bestimmt die `qm.yml`: standardmäßig unter `_output`, in einem Ordner pro Format. Wie ein Projekt eingerichtet wird, steht in der README des Projekts.
- Inhalte lassen sich für ein Format oder eine Zielgruppe markieren; ein Rendering behält nur, was die Auswahl zeigt.
- Als Entwurf markierte Seiten gehören nicht zu dem, was ein Rendering veröffentlicht (siehe [der Seitenbaum](tree.md)).

Zurück zum [Inhalt](index.md).
