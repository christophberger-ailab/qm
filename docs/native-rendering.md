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
Instead, `qm render` selects `--to docx` and adds a final Lua filter which writes
the filtered Pandoc AST to a unique build directory. Quarto's existing audience
filter, includes, variables and book assembly run normally once. The final AST
contains some DOCX-specific callout layout fragments: these are accepted only
when they contain known layout elements, while content-bearing unsupported
raw XML reports an error.

`internal/bookrender` owns orchestration; `internal/docrender` owns PDF layout
and DOCX cover insertion. `qm web` shares the same orchestration.

## Cover packaging

The DOCX cover uses a page-relative anchored DrawingML image, zero offsets,
page-sized extents and a separate zero-margin section. Aspect-ratio mismatches
use symmetric source cropping. The original body and final section XML are
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

The integration test exercises a real Quarto book, audience filtering, table,
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
