# Projekte öffnen

Ein Projekt ist ein Quarto-Dokumentbaum. `qm web` zeigt jeweils ein Projekt.

## qm web starten

```sh
qm web                    # serve the project given by --project (default: the current directory)
qm web ../handbook        # serve that project instead
qm web --addr :9000       # listen elsewhere
```

- Ein Pfad nach `web` benennt das zu öffnende Projekt. Er hat Vorrang vor `--project`.
- Ohne Pfad ist das Projekt das, das `--project` (oder `-p`) angibt, sonst das aktuelle Verzeichnis.
- Der Befehl läuft, bis du ihn unterbrichst, und gibt die Adresse aus, unter der er erreichbar ist.

## Adresse und Port

- Standardmäßig lauscht `qm web` auf `localhost:8199`.
- Ist dieser Port belegt, meist durch ein anderes `qm web`, wird der nächsthöhere probiert und so weiter, bis ein freier gefunden ist. Die tatsächlich verwendete Adresse wird ausgegeben.
- Eine Adresse, die du mit `--addr` angibst, wird genau so verwendet. Ist sie belegt, ist das ein Fehler.

## Das Feld Open

Das Feld Open sitzt in der oberen Leiste.

- Es zeigt nur das letzte Element des Projektpfads, denn der Ordnername unterscheidet Projekte. Fahre mit der Maus darüber, um den vollen Pfad zu sehen.
- Tippe oder füge einen Pfad ein und drücke `Open`, um genau diesen Pfad zu öffnen.
- Die Schaltfläche `▾` neben dem Feld listet die zehn zuletzt geöffneten Projekte mit ihren vollen Pfaden auf, das neueste zuerst. Wähle eines, um das Feld zu füllen.
- Ein Projekt, das du über die Kommandozeile gestartet hast, gilt als geöffnet und erscheint beim nächsten Mal in der Liste.

Beim Öffnen eines Projekts wird alles aktualisiert, was dazugehört: der Seitenbaum, die Bereiche, das Render-Panel, das Git-Panel und die Schaltfläche Editor.

Die Liste der zuletzt geöffneten Projekte steht in der [Einstellungsdatei](settings.md).

Zurück zum [Inhalt](index.md).
