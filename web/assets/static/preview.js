// Quarto Manager - Markdown preview rendering (plain ES6, no build step)
//
// The preview is produced in the browser from the text in the editor, so it
// follows typing without a server round trip. It approximates Quarto's
// output; it is not a Quarto render:
//
//   - YAML frontmatter is shown verbatim in a small header block,
//   - Pandoc fenced divs (`::: {.callout-note}`) become real <div>s so their
//     content is laid out instead of printed as literal colons,
//   - Pandoc bracketed spans (`[text]{.class}`) become real <span>s the
//     same way,
//   - image sources are pointed at the server's /media route so that the
//     page's images show up (see mediaURL),
//   - an image standing alone in a paragraph becomes a figure with its alt
//     text as the caption below it, the way Pandoc's implicit figures --
//     which Quarto builds on -- come out (see captionFigures),
//   - a page written for one target group -- an `_FW` or `_POL` suffix on
//     its name or on a folder above it -- is shown inside that group's
//     Quarto div, the way the flattener wraps it (see targetGroupOf),
//   - everything else is CommonMark/GFM as the embedded marked library reads
//     it. Shortcodes, citations, and math stay as written.

// divFence matches a Pandoc fenced-div line, codeFence a fenced code
// block delimiter. Both mirror the patterns the bookmaker uses on the Go
// side (internal/bookmaker/markdown.go).
var divFence = /^ {0,3}(:{3,})[ \t]*(.*)$/;
var codeFence = /^ {0,3}(`{3,}|~{3,})(.*)$/;

// atxHeading matches a CommonMark ATX heading with its text; headingAttrs
// matches the heading's own trailing Pandoc attribute block, e.g.
// `# Schulungen {.unnumbered .unlisted}`. The `{...}` is only the
// heading's own when it is not preceded by `]`, which would make it the
// attributes of a bracketed span instead (`# [Spickzettel]{.pol}`) -- the
// same rule the bookmaker applies on the Go side.
var atxHeading = /^( {0,3}#{1,6})([ \t]+.*)$/;
var headingAttrs = /(^|[^\]])\{[^{}]*\}[ \t]*$/;

// unsafeTags are dropped from the rendered preview: a page may contain raw
// HTML, and the preview must display it, not run it.
var unsafeTags = 'script,style,iframe,frame,frameset,object,embed,link,meta,base,form';

function escapeHTML(text) {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

// splitFrontmatter separates a leading YAML block from the page body. A page
// without frontmatter yields an empty front.
function splitFrontmatter(text) {
  var lines = text.split('\n');
  if (lines[0].trim() !== '---') {
    return { front: '', body: text };
  }
  for (var i = 1; i < lines.length; i++) {
    var line = lines[i].trim();
    if (line === '---' || line === '...') {
      return {
        front: lines.slice(1, i).join('\n'),
        body: lines.slice(i + 1).join('\n')
      };
    }
  }
  return { front: '', body: text }; // unterminated: treat it all as body
}

// Target groups
//
// A page meant for one target group carries the group's name as a suffix
// of its file name -- `spickzettel_POL.qmd` -- or sits in a folder that
// does -- `betriebszustaende_FW/seite.qmd`. The flattener wraps such a
// page's content in the group's own Quarto div (`::: pol`, `::: fw`), so
// the preview shows the page inside the very same div: whatever the custom
// stylesheet does to `.quarto.pol` -- the tint, the symbol in front of it
// -- it does to the whole page, exactly as it does to a block the page
// carries itself.
//
// groupSuffix names the groups; it is the pattern the Go side matches them
// by (internal/bookmaker/tree.go), so a group added there is added here.
var groupSuffix = /_(fw|pol)$/i;

// targetGroupOf returns the class of the target group the page at pagePath
// belongs to, or "" for a page that is written for all of them. The
// deepest name decides, as it does for the audience filter: a `_POL` page
// inside an `_FW` folder is a POL page.
function targetGroupOf(pagePath) {
  var segments = (pagePath || '').split('/');
  var group = '';
  segments.forEach(function (segment, i) {
    if (i === segments.length - 1) {
      segment = segment.replace(/\.[^.\/]*$/, ''); // the extension is not part of the name
    }
    var match = groupSuffix.exec(segment);
    if (match) {
      group = match[1].toLowerCase();
    }
  });
  return group;
}

// parseAttrs reads the attribute text of a fenced div or bracketed span --
// both spellings Pandoc accepts are handled: the shorthand `::: slide` /
// `[text]{.slide}` and the explicit `::: {#id .slide key="value"}`. Key/value
// attributes are dropped -- the preview only needs the classes to style by.
// Every match gets the `quarto` class, marking it as one of these
// Quarto-specific constructs regardless of which named class(es) follow.
function parseAttrs(attrs) {
  var classes = ['quarto'];
  var id = '';
  attrs.replace(/^\{|\}$/g, '').split(/\s+/).forEach(function (token) {
    if (token === '' || token.indexOf('=') >= 0) {
      return;
    }
    if (token.charAt(0) === '#') {
      id = token.slice(1);
    } else if (token.charAt(0) === '.') {
      classes.push(token.slice(1));
    } else {
      classes.push(token); // shorthand: the bare word is the class
    }
  });
  return { classes: classes, id: id };
}

// attrsToHTML renders the attribute text of a fenced div or bracketed span
// as the class (and, if present, id) attributes of an HTML tag.
function attrsToHTML(attrs) {
  var parsed = parseAttrs(attrs);
  var out = ' class="' + escapeHTML(parsed.classes.join(' ')) + '"';
  if (parsed.id !== '') {
    out += ' id="' + escapeHTML(parsed.id) + '"';
  }
  return out;
}

// divAttrs turns the attribute text of an opening div fence into HTML
// attributes.
function divAttrs(attrs) {
  return attrsToHTML(attrs);
}

// stripHeadingAttrs removes the Pandoc attribute block from every ATX
// heading, so that `# Schulungen {.unnumbered .unlisted}` previews as
// "Schulungen". The attributes tell Quarto how to render the heading --
// keep it out of the numbering, out of the sidebar -- and are not part of
// what it says, so showing them would only be noise. Headings inside code
// blocks are left alone, being code and not headings.
function stripHeadingAttrs(body) {
  var fence = null; // the open code fence's delimiter, if any

  return body.split('\n').map(function (line) {
    var code = line.match(codeFence);
    if (fence !== null) {
      if (code && code[1].charAt(0) === fence.charAt(0) &&
        code[1].length >= fence.length && code[2].trim() === '') {
        fence = null;
      }
      return line;
    }
    if (code) {
      fence = code[1];
      return line;
    }
    var head = line.match(atxHeading);
    if (!head) {
      return line;
    }
    // The closing "#" run goes first: an attribute block sits inside it
    // (`## Title {.foo} ##`), so it has to be out of the way before the
    // block can be recognized as trailing.
    var text = head[2].replace(/[ \t]+#+[ \t]*$/, '');
    return head[1] + text.replace(headingAttrs, '$1').replace(/[ \t]+$/, '');
  }).join('\n');
}

// convertDivs rewrites fenced divs into HTML block tags, surrounded by blank
// lines so that marked reads them as HTML blocks and keeps parsing the
// Markdown between them. Fences inside code blocks are left alone.
function convertDivs(body) {
  var out = [];
  var fence = null; // the open code fence's delimiter, if any
  var depth = 0;    // number of open divs

  body.split('\n').forEach(function (line) {
    var code = line.match(codeFence);
    if (fence !== null) {
      out.push(line);
      if (code && code[1].charAt(0) === fence.charAt(0) &&
        code[1].length >= fence.length && code[2].trim() === '') {
        fence = null;
      }
      return;
    }
    if (code) {
      fence = code[1];
      out.push(line);
      return;
    }
    var div = line.match(divFence);
    if (!div) {
      out.push(line);
      return;
    }
    if (div[2].trim() !== '') {
      out.push('', '<div' + divAttrs(div[2].trim()) + '>', '');
      depth++;
    } else if (depth > 0) {
      out.push('', '</div>', '');
      depth--;
    } else {
      out.push(line); // stray closing fence: show it as written
    }
  });

  while (depth-- > 0) {
    out.push('', '</div>', '');
  }
  return out.join('\n');
}

// sanitize strips the parts of the rendered HTML that would execute rather
// than display: script-like elements, event handlers, and javascript: URLs.
function sanitize(root) {
  root.querySelectorAll(unsafeTags).forEach(function (el) {
    el.remove();
  });
  root.querySelectorAll('*').forEach(function (el) {
    Array.prototype.slice.call(el.attributes).forEach(function (attr) {
      var name = attr.name.toLowerCase();
      var url = attr.value.replace(/[\s\u0000-\u001f]/g, '').toLowerCase();
      var isURL = name === 'href' || name === 'src' || name === 'srcset' ||
        name === 'xlink:href';
      if (name.indexOf('on') === 0 || (isURL && url.indexOf('javascript:') === 0)) {
        el.removeAttribute(attr.name);
      }
    });
  });
}

// normalizePath resolves the "." and ".." segments of a slash-separated
// path. A path climbing past the root keeps its leading "..", which marks it
// as leaving the project.
function normalizePath(p) {
  var out = [];
  p.split('/').forEach(function (seg) {
    if (seg === '' || seg === '.') {
      return;
    }
    if (seg === '..' && out.length > 0 && out[out.length - 1] !== '..') {
      out.pop();
      return;
    }
    out.push(seg);
  });
  return out.join('/');
}

// Image freshness
//
// A browser caches an image by its URL, and an image replaced on disk keeps
// the name the page addresses it by -- so the URL stays the same and the
// cached picture is what the preview would go on showing. Every /media URL
// therefore carries a stamp, and a new stamp is a URL the cache has nothing
// for, which is what makes the browser fetch the file again. app.js sets
// it: once when the page loads, and again on every ↻ Reload, so that
// reloading a page reloads its images along with its text.
var mediaVersion = '';

function setMediaVersion(version) {
  mediaVersion = version;
}

// stampVersion appends the current stamp to a /media URL, as a query
// parameter and ahead of the fragment: the fragment is not part of what is
// fetched, so a stamp behind it would be no stamp at all.
function stampVersion(url, suffix) {
  if (mediaVersion === '') {
    return url + suffix;
  }
  var fragment = '';
  var mark = suffix.indexOf('#');
  if (mark >= 0) {
    fragment = suffix.slice(mark);
    suffix = suffix.slice(0, mark);
  }
  var sep = suffix === '' ? '?' : '&';
  return url + suffix + sep + 'v=' + encodeURIComponent(mediaVersion) + fragment;
}

// mediaURL turns the source of an image on a page into a URL the server
// serves it from. Pages address their media the way the rendered website
// does -- `/assets/images/x.png`, relative to the project root, which is
// what makes the flattened book render -- so a leading slash means the
// root, and anything else is relative to the folder the page sits in.
// External and inline sources are left alone, and so is anything that
// climbs out of the project: the server would refuse it anyway.
function mediaURL(src, baseDir) {
  if (src === '' || /^[a-z][a-z0-9+.-]*:/i.test(src) || src.slice(0, 2) === '//') {
    return src;
  }
  var rel = src.charAt(0) === '/' ? src.slice(1) : baseDir + '/' + src;

  // A query or fragment is not part of the path and must not be escaped
  // along with it.
  var suffix = '';
  var mark = rel.search(/[?#]/);
  if (mark >= 0) {
    suffix = rel.slice(mark);
    rel = rel.slice(0, mark);
  }

  rel = normalizePath(rel);
  if (rel === '' || rel === '..' || rel.slice(0, 3) === '../') {
    return src;
  }
  return stampVersion('/media/' + rel.split('/').map(encodeURIComponent).join('/'), suffix);
}

// resolveMedia points the images of the rendered page at /media, the only
// route that reaches a file inside the project. pagePath is the edited
// page's path relative to the project root; relative image paths resolve
// against the folder it sits in.
function resolveMedia(root, pagePath) {
  var cut = (pagePath || '').lastIndexOf('/');
  var baseDir = cut < 0 ? '' : pagePath.slice(0, cut);
  root.querySelectorAll('img[src]').forEach(function (img) {
    img.setAttribute('src', mediaURL(img.getAttribute('src'), baseDir));
  });
}

// captionFigures turns an image that stands alone in a paragraph -- a
// blank line before and after it -- into a figure whose caption is the
// image's alt text, shown below the image. This is Pandoc's implicit
// figure, the form Quarto renders such an image in, so the preview shows
// the caption the rendered page will carry.
//
// A paragraph holding anything besides the image is left alone: the image
// is part of the running text there, and Pandoc gives it no caption
// either. So is an image without alt text, which has no caption to show.
function captionFigures(root) {
  root.querySelectorAll('p > img:only-child').forEach(function (img) {
    var para = img.parentNode;
    if (para.textContent.trim() !== '') {
      return; // text alongside the image: not a figure
    }
    var caption = (img.getAttribute('alt') || '').trim();
    if (caption === '') {
      return;
    }
    var figure = document.createElement('figure');
    var legend = document.createElement('figcaption');
    // textContent, not innerHTML: the alt text is the page's, and the
    // preview displays it rather than letting it become markup.
    legend.textContent = caption;
    figure.appendChild(img);
    figure.appendChild(legend);
    para.parentNode.replaceChild(figure, para);
  });
}

// matchSpan looks for a Pandoc bracketed span (`[text]{.class}`) at the
// start of src. Brackets are matched by depth rather than a regexp so that
// spans wrapping their own bracketed content -- an image, say, as in
// `[![alt](img.png)]{.pol}` -- are still recognized. Returns null when src
// does not start with a complete `[...]{...}`.
function matchSpan(src) {
  if (src.charAt(0) !== '[') {
    return null;
  }
  var depth = 0;
  var i = 0;
  for (; i < src.length; i++) {
    var ch = src.charAt(i);
    if (ch === '\\') {
      i++; // an escaped character never opens/closes a bracket
      continue;
    }
    if (ch === '\n') {
      return null; // spans do not cross line breaks
    }
    if (ch === '[') {
      depth++;
    } else if (ch === ']') {
      depth--;
      if (depth === 0) {
        break;
      }
    }
  }
  if (depth !== 0 || i >= src.length) {
    return null; // no closing bracket, or brackets never balance
  }
  var attrs = /^\{([^}\n]*)\}/.exec(src.slice(i + 1));
  if (!attrs) {
    return null; // not immediately followed by an attribute block
  }
  return {
    text: src.slice(1, i),
    attrs: attrs[1],
    raw: src.slice(0, i + 1 + attrs[0].length)
  };
}

var spanExtensionRegistered = false;

// registerSpanExtension teaches marked about bracketed spans. It runs once,
// the first time marked is available, since marked.use() adds the
// extension for good.
function registerSpanExtension() {
  if (spanExtensionRegistered || typeof marked === 'undefined') {
    return;
  }
  spanExtensionRegistered = true;
  marked.use({
    extensions: [{
      name: 'quartoSpan',
      level: 'inline',
      start: function (src) {
        var match = src.match(/\[/);
        return match ? match.index : -1;
      },
      tokenizer: function (src) {
        var span = matchSpan(src);
        if (!span) {
          return undefined;
        }
        return {
          type: 'quartoSpan',
          raw: span.raw,
          attrs: span.attrs,
          tokens: this.lexer.inlineTokens(span.text)
        };
      },
      renderer: function (token) {
        return '<span' + attrsToHTML(token.attrs) + '>' +
          this.parser.parseInline(token.tokens) + '</span>';
      }
    }]
  });
}

// matchImage looks for a Markdown image at the start of src and, if one is
// found, for a Pandoc attribute block (`{width=50%}`) immediately after it.
// Only the attributed form is of interest here -- a plain `![alt](src)` is
// left to marked's own image tokenizer -- so callers get null both when src
// does not start with an image and when it does but no attributes follow.
var imageRE = /^!\[((?:\\.|[^\[\]\\])*)\]\(\s*(<(?:\\.|[^<>\\])*>|(?:\\.|[^()\s\\])*)(?:\s+"((?:\\.|[^"\\])*)"|\s+'((?:\\.|[^'\\])*)')?\s*\)/;

function matchImage(src) {
  var m = imageRE.exec(src);
  if (!m) {
    return null;
  }
  var attrs = /^\{([^}\n]*)\}/.exec(src.slice(m[0].length));
  if (!attrs) {
    return null; // not immediately followed by an attribute block
  }
  var href = m[2];
  if (href.charAt(0) === '<' && href.charAt(href.length - 1) === '>') {
    href = href.slice(1, -1);
  }
  return {
    raw: m[0] + attrs[0],
    alt: m[1],
    href: href,
    title: m[3] !== undefined ? m[3] : m[4],
    attrs: attrs[1]
  };
}

// imageAttrsToHTML turns the attribute text following an image
// (`{width=50%}`) into HTML attributes. Unlike attrsToHTML (used for divs
// and spans), key/value pairs are not dropped: since image attributes
// almost always name a CSS property -- width and height above all -- each
// becomes part of a style attribute instead, and an explicit `style` value
// is folded in the same way.
function imageAttrsToHTML(attrs) {
  var classes = [];
  var id = '';
  var styles = [];
  attrs.trim().split(/\s+/).forEach(function (token) {
    if (token === '') {
      return;
    }
    var eq = token.indexOf('=');
    if (eq >= 0) {
      var key = token.slice(0, eq);
      var value = token.slice(eq + 1).replace(/^["']|["']$/g, '');
      styles.push(key === 'style' ? value.replace(/;\s*$/, '') : key + ': ' + value);
      return;
    }
    if (token.charAt(0) === '#') {
      id = token.slice(1);
    } else if (token.charAt(0) === '.') {
      classes.push(token.slice(1));
    } else {
      classes.push(token);
    }
  });
  var out = '';
  if (classes.length > 0) {
    out += ' class="' + escapeHTML(classes.join(' ')) + '"';
  }
  if (id !== '') {
    out += ' id="' + escapeHTML(id) + '"';
  }
  if (styles.length > 0) {
    out += ' style="' + escapeHTML(styles.join('; ')) + '"';
  }
  return out;
}

var imageExtensionRegistered = false;

// registerImageExtension teaches marked about the attribute block that may
// follow an image. It runs once, the first time marked is available, since
// marked.use() adds the extension for good.
function registerImageExtension() {
  if (imageExtensionRegistered || typeof marked === 'undefined') {
    return;
  }
  imageExtensionRegistered = true;
  marked.use({
    extensions: [{
      name: 'quartoImage',
      level: 'inline',
      start: function (src) {
        var match = src.match(/!\[/);
        return match ? match.index : -1;
      },
      tokenizer: function (src) {
        var img = matchImage(src);
        if (!img) {
          return undefined; // no attributes: let marked's own tokenizer render it
        }
        return {
          type: 'quartoImage',
          raw: img.raw,
          href: img.href,
          title: img.title,
          alt: img.alt,
          attrs: img.attrs
        };
      },
      renderer: function (token) {
        var out = '<img src="' + escapeHTML(token.href) + '" alt="' + escapeHTML(token.alt) + '"';
        if (token.title) {
          out += ' title="' + escapeHTML(token.title) + '"';
        }
        return out + imageAttrsToHTML(token.attrs) + '>';
      }
    }]
  });
}

// renderPreview fills el with the preview of the Quarto Markdown in text.
// pagePath is the edited page's path relative to the project root: it is
// what the image paths resolve against, and what says which target group
// the page belongs to.
function renderPreview(el, text, pagePath) {
  if (typeof marked === 'undefined') { // asset missing: show the source
    el.textContent = text;
    return;
  }
  registerSpanExtension();
  registerImageExtension();
  var page = splitFrontmatter(text);
  var html = '';
  if (page.front.trim() !== '') {
    html += '<pre class="preview-frontmatter">' + escapeHTML(page.front) + '</pre>';
  }
  var body = marked.parse(convertDivs(stripHeadingAttrs(page.body)));
  var group = targetGroupOf(pagePath);
  if (group !== '') {
    // The frontmatter stays outside the wrapper: it directs the render, it
    // is not content the group's div would hold.
    body = '<div class="quarto ' + escapeHTML(group) + '">' + body + '</div>';
  }
  el.innerHTML = html + body;
  // Order matters: sanitize drops the sources that must never be fetched,
  // and only what survives is worth pointing at /media or captioning.
  sanitize(el);
  resolveMedia(el, pagePath);
  captionFigures(el);
}
