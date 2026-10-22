# Dateien in anderen Editoren öffnen

`qm web` bearbeitet deine `.qmd`-Seiten. Für alles andere, etwa `_quarto.yml`, Filter, Stylesheets und Bilder, kannst du Dateien an ein anderes Programm übergeben.

## Die Schaltfläche Editor

`Editor ↗` in der oberen Leiste öffnet das Projekt in einem externen Editor, etwa VS Code oder VSCodium. Ist eine Seite geöffnet, wird der Editor angewiesen, auch diese Seite zu öffnen. Lässt er sich nicht starten, steht der Grund neben der Schaltfläche.

## Den Befehl festlegen

Gehe zu `Config`, dann `External editor`.

- Der Befehl ist eine einzelne Kommandozeile. `{root}` steht für das Projektverzeichnis und `{file}` für die geöffnete Seite.
- Standard ist VS Code: `code {root} --goto {file}`.
- Die Schaltflächen unter `Presets:` füllen fertige Zeilen für gängige Editoren ein.
- Ist keine Seite geöffnet, entfällt das Argument `{file}` samt einer Option unmittelbar davor (`--goto {file}` entfällt als Ganzes). Kann dein Editor keine Datei innerhalb eines Ordners öffnen, lass `{file}` im Befehl weg.
- Setze Argumente mit Leerzeichen in Anführungszeichen. Eine Zeile mit nicht geschlossenem Anführungszeichen wird abgelehnt.
- Der Befehl wird direkt ausgeführt, nicht über eine Shell. Er muss also im `PATH` liegen oder mit vollem Pfad angegeben sein.

## Der Bildeditor

Ein Doppelklick auf ein Bild in der [Vorschau](preview.md) öffnet es in einem Bildeditor. Den zweiten Befehl auf derselben Config-Seite legst du dafür fest.

- `{file}` steht für das Bild und ist erforderlich. `{root}` steht für das Projektverzeichnis.
- Der Standard `System default` öffnet das Bild in dem Programm, das dein System dafür verwendet.
- Es gibt Presets für GIMP, Krita, Inkscape und Paint.
- Wenn du das Bild speicherst, zeigt die Vorschau die neue Version ohne Neuladen.
- Lässt sich der Bildeditor nicht starten, steht der Grund neben der Schaltfläche `Editor ↗`.

Beide Befehle werden in der [Einstellungsdatei](settings.md) gespeichert.

Zurück zum [Inhalt](index.md).
