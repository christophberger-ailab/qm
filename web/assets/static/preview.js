// Quarto Sorter - Markdown preview rendering (plain ES6, no build step)
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
//   - everything else is CommonMark/GFM as the embedded marked library reads
//     it. Shortcodes, citations, and math stay as written.

// divFence matches a Pandoc fenced-div line, codeFence a fenced code
// block delimiter. Both mirror the patterns the bookmaker uses on the Go
// side (internal/bookmaker/markdown.go).
var divFence = /^ {0,3}(:{3,})[ \t]*(.*)$/;
var codeFence = /^ {0,3}(`{3,}|~{3,})(.*)$/;

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
  return '/media/' + rel.split('/').map(encodeURIComponent).join('/') + suffix;
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

// renderPreview fills el with the preview of the Quarto Markdown in text.
// pagePath is the edited page's path relative to the project root, which is
// what the image paths resolve against.
function renderPreview(el, text, pagePath) {
  if (typeof marked === 'undefined') { // asset missing: show the source
    el.textContent = text;
    return;
  }
  registerSpanExtension();
  var page = splitFrontmatter(text);
  var html = '';
  if (page.front.trim() !== '') {
    html += '<pre class="preview-frontmatter">' + escapeHTML(page.front) + '</pre>';
  }
  html += marked.parse(convertDivs(page.body));
  el.innerHTML = html;
  // Order matters: sanitize drops the sources that must never be fetched,
  // and only what survives is worth pointing at /media.
  sanitize(el);
  resolveMedia(el, pagePath);
}
