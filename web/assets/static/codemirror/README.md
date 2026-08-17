# Vendored CodeMirror 5

CodeMirror 5.65.21, taken from the npm tarball
<https://registry.npmjs.org/codemirror/-/codemirror-5.65.21.tgz> and
flattened into this directory. The files are unmodified; only their paths
changed, which is safe because each one's UMD wrapper falls back to
`mod(CodeMirror)` in a plain browser environment and so resolves nothing at
run time.

CodeMirror 5 is the maintenance line. It is used here rather than
CodeMirror 6 because it ships plain, non-module scripts: they are dropped in
and loaded with `<script>` tags, the same way `marked.umd.js`,
`htmx.min.js`, and `sortable.min.js` are. CodeMirror 6 is ESM-only and would
require a bundler, and with it a Node build step in a Go repository.

| file | tarball path |
| --- | --- |
| `codemirror.js`, `codemirror.css` | `lib/` |
| `vim.js` | `keymap/vim.js` |
| `markdown.js`, `gfm.js`, `yaml.js`, `yaml-frontmatter.js`, `xml.js` | `mode/` |
| `css.js` | `mode/css/css.js` |
| `meta.js` | `mode/meta.js` |
| `overlay.js` | `addon/mode/overlay.js` |
| `dialog.js`, `dialog.css` | `addon/dialog/` |
| `searchcursor.js` | `addon/search/searchcursor.js` |
| `matchbrackets.js`, `continuelist.js` | `addon/edit/` |
| `LICENSE` | `LICENSE` |

The set is what the editors need and no more. `yaml-frontmatter` over `gfm`
is the mode a `.qmd` page is read with; `css` is used by the custom preview
stylesheet editor; `overlay` is what `gfm` composes with; `dialog`,
`searchcursor`, and `matchbrackets` are what the vim keymap builds on.
Per-language modes for fenced code blocks are deliberately left out -- that
set has no end -- so a fenced block is highlighted as plain text.

`TestEditorAssetsHaveTheirDependencies` in `web/server_test.go` reads the
`require(...)` calls out of these files and fails if one of them names a
file that is not here. A missing dependency otherwise surfaces only as a
run-time error in the browser, leaving the page with a bare textarea and no
indication of why.

## Upgrading

Download the tarball for the new version, copy the files in the table above
to the same names, and update the version at the top of this file. Then run
`go test ./web/` -- it checks that every dependency is present and that the
page loads them in dependency order -- and open a page in `qm web` to
confirm the editor still mounts.
