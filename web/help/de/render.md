# Rendern

Die Schaltfläche `Render` in der oberen Leiste öffnet das Render-Panel. Es führt denselben Ablauf aus wie der Befehl `qm render`, beide erzeugen also identische Ausgabe (siehe [die Kommandozeile](commands.md)).

## Die Matrix

Ein Rendering wird durch drei Dinge bestimmt:

- **Topics** (Themen): was gerendert wird. Ein Kästchen pro Thema, entnommen aus den `topic-*`-Profilen des Projekts.
- **Audiences** (Zielgruppen): für wen. Ein Thema mit zwei oder mehr Zielgruppen zeigt darunter je ein Kästchen. Ein Thema mit nur einer Zielgruppe zeigt keines und verwendet diese eine. Angeboten werden nur die Zielgruppen, die ein Thema deklariert.
- **Formats** (Formate): wie. Ein Kästchen pro Format, in der Gruppe `Formats`, entnommen aus den `format-*`-Profilen.

Jedes angehakte Thema wird einmal für jede angehakte Zielgruppe und jedes angehakte Format gerendert. Die Zielgruppen-Kästchen eines Themas sind abgeblendet, solange das Thema selbst nicht angehakt ist, du kannst sie aber trotzdem schon vorab ändern. Ein Thema, das du noch nie eingerichtet hast, startet mit allen Zielgruppen angehakt.

Deine Auswahl wird pro Projekt in der [Einstellungsdatei](settings.md) gemerkt.

## Ausführen

1. Hake an, was du willst.
2. Drücke unten im Panel auf `Render`.

Das Rendering läuft im Hintergrund. Das Panel zeigt das Log und aktualisiert es jede Sekunde, solange es läuft. Am Ende steht dort `Done` oder `Render failed`.

- Es läuft immer nur ein Rendering gleichzeitig.
- Das Log folgt der neuesten Zeile. Scrolle nach oben, um das Folgen zu beenden und deine Position zu halten; scrolle wieder ganz nach unten oder starte ein neues Rendering, um wieder zu folgen.

## Verwandtes

- Wohin die Ausgabe geht, bestimmen die Profile des Projekts. Wie ein Projekt eingerichtet wird, steht in der README des Projekts.
- Als Entwurf markierte Seiten gehören nicht zu dem, was ein Rendering veröffentlicht (siehe [der Seitenbaum](tree.md)).

Zurück zum [Inhalt](index.md).
