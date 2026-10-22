# Lektorat mit einem LLM

Der Copyedit-Bereich schickt die Seite an ein Sprachmodell und zeigt dessen Vorschläge an. Öffne ihn mit der Schaltfläche `Copyedit` (siehe [die Bereiche](panes.md)).

## Zuerst einrichten

Du brauchst mindestens eine Aufgabe und eine Verbindung. Beides findest du auf den [Konfigurationsseiten](settings.md); gespeichert wird es in deiner eigenen Einstellungsdatei, nicht im Projekt.

**Editing tasks** (Config, dann Copyedit: Editing tasks). Eine Aufgabe besteht aus einem Titel und einem Prompt, zum Beispiel „Passiv“ mit einer Anweisung, worauf zu achten ist. Du kannst Aufgaben hinzufügen, bearbeiten und löschen. Eine Aufgabe braucht Titel und Prompt. Der Bereich listet die Aufgaben nach Titel auf; der Prompt erscheint als Tooltip.

**API connections** (Config, dann Copyedit: API connections). Eine Verbindung hat einen Namen, eine API-Art, ein Modell und meist eine Basis-URL und einen Schlüssel. Du kannst beliebig viele speichern.

- `Anthropic` sendet an `<base URL>/messages`.
- `OpenAI-compatible` sendet an `<base URL>/chat/completions`. Das sprechen OpenAI, Ollama, LM Studio und OpenRouter. Ein lokales Modell braucht keinen Schlüssel.
- `GitHub Copilot` startet die GitHub-Copilot-CLI. Sie muss installiert sein, im `PATH` liegen (oder über `COPILOT_CLI_PATH` benannt sein) und angemeldet sein. Diese Art hat weder Basis-URL noch Schlüssel. Die CLI-Sitzung bekommt keine Werkzeuge und kann deshalb nur die Seite lesen, die ihr geschickt wird.

Du darfst den vollständigen Endpunkt als Basis-URL einfügen; der Pfad wird nicht doppelt angehängt. Ein gespeicherter Schlüssel wird nie an den Browser zurückgeschickt. Lass das Schlüsselfeld beim Bearbeiten leer, um ihn zu behalten.

## Aufgaben ausführen

- Das Dropdown `Model` oben im Bereich wählt die Verbindung. Deine Wahl wird gemerkt.
- Klicke auf eine Aufgabe, um sie auf die Seite anzuwenden.
- Oder hake mehrere Aufgaben an und drücke `Run selected`. Das Kästchen `Select all` hakt alle an oder ab. Deine Haken werden gemerkt.

Gesendet wird der Text, den der Editor enthält, einschließlich ungespeicherter Änderungen.

## Ein Aufruf oder ein Aufruf pro Aufgabe

Auf der Config-Seite für die Editing tasks legt die Moduseinstellung fest, wie mehrere angehakte Aufgaben laufen:

- `One call for all ticked tasks` (Standard) schickt die Seite einmal.
- `One call per ticked task` macht einen Aufruf nach dem anderen. Das Modell kann sich jeder Aufgabe voll widmen, und die Antworten lesen sich oft besser.

Jede Liste von Vorschlägen sagt, auf welchem Weg sie entstanden ist. Schlägt eine Aufgabe fehl, werden die anderen trotzdem angezeigt, und der Fehler wird benannt.

## Mit Vorschlägen arbeiten

Der Bereich wechselt von der Aufgabenliste zu den Vorschlägen. Jeder zeigt die zitierte Textstelle, was stattdessen stehen soll und warum. Bei mehreren Aufgaben sind die Vorschläge nach Aufgabe gruppiert. Chips über der Liste beschränken sie auf eine Aufgabe.

- Die Textstelle jedes Vorschlags ist im Editor markiert. Klicke auf einen Vorschlag, um im Editor dorthin zu springen.
- `Apply` schreibt den Ersatz an der Markierung in die Seite. Das wirkt wie Tippen, also funktionieren Autosave, Vorschau und Rückgängig wie gewohnt.
- `Done` markiert einen Vorschlag, den du von Hand umgesetzt hast. Jeder Vorschlag hat es. Vorschläge ohne Ersatz oder mit nicht auffindbarer Textstelle bieten nur `Done`.
- Ein Vorschlag, dessen Textstelle nicht auf der Seite steht, wird aufgelistet und weist darauf hin.
- `◀ Tasks` kehrt zur Aufgabenliste zurück und entfernt die Markierungen.

Vorschläge werden pro Seite aufbewahrt, während du an anderen Seiten arbeitest, sodass du zu ihnen zurückkehren kannst. Wenn du zur Aufgabenliste zurückgehst oder ein anderes Projekt öffnest, sind sie weg.

Fehler, etwa eine fehlende Verbindung oder eine Ablehnung durch die API, werden im Bereich angezeigt.

Zurück zum [Inhalt](index.md).
