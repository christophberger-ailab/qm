# The Markdown preview

The preview shows the page you are editing, updated as you type. Show or hide it with the `Preview` button (see [the panes](panes.md)).

The preview is an approximation. It is drawn in the browser from your Markdown and is not a Quarto render, so use [Rendering](render.md) for the real result.

## What it shows

- Quarto fenced divs (`::: name`) and bracketed spans (`[text]{.name}`) get their class, so your custom CSS can style them.
- A heading's attribute block, such as `{.unnumbered}`, is dropped, because it directs the render and is not part of what the heading says. Headings inside code blocks are left alone.
- A page written for one target group is previewed inside that group's div. This applies when the file name, or a folder above it, ends in `_FW` or `_POL` (the deepest name wins). The stylesheet then tints and marks the whole page, as it does a `::: fw` or `::: pol` block. The frontmatter stays outside the div.

## Images

Images are loaded from your project.

- A path starting with `/`, like `/assets/images/x.png`, is relative to the project root.
- Any other path is relative to the folder of the page.
- Only image files inside the open project are served.
- An image alone in its paragraph (blank line before and after) shows its alt text as a caption underneath. An image inside running text, or without alt text, gets no caption.
- Hover over an image to see its path as written in the page. If the image has a Markdown title, the title is shown first.
- Double-click an image to open it in your [image editor](external-editor.md). When you save in that program, the preview picks up the new version within a couple of seconds.
- `↻ Reload` shows images as they are on disk, even if you replaced one under the same name.

## Scrolling

The editor and the preview scroll together: moving either one brings the other to the same part of the page. The preview also stays with the editor while you type.

## Previous and Next

Below the preview, `◀ Previous` and `Next ▶` open the page before or after the current one, in the order of the [page tree](tree.md). Each button's tooltip names the page. At either end of the project the button is disabled.

## Custom CSS

If you have more than one custom stylesheet, a dropdown below the preview switches between them. The choice is remembered and the preview reloads to apply it. See [Config pages and the settings file](settings.md) for how to write the stylesheets.

Back to [Contents](index.md).
