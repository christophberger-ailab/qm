// Package site renders a static website from a project's content folders.
//
// Each topic folder becomes one menu entry in the top navigation bar; the
// current topic's page tree forms the sidebar, and the headings of the
// current page form the table of contents on the right. All links in the
// output are relative, so the site works from file:// and below any URL
// prefix.
package site
