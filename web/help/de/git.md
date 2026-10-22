# Das Git-Panel

Die Schaltfläche `Git` in der oberen Leiste, neben `Render`, öffnet ein Panel mit den Änderungen seit dem letzten Commit. Es führt das echte `git` aus, daher gelten deine Hooks, Credential Helper, Signierung und Konfiguration wie im Terminal. Eingabeaufforderungen sind nicht möglich; was ein Terminal braucht, schlägt mit einer Meldung fehl.

## Kein Git-Projekt

Liegt das Projekt nicht in einem Git-Arbeitsverzeichnis oder ist git nicht installiert, sagt das Panel das, statt Operationen anzubieten.

## Was du siehst

- Den Branch, seinen Upstream und wie viele Commits zu pushen oder im Rückstand sind.
- `Staged` listet auf, was der nächste Commit enthalten würde. `Not staged` listet auf, was er zurückließe. Eine Datei, die an beiden Stellen geändert ist, steht in beiden Listen.
- Jeder Eintrag zeigt seine Statusbuchstaben und seinen Pfad.

Das Panel umfasst den ganzen Arbeitsbaum. Liegt dein Projekt in einem Unterordner eines größeren Repositorys, werden auch Dateien außerhalb des Projekts aufgelistet und können gestaged werden.

## Staging

- `Stage` und `Unstage` in einer Zeile wirken auf eine einzelne Datei.
- `Stage all` und `Unstage all` wirken auf eine ganze Liste.

## Committen und pushen

- Tippe eine Nachricht in `Commit message` und drücke `Commit`. Committen ist nur möglich, solange etwas gestaged ist, und eine Nachricht ist Pflicht.
- `Push` schiebt den Branch zu seinem Remote. Ein Branch ohne Upstream wird mit gesetztem Upstream gepusht.
- `↻ Refresh` liest den Arbeitsbaum erneut.

Was git gesagt hat (Commit-Zusammenfassung, Push-Protokoll oder der Grund einer Ablehnung), wird unverändert angezeigt.

## Diffs

Klicke auf einen Pfad, um seinen Diff unter den Listen zu sehen. Er zeigt, wofür seine eigene Liste steht; eine Datei in beiden Listen hat also in jeder einen anderen Diff. Die Kopfzeile nennt die Datei, sagt `staged` oder `not staged` und zählt hinzugefügte und entfernte Zeilen. Neue, nicht verfolgte Dateien zeigen ihren gesamten Inhalt als hinzugefügt.

- `Open in editor` öffnet die Datei im Seiteneditor und schließt das Panel. Für gelöschte Dateien, Binärdateien und Dateien außerhalb des Projekts wird es nicht angeboten.
- `✕` schließt den Diff.
- Der Diff dient nur zum Lesen. Änderungen machst du im [Editor](editor.md).

Jede Operation leert den Diff, da die Datei in die andere Liste gewandert sein kann. Der Status wird beim Öffnen des Panels und nach jeder Operation gelesen, nie früher.

Zurück zum [Inhalt](index.md).
