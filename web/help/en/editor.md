# Editing a page

Click a page in the [page tree](tree.md) to open it in the editor. The editor highlights the page's YAML frontmatter and Markdown.

## Autosave

Your changes are saved automatically a moment after you stop typing. Leaving a page, reloading, or closing the tab while an edit is unsaved asks for confirmation.

If a save fails, the reason is shown as an error next to the page heading.

## Renaming the file

The heading above the editor shows the page's path. The `✎` button after it turns the file name, and only the file name, into a field: the page stays in its folder. Type the new name and press `Enter`; `.qmd` is added if you leave it out. `Esc`, or clicking elsewhere, cancels.

- If the page has child pages in a folder of the same name, that folder is renamed too, so the children stay with it.
- A name that is already taken is refused, and the field stays open so you can correct it.
- The editor follows the file to its new name. Your unsaved text stays untouched, and autosave keeps writing to the right file.
- A section's `index.qmd` stands for its folder and cannot be renamed here.

## Moves while you edit

If you move or create pages in the tree while a page is open, the tree changes `order:` and heading levels in that file. Your next autosave keeps both your text and the move. An `order:` that you typed into the frontmatter yourself is kept over the one the tree wrote. When this happens you are told, and `↻ Reload` brings the editor in line with the file.

## Conflicts

If the file was changed outside the browser in a way the tree cannot account for, the save is refused and the file is left as it is. You then choose:

- `↻ Reload` loads the version from disk and drops your editor text.
- `Overwrite` writes the editor's text over the file on disk.

## Reload

`↻ Reload` in the content header reloads the page from disk. Images in the preview are fetched again too.

## Vim mode

The `⌨ Vim Mode` button below the editor switches Vim keybindings on and off. It is off by default and your choice is remembered. In Vim mode, `:w` saves the page immediately instead of waiting for autosave.

## Link and image completion

When you type the destination of a Markdown link, a popup below the cursor offers matching entries.

- `[text](` offers `.qmd` pages, `![alt](` offers image files. Folders are offered in both cases so you can go down a level at a time.
- A destination starting with `/` completes from the project root. Any other completes from the folder of the page you are editing.
- What you type matches anywhere in a name; names that match at the start come first.
- Up and Down move the selection, Tab or Enter inserts it, Esc closes the popup.
- A destination containing a space is wrapped in `<...>` automatically.

## Remembered page

The page you had open is remembered for each project and reopened when you come back, including after a restart. It is selected in the tree.

Back to [Contents](index.md).
