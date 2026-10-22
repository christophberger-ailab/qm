# Eine Seite bearbeiten

Klicke im [Seitenbaum](tree.md) auf eine Seite, um sie im Editor zu öffnen. Der Editor hebt das YAML-Frontmatter und das Markdown der Seite farbig hervor.

## Autosave

Deine Änderungen werden automatisch kurz nach dem Tippen gespeichert. Wenn du eine Seite verlässt, neu lädst oder den Tab schließt, während eine Änderung ungespeichert ist, wirst du um Bestätigung gebeten.

Schlägt das Speichern fehl, erscheint der Grund als Fehler neben der Seitenüberschrift.

## Verschiebungen während der Bearbeitung

Wenn du im Baum Seiten verschiebst oder anlegst, während eine Seite geöffnet ist, ändert der Baum `order:` und Überschriftenebenen in dieser Datei. Dein nächstes Autosave behält Text und Verschiebung. Ein `order:`, das du selbst ins Frontmatter geschrieben hast, hat Vorrang vor dem, das der Baum geschrieben hat. In diesem Fall wirst du benachrichtigt, und `↻ Reload` bringt den Editor wieder mit der Datei in Einklang.

## Konflikte

Wurde die Datei außerhalb des Browsers so geändert, dass der Baum es nicht berücksichtigen kann, wird das Speichern abgelehnt und die Datei bleibt unverändert. Dann entscheidest du:

- `↻ Reload` lädt die Version von der Platte und verwirft deinen Editortext.
- `Overwrite` schreibt den Text des Editors über die Datei auf der Platte.

## Neu laden

`↻ Reload` im Inhaltskopf lädt die Seite neu von der Platte. Auch Bilder in der Vorschau werden neu geholt.

## Vim-Modus

Die Schaltfläche `⌨ Vim Mode` unter dem Editor schaltet die Vim-Tastenbelegung ein und aus. Sie ist standardmäßig aus, und deine Wahl wird gemerkt. Im Vim-Modus speichert `:w` die Seite sofort, statt auf das Autosave zu warten.

## Vervollständigung von Links und Bildern

Wenn du das Ziel eines Markdown-Links tippst, bietet ein Popup unter dem Cursor passende Einträge an.

- `[text](` bietet `.qmd`-Seiten an, `![alt](` Bilddateien. In beiden Fällen werden Ordner angeboten, damit du Ebene für Ebene hinabsteigen kannst.
- Ein Ziel, das mit `/` beginnt, wird ab dem Projektstamm vervollständigt. Jedes andere ab dem Ordner der Seite, die du bearbeitest.
- Was du tippst, passt irgendwo in einem Namen; Namen, die am Anfang passen, stehen zuerst.
- Hoch und Runter bewegen die Auswahl, Tab oder Enter fügt sie ein, Esc schließt das Popup.
- Ein Ziel mit Leerzeichen wird automatisch in `<...>` gesetzt.

## Gemerkte Seite

Die zuletzt geöffnete Seite wird pro Projekt gemerkt und beim nächsten Mal wieder geöffnet, auch nach einem Neustart. Sie ist im Baum ausgewählt.

Zurück zum [Inhalt](index.md).
