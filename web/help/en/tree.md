# The page tree

The left side of the window shows the project's `.qmd` pages as a tree, in the order of the website, nested by folder. Click a title to open the page in the [editor](editor.md).

## Titles

A page's title is taken from, in this order:

- the `title:` in its frontmatter,
- the text of its first Markdown heading,
- a title made from its file name.

A heading's own attribute block, such as `{.unnumbered}`, is dropped from the title. A Quarto span such as `[Polizei-]{.pol}` is shown as the symbol its class carries in the active [custom preview stylesheet](preview.md), followed by the span's text. Spans whose class has no symbol are shown as written.

## Drafts

A page with `draft: true` in its frontmatter is greyed out. It is part of the project but not part of what a render publishes. You can still open, move, and search it.

## Status marks

A page can carry status marks after its title:

- 🚧 to do: `todo: true` under `qm:` in the frontmatter, or the text `==TODO==` anywhere in the page, even inside an HTML comment.
- 👀 to review: `review: true` under `qm:` in the frontmatter.

```yaml
---
title: My page
qm:
  todo: true
  review: true
---
```

Each mark is shown once per page, however many `==TODO==` the page holds.

## Reordering and moving

Drag a page by its `⠿` handle.

- Dropping it among its siblings rewrites the `order` frontmatter of the affected pages.
- Dropping it under another parent also renames its file on disk and shifts the Markdown heading levels of the moved page to fit its new depth.
- If the page you have open moves, the editor follows it to the new path. Your unsaved text stays untouched, and autosave keeps writing to the right file.

## Creating pages

- Type a file name (without `.qmd`) and an optional title at the top, then press `＋ Page`.
- The `＋` button on a row adds a child page under that page. You are asked for the name.

## Deleting pages

The `🗑` button on a row asks for confirmation and then moves the page to the trash.

## Expanding and collapsing

Click the small toggle on a row to collapse or expand a branch. The `⊞` and `⊟` buttons below the tree expand or collapse every branch.

## Outside changes

The tree checks the project for changes made outside the browser and refreshes itself when something changed.

Back to [Contents](index.md).
