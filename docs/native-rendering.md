# Native document rendering

## Library research (2026-09-30)

| Library | Finding | Decision |
| --- | --- | --- |
| [signintech/gopdf](https://github.com/signintech/gopdf) | MIT; repository pushed September 2026; Unicode TrueType embedding, PNG/JPEG, links and outlines | PDF backend, pinned to v0.38.1 |
| [johnfercher/maroto](https://github.com/johnfercher/maroto) | MIT; repository pushed September 2026; higher-level grid layout | Viable alternative; direct gopdf gives control over flowing book text |
| [gomutex/godocx](https://github.com/gomutex/godocx) | MIT; repository last pushed August 2025 | Investigated, but not added as a dependency |
| [fumiama/go-docx](https://github.com/fumiama/go-docx) | AGPL-3.0; repository last pushed May 2025 | Not selected |
| [unidoc/unioffice](https://github.com/unidoc/unioffice) | Active, commercial licensing | Not selected |

Repository activity is a snapshot, not a maintenance guarantee. The selected
DOCX writer is **Pandoc**, already bundled with Quarto, following the user's
choice to retain its mature document support. Go's ZIP/XML standard library
implements the narrow cover insertion step.

## Why capture JSON during DOCX rendering?

Quarto book projects reject `--to json`. Rendering Markdown and parsing it again
would also lose structure and require another interpretation of attributes.
Instead, the format profile renders DOCX only and declares a Lua filter at
Quarto's last entry point, `post-finalize`. The filter, written to `_build/` by
`qm prepare`, saves the filtered Pandoc AST to `_build/qm-document.json`, and
`qm finalize` lays out the PDF from it. Quarto's existing audience
filter, includes, variables and book assembly run normally once. The final AST
contains some DOCX-specific callout layout fragments: these are accepted only
when they contain known layout elements, while content-bearing unsupported
raw XML reports an error.
The PDF reads the colors of such a callout from Quarto's markup (the left
border color and the title cell's shading), so it uses Quarto's own values for
note, tip, warning, important and caution, and draws the callout as a box.

Because the whole pipeline runs in the project's pre- and post-render hooks, a
plain `quarto render --profile …` produces the same PDF and DOCX as `qm render`.
The AST captured at `post-finalize` differs from one captured by a command-line
`--lua-filter` only by Quarto's empty, hidden file-metadata `Div`. The filter
drops that `Div`, so the two captures are byte-identical.

`internal/bookrender` owns the pipeline; `internal/docrender` owns PDF layout
and DOCX cover insertion. `qm prepare`, `qm finalize`, `qm render` and `qm web`
share it.

## Cover packaging

The DOCX cover uses a page-relative anchored DrawingML image in a separate
zero-margin section. The image keeps its aspect ratio, shrinks to fit the page
and is anchored at the bottom-right corner; it is never cropped. The original body and final section XML are
preserved, and existing ZIP parts remain intact. A unique image relationship
and media part are added. The ZIP is staged before replacing the document.

Pandoc's reference document cannot supply this cover itself: its body content
is ignored, while its styles and document properties are reused. See the
[Pandoc reference-doc documentation](https://pandoc.org/MANUAL.html#option--reference-doc).

## Verification

```sh
go test ./...
go vet ./...
QM_TEST_QUARTO=1 go test ./internal/bookrender -run TestNativeQuartoIntegration -v
```

The integration test builds qm, wires it into a real Quarto book's hooks and
runs a plain `quarto render`. It covers audience filtering, a table,
Unicode text, PDF generation and full-page DOCX cover insertion. Unit tests
verify DOCX package preservation, XML geometry, PDF pagination and refusal to
silently discard unsupported document content.

Training project checks: Einführung handout/handbook and Calltaker FW/POL
handouts rendered successfully. PDF cover/body samples were rasterized with
Poppler and visually inspected; the DOCX passed XML/ZIP tests and a Pandoc
read-back. Visual DOCX verification remains outstanding: the locally extracted
LibreOffice build could not start in the test environment. The sysadmin render
stopped during Quarto preprocessing because Chrome (needed for diagrams) was
not installed; this dependency is independent of PDF typesetting.

## Without Quarto: `qm: renderer: pandoc` (2026-10-28)

The pipeline above runs Pandoc *inside* Quarto: Quarto reads the book, runs
its filters, and writes the DOCX, and the project's hooks capture the document
and lay out the PDF. A format with `qm: renderer: pandoc` takes Quarto out of
the loop. `qm` flattens the topic itself, resolves the few shortcodes the
content uses, runs Pandoc with the project's own filter chain, and lays out
the PDF from the document that chain captures. See
[the README](../README.md#documents-without-quarto-qm-renderer-pandoc).

Why this is safe to rely on. The renderer was checked against Quarto's output
for the same selections:

- **DOCX** — for 9 selections (calltaker, dispatcher, Einführung,
  Führungskräfte, Spickzettel, Fachadmin and Auswertung; handouts and
  handbooks; both agencies; the largest with 182 tables and 265 images) every
  part of the package is identical to Quarto's: `document.xml`, styles,
  numbering, settings, footer, fonts and media. What was ignored is cosmetic:
  XML whitespace, a duplicated `tblLook` element Quarto writes, the timestamps
  and custom properties, and the trailing space Quarto leaves in a title whose
  audience label is empty.
- **PDF** — laid out by the same Go code from the same document. For the
  calltaker handout the captured document is identical to Quarto's, and the
  page counts of four large PDFs match Quarto's exactly.
- **PPTX** — three decks (calltaker, dispatcher dark, sysadmin): slides,
  notes and media match; every part is identical except one link in the
  calltaker deck that Quarto rewrites relative to `_build/`.

What the comparison turned up, and the renderer therefore does as Quarto does:

- Quarto reads the content with Pandoc's own Markdown (minus
  `table_attributes`), not with a dialect of its own. A list that directly
  follows a paragraph is not a list, in both.
- A callout is, in a DOCX, a table assembled from raw XML spread over the
  blocks around it. The first body paragraph gets space above, the last one
  below. Word merges tables that touch, so a callout after a table or another
  callout is preceded by an empty paragraph. A callout without a type is a
  bare gray bar whose first line is the callout's title, when it has one
  (`## Heading` as the first line of a `::: {.callout}` is that title).
- For slides, Quarto drops empty paragraphs and block-level HTML comments
  (they change the layout Pandoc picks for a slide), and a callout becomes a
  block quote headed by its title in bold.

Known gaps: Mermaid diagrams (only Quarto, with a headless Chrome, draws
them) stay code blocks; shortcodes other than `include`, `pagebreak` and
`var` are not resolved; a `filters:` entry with a Quarto entry point (`at:`)
is rejected.

One defect of the *generated decks* shows in both renderers and is left as it
is: `qm` writes an HTML comment before a deck's front matter, so Pandoc does
not read the front matter, and the deck gets a slide titled
`title: 'Calltaker Polizei'` after its real title slide.
