# Konfigurationsseiten und Einstellungsdatei

## Die Schaltfläche Config

`Config` ganz rechts in der oberen Leiste öffnet eine Seite, die die Konfigurationsbereiche auflistet. Mit der Schaltfläche `App` oben kehrst du zurück. Die Bereiche sind:

- `Preview: Custom CSS`: das Stylesheet für die [Vorschau](preview.md).
- `Copyedit: Editing tasks` und `Copyedit: API connections`: siehe [Lektorat](copyedit.md).
- `External editor`: siehe [andere Editoren](external-editor.md).

## Preview: Custom CSS

Hier schreibst du CSS, das die eingebauten Stile der Vorschau überschreibt. Es wird dokumentweit nach dem eigenen Stylesheet der App geladen. Damit es nur die Vorschau betrifft, begrenze deine Regeln mit `.markdown-preview`, zum Beispiel `.markdown-preview h1 { color: rebeccapurple; }`.

- Drücke `Save`, um es zu speichern. Ziehe an der Ecke des Editors, um ihn höher oder niedriger zu machen; die Höhe wird gemerkt.
- Um ein weiteres Stylesheet hinzuzufügen, tippe unter `Add an alternate stylesheet` einen Namen wie `dark.css` und drücke `Add stylesheet`. Bei mehr als einem Stylesheet wechselt ein Dropdown `Editing:` zwischen ihnen.
- Das aktive Stylesheet wählst du mit dem Dropdown unter der Vorschau.
- Regeln wie `.quarto.pol:before { content: "..." }` liefern auch die Symbole, die der [Seitenbaum](tree.md) für Spans zeigt.

Stylesheets sind Dateien mit dem Namen `<user config dir>/qm/custom-css/*.css`. Eine Standarddatei `custom.css` wird beim ersten Start von `qm web` angelegt.

## Die Einstellungsdatei

Alles, was sich `qm web` merkt, steht in einer einzigen Datei: `<user config dir>/qm/config.cue`. Sie enthält:

- die zuletzt geöffneten Projekte,
- die Render-Auswahl und die zuletzt geöffnete Seite jedes Projekts,
- das aktive Vorschau-Stylesheet,
- die Lektorat-Aufgaben und -Verbindungen (einschließlich API-Schlüsseln),
- die Kommandozeilen des externen Editors.

Wegen der Schlüssel ist sie nur für dich lesbar. Die Datei wird beim ersten Start mit Standardwerten angelegt. Ältere JSON-Einstellungsdateien werden einmalig übernommen und in `*.migrated` umbenannt.

## Von Hand bearbeiten

Du darfst die Datei selbst bearbeiten.

- Sie ist in CUE geschrieben und beginnt mit ihrem eigenen Schema, das also sagt, was hineingehört. Die App schreibt das Schema bei jedem Speichern neu.
- Weggelassene Felder nehmen ihre Standardwerte an. Weggelassene Listen und Maps sind leer.
- Ein falsch geschriebener Feldname oder ein falscher Wert ist ein Fehler, der mit Datei, Zeile und Spalte gemeldet wird.
- Kommentare, die du zwischen die Einstellungen schreibst, gehen beim nächsten Speichern der App verloren.

## Fehler halten die App an

Besteht die Datei die Prüfung nicht, hält `qm web` an, statt sie zu ersetzen, denn sie ist die einzige Kopie deiner Verbindungen und Auswahlen. Behebe die gemeldete Zeile und starte erneut. Ein Speichern, das die App selbst nicht wieder einlesen konnte, wird abgelehnt.

Zurück zum [Inhalt](index.md).
