// Package bookmaker flattens a subfolder of a Quarto website into a single
// .qmd document that Quarto can render as a book (PDF, DOCX, ...), plus a
// second document holding the slides those pages carry.
//
// A Quarto website expresses structure through nested folders, but a Quarto
// book project is flat: every chapter is one file and folder depth carries
// no meaning. Quarto also insists that a book's first chapter is the project
// root's index.qmd, so a book cannot simply start in a subfolder. The
// bookmaker resolves both restrictions by concatenating a folder's pages
// into one file in website order, shifting each page's headings to the level
// its folder depth implies.
//
// The package is a copy of the quarto-bookmaker command's internal package
// (https://github.com/christophberger-ailab/quarto-bookmaker, MIT), which
// cannot be imported directly because Go's internal rule keeps it inside its
// own module. It reached qm by way of quarto-sorter, which carried the same
// copy. Keep changes in sync with upstream.
package bookmaker
