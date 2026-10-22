# Config pages and the settings file

## The Config button

`Config` at the far right of the top bar opens a page listing the configuration areas. Use the `App` button at the top to return. The areas are:

- `Preview: Custom CSS`: the stylesheet for the [preview](preview.md).
- `Copyedit: Editing tasks` and `Copyedit: API connections`: see [copyediting](copyedit.md).
- `External editor`: see [other editors](external-editor.md).

## Preview: Custom CSS

Here you write CSS that overrides the preview's built-in styles. It is loaded document-wide after the app's own stylesheet. To affect only the preview, scope your rules with `.markdown-preview`, for example `.markdown-preview h1 { color: rebeccapurple; }`.

- Press `Save` to store it. Drag the corner of the editor to make it taller or shorter; the height is remembered.
- To add another stylesheet, type a name such as `dark.css` under `Add an alternate stylesheet` and press `Add stylesheet`. With more than one stylesheet, an `Editing:` dropdown switches between them.
- The active stylesheet is chosen with the dropdown below the preview.
- Rules like `.quarto.pol:before { content: "..." }` also supply the symbols the [page tree](tree.md) shows for spans.

Stylesheets are files named `<user config dir>/qm/custom-css/*.css`. A default `custom.css` is created the first time `qm web` runs.

## The settings file

Everything `qm web` remembers lives in one file: `<user config dir>/qm/config.cue`. It holds:

- the recently opened projects,
- the render selection and last open page of each project,
- the active preview stylesheet,
- the copyediting tasks and connections (including API keys),
- the external editor command lines.

It is readable by you alone because of the keys. The file is created with defaults on the first run. Older JSON settings files are carried over once and renamed to `*.migrated`.

## Editing by hand

You may edit the file yourself.

- It is written in CUE and begins with its own schema, so it says what belongs in it. The app rewrites the schema on every save.
- Fields you leave out take their defaults. Lists and maps left out are empty.
- A misspelled field name or a wrong value is an error, reported with file, line, and column.
- Comments you write among the settings are lost the next time the app saves.

## Errors stop the app

If the file does not check out, `qm web` stops instead of replacing it, because it is the only copy of your connections and selections. Fix the reported line and start again. A save that the app itself could not read back is refused.

Back to [Contents](index.md).
