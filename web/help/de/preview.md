# Die Markdown-Vorschau

Die Vorschau zeigt die Seite, die du bearbeitest, und aktualisiert sich beim Tippen. Blende sie mit der Schaltfläche `Preview` ein oder aus (siehe [die Bereiche](panes.md)).

Die Vorschau ist eine Näherung. Sie wird im Browser aus deinem Markdown gezeichnet und ist kein Quarto-Render; für das echte Ergebnis nutze [Rendern](render.md).

## Was sie zeigt

- Quarto-Fenced-Divs (`::: name`) und Bracketed Spans (`[text]{.name}`) bekommen ihre Klasse, sodass dein eigenes CSS sie gestalten kann.
- Der Attributblock einer Überschrift, etwa `{.unnumbered}`, wird weggelassen, weil er das Rendering steuert und nicht zum Inhalt der Überschrift gehört. Überschriften in Codeblöcken bleiben unberührt.
- Eine Seite, die für eine Zielgruppe geschrieben ist, wird innerhalb des Divs dieser Gruppe angezeigt. Das gilt, wenn der Dateiname oder ein Ordner darüber auf `_FW` oder `_POL` endet (der tiefste Name gewinnt). Das Stylesheet färbt und markiert dann die ganze Seite, wie es bei einem `::: fw`- oder `::: pol`-Block geschieht. Das Frontmatter bleibt außerhalb des Divs.

## Bilder

Bilder werden aus deinem Projekt geladen.

- Ein Pfad, der mit `/` beginnt, etwa `/assets/images/x.png`, ist relativ zum Projektstamm.
- Jeder andere Pfad ist relativ zum Ordner der Seite.
- Ausgeliefert werden nur Bilddateien innerhalb des geöffneten Projekts.
- Ein Bild allein in seinem Absatz (Leerzeile davor und danach) zeigt seinen Alt-Text als Beschriftung darunter. Ein Bild im Fließtext oder ohne Alt-Text bekommt keine Beschriftung.
- Fahre mit der Maus über ein Bild, um seinen Pfad so zu sehen, wie er auf der Seite steht. Hat das Bild einen Markdown-Titel, steht dieser zuerst.
- Doppelklicke auf ein Bild, um es in deinem [Bildeditor](external-editor.md) zu öffnen. Wenn du in diesem Programm speicherst, übernimmt die Vorschau die neue Version innerhalb weniger Sekunden.
- `↻ Reload` zeigt Bilder so, wie sie auf der Platte liegen, auch wenn du eines unter demselben Namen ersetzt hast.

## Scrollen

Editor und Vorschau scrollen gemeinsam: Bewegst du eines von beiden, springt das andere zur selben Stelle der Seite. Die Vorschau bleibt auch beim Tippen beim Editor.

## Previous und Next

Unter der Vorschau öffnen `◀ Previous` und `Next ▶` die Seite vor oder nach der aktuellen, in der Reihenfolge des [Seitenbaums](tree.md). Der Tooltip jeder Schaltfläche nennt die Seite. An beiden Enden des Projekts ist die Schaltfläche deaktiviert.

## Eigenes CSS

Hast du mehr als ein eigenes Stylesheet, wechselt ein Dropdown unter der Vorschau zwischen ihnen. Die Wahl wird gemerkt, und die Vorschau lädt neu, um sie anzuwenden. Wie du die Stylesheets schreibst, steht unter [Konfigurationsseiten und Einstellungsdatei](settings.md).

Zurück zum [Inhalt](index.md).
