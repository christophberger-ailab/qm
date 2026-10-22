# The Git panel

The `Git` button in the top bar, next to `Render`, opens a panel showing the changes since the last commit. It runs the real `git` command, so your hooks, credential helpers, signing, and config apply as in a terminal. No prompts are possible, so anything that needs a terminal will fail with a message.

## Not a Git project

If the project is not inside a Git working tree, or git is not installed, the panel says so instead of offering operations.

## What you see

- The branch, its upstream, and how many commits are to push or behind.
- `Staged` lists what the next commit would carry. `Not staged` lists what it would leave behind. A file changed in both places is in both lists.
- Each entry shows its status letters and its path.

The panel covers the whole working tree. If your project is a subfolder of a larger repository, files outside the project are listed and can be staged too.

## Staging

- `Stage` and `Unstage` on a row act on one file.
- `Stage all` and `Unstage all` act on a whole list.

## Committing and pushing

- Type a message in `Commit message` and press `Commit`. Committing is offered only while something is staged, and a message is required.
- `Push` pushes the branch to its remote. A branch with no upstream yet is pushed with the upstream set.
- `↻ Refresh` reads the working tree again.

What git said (commit summary, push transcript, or the reason it was refused) is shown as it stands.

## Diffs

Click a path to see its diff below the lists. It shows what its own list stands for, so a file in both lists has a different diff in each. The header names the file, says `staged` or `not staged`, and counts added and removed lines. New untracked files show all their content as added.

- `Open in editor` opens the file in the page editor and closes the panel. It is not offered for deleted files, binary files, or files outside the project.
- `✕` closes the diff.
- The diff is for reading only. Make changes in the [editor](editor.md).

Every operation clears the diff, since the file may have moved to the other list. The status is read when you open the panel and after each operation, never earlier.

Back to [Contents](index.md).
