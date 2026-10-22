# Opening files in other editors

`qm web` edits your `.qmd` pages. For everything else, such as `_quarto.yml`, filters, stylesheets, and images, you can hand files to another program.

## The Editor button

`Editor ↗` in the top bar opens the project in an external editor, such as VS Code or VSCodium. If a page is open, the editor is told to open that page too. If it cannot be started, the reason is shown next to the button.

## Setting the command

Go to `Config`, then `External editor`.

- The command is one command line. `{root}` stands for the project directory and `{file}` for the open page.
- The default is VS Code: `code {root} --goto {file}`.
- Buttons under `Presets:` fill in ready-made lines for common editors.
- With no page open, the `{file}` argument is left out, together with an option right before it (`--goto {file}` goes as a whole). If your editor cannot open a file inside a folder, leave `{file}` out of the command.
- Put quotes around an argument that contains spaces. A line with an unmatched quote is refused.
- The command is run directly, not through a shell, so it must be on your `PATH` or given with its full path.

## The image editor

Double-clicking an image in the [preview](preview.md) opens that image in an image editor. The second command on the same config page sets it.

- `{file}` stands for the image and is required. `{root}` stands for the project directory.
- The default, `System default`, opens the image in the program your system uses for it.
- Presets exist for GIMP, Krita, Inkscape, and Paint.
- When you save the image, the preview shows the new version without a reload.
- If the image editor cannot be started, the reason is shown next to the `Editor ↗` button.

Both commands are stored in the [settings file](settings.md).

Back to [Contents](index.md).
