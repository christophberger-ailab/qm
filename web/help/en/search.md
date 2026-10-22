# Searching

The search field in the top bar finds the pages of the open project that contain your words.

## How it works

- Searching uses a word index that is rebuilt in the background whenever the project changes, so typing is never slow. While the index is being built, the field says so and the search repeats until it is ready.
- Each term matches words that start with it. `edit` finds "editor".
- A page matches when it contains every term. Case is ignored by default.
- Single letters and digits are ignored, because they would match nearly everything.
- Put words in quotes, single or double, to search for a phrase: `"logging in"`. The words must stand next to each other in that order. While you type, the last word of a phrase still matches as a prefix; once you close the quote, it must match whole.
- A hyphen between two letters belongs to the word: `semi-wide` is found by `semi-wide` or `semi`, not by `wide`.
- Markdown and YAML symbols are not part of words. Words in any script, and emoji, can be searched.
- Only pages shown in the [page tree](tree.md) are searched.

## Match case

The `Aa` button next to the field makes the search case-sensitive, including the highlighting in the editor. It is remembered.

## Results

While a search is active, the tree is replaced by a list of the pages with hits, in the tree's order. Each entry shows the title, the path, and the number of hits. Click one to open it.

- The tree is kept underneath. Ending the search brings it back with its open branches, selection, and scroll position.
- The list stays while you edit or switch pages. The query is remembered across sessions.
- Press Enter in the field to search again and read the project afresh.

## Highlights

The hits of your query are highlighted in the [editor](editor.md). Opening a page scrolls to its first hit.

## Clearing

While the field holds a query, the `×` inside its right end clears it, and so does Escape in the field. Clearing ends the search and the tree returns.

Back to [Contents](index.md).
