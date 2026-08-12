// Package project reads and edits the page tree of a Quarto website: the
// .qmd pages, their `order` frontmatter, and their Markdown heading levels.
// Moving a page keeps all three in sync.
//
// The package comes from quarto-sorter
// (https://github.com/christophberger-ailab/quarto-sorter), which was merged
// into qm as the `qm web` command. It backs the web UI; the command-line
// subcommands read the tree through internal/qmcore instead.
package project
