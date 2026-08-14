// Quarto Sorter - Markdown preview rendering (plain ES6, no build step)
//
// The preview is produced in the browser from the text in the editor, so it
// follows typing without a server round trip. It approximates Quarto's
// output; it is not a Quarto render:
//
//   - YAML frontmatter is shown verbatim in a small header block,
//   - Pandoc fenced divs (`::: {.callout-note}`) become real <div>s so their
//     content is laid out instead of printed as literal colons,
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

// divAttrs turns the attribute text of an opening div fence into HTML
// attributes. Both spellings Pandoc accepts are handled: the shorthand
// `::: slide` and the explicit `::: {#id .slide key="value"}`. Key/value
// attributes are dropped -- the preview only needs the classes to style by.
function divAttrs(attrs) {
  var classes = ['qdiv'];
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
  var out = ' class="' + escapeHTML(classes.join(' ')) + '"';
  if (id !== '') {
    out += ' id="' + escapeHTML(id) + '"';
  }
  return out;
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

// renderPreview fills el with the preview of the Quarto Markdown in text.
function renderPreview(el, text) {
  if (typeof marked === 'undefined') { // asset missing: show the source
    el.textContent = text;
    return;
  }
  var page = splitFrontmatter(text);
  var html = '';
  if (page.front.trim() !== '') {
    html += '<pre class="preview-frontmatter">' + escapeHTML(page.front) + '</pre>';
  }
  html += marked.parse(convertDivs(page.body));
  el.innerHTML = html;
  sanitize(el);
}
