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
//   - a passage between a `<!-- << comment -->` and a `<!-- >> -->` is
//     highlighted, with the comment as its tooltip (see markComments),
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
// without frontmatter yields an empty front. offset is the number of lines
// in front of the body, which is what turns a line of the body back into a
// line of the page.
function splitFrontmatter(text) {
  var lines = text.split('\n');
  if (lines[0].trim() !== '---') {
    return { front: '', body: text, offset: 0 };
  }
  for (var i = 1; i < lines.length; i++) {
    var line = lines[i].trim();
    if (line === '---' || line === '...') {
      return {
        front: lines.slice(1, i).join('\n'),
        body: lines.slice(i + 1).join('\n'),
        offset: i + 1
      };
    }
  }
  return { front: '', body: text, offset: 0 }; // unterminated: treat it all as body
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
// `[text]{.slide}` and the explicit `::: {#id .slide key="value"}`.
// Key/value attributes are collected in keys, a value in quotes keeping its
// spaces (`title="Mind the gap"`); the preview styles by the classes and
// reads only the few keys a callout takes (see calloutAttrs).
// Every match gets the `quarto` class, marking it as one of these
// Quarto-specific constructs regardless of which named class(es) follow.
var attrToken = /([^\s=]+)=(?:"([^"]*)"|'([^']*)'|(\S*))|(\S+)/g;

function parseAttrs(attrs) {
  var classes = ['quarto'];
  var id = '';
  var keys = {};
  var text = attrs.replace(/^\{|\}$/g, '');
  var m;
  attrToken.lastIndex = 0;
  while ((m = attrToken.exec(text)) !== null) {
    if (m[1] !== undefined) {
      keys[m[1]] = m[2] !== undefined ? m[2] : m[3] !== undefined ? m[3] : m[4];
      continue;
    }
    var token = m[5];
    if (token.charAt(0) === '#') {
      id = token.slice(1);
    } else if (token.charAt(0) === '.') {
      classes.push(token.slice(1));
    } else {
      classes.push(token); // shorthand: the bare word is the class
    }
  }
  return { classes: classes, id: id, keys: keys };
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
// attributes. A callout also carries the keys that shape it, as data
// attributes for buildCallouts to read once the page is rendered.
function divAttrs(attrs) {
  var out = attrsToHTML(attrs);
  var keys = parseAttrs(attrs).keys;
  calloutKeys.forEach(function (key) {
    if (keys[key] !== undefined) {
      out += ' data-callout-' + key + '="' + escapeHTML(keys[key]) + '"';
    }
  });
  return out;
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
//
// Each fence becomes three lines, so the result no longer lines up with the
// body. lineMap, if given, is filled with the body line every line of the
// result came from; that is what lets the preview say which line of the
// page each of its blocks stands for (see markSourceLines).
function convertDivs(body, lineMap) {
  var out = [];
  var fence = null; // the open code fence's delimiter, if any
  var depth = 0;    // number of open divs

  // emit adds lines to the result, each noted as coming from body line n.
  function emit(n, lines) {
    lines.forEach(function (line) {
      out.push(line);
      if (lineMap) {
        lineMap.push(n);
      }
    });
  }

  var lines = body.split('\n');
  lines.forEach(function (line, n) {
    var code = line.match(codeFence);
    if (fence !== null) {
      emit(n, [line]);
      if (code && code[1].charAt(0) === fence.charAt(0) &&
        code[1].length >= fence.length && code[2].trim() === '') {
        fence = null;
      }
      return;
    }
    if (code) {
      fence = code[1];
      emit(n, [line]);
      return;
    }
    var div = line.match(divFence);
    if (!div) {
      emit(n, [line]);
      return;
    }
    if (div[2].trim() !== '') {
      emit(n, ['', '<div' + divAttrs(div[2].trim()) + '>', '']);
      depth++;
    } else if (depth > 0) {
      emit(n, ['', '</div>', '']);
      depth--;
    } else {
      emit(n, [line]); // stray closing fence: show it as written
    }
  });

  while (depth-- > 0) {
    emit(lines.length - 1, ['', '</div>', '']);
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
  var rel = mediaPath(src, baseDir);
  if (rel === '') {
    return src;
  }
  var suffix = '';
  var mark = src.search(/[?#]/);
  if (mark >= 0) {
    suffix = src.slice(mark);
  }
  return stampVersion('/media/' + rel.split('/').map(encodeURIComponent).join('/'), suffix);
}

// mediaPath is the path, relative to the project root, of the file an
// image source on a page names -- the path /media serves it by, and the
// one the image editor is asked to open. It is empty for a source that
// names no file of the project: an external or inline one, or one that
// climbs out of it.
function mediaPath(src, baseDir) {
  if (src === '' || /^[a-z][a-z0-9+.-]*:/i.test(src) || src.slice(0, 2) === '//') {
    return '';
  }
  var rel = src.charAt(0) === '/' ? src.slice(1) : baseDir + '/' + src;

  // A query or fragment is not part of the path.
  var mark = rel.search(/[?#]/);
  if (mark >= 0) {
    rel = rel.slice(0, mark);
  }

  rel = normalizePath(rel);
  if (rel === '' || rel === '..' || rel.slice(0, 3) === '../') {
    return '';
  }
  return rel;
}

// resolveMedia points the images of the rendered page at /media, the only
// route that reaches a file inside the project. pagePath is the edited
// page's path relative to the project root; relative image paths resolve
// against the folder it sits in.
//
// Each image also gets the path written in the page as its tooltip, so
// hovering it tells which file it is. An image that has a Markdown title
// keeps it, with the path on a line below. An image of the project carries
// its project path in data-media-path, which is what a double-click opens
// in the image editor (see app.js), and says so in its tooltip.
function resolveMedia(root, pagePath) {
  var cut = (pagePath || '').lastIndexOf('/');
  var baseDir = cut < 0 ? '' : pagePath.slice(0, cut);
  root.querySelectorAll('img[src]').forEach(function (img) {
    var src = img.getAttribute('src');
    var title = img.getAttribute('title');
    var rel = mediaPath(src, baseDir);
    var tip = title ? title + '\n' + src : src;
    if (rel !== '') {
      img.setAttribute('data-media-path', rel);
      tip += '\nDouble-click to open it in the image editor';
    } else {
      img.removeAttribute('data-media-path');
    }
    img.setAttribute('title', tip);
    img.setAttribute('src', mediaURL(src, baseDir));
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

// Comments
//
// A comment on a passage is a pair of HTML comments around it:
//
//   <!-- << The comment itself -->the passage<!-- >> -->
//
// The `<<` comment opens the passage and carries the comment's text, the
// `>>` comment closes it. Being HTML comments, both are dropped by Quarto,
// so a commented page renders as if it had none. The preview shows the
// passage highlighted, with the comment as its tooltip (see markComments),
// and the editor writes the pair around a selection (see editor.js).
//
// A Pandoc attribute block may follow either marker, ahead of the text:
//
//   <!-- << {#c1 author="ana" date="2026-10-30"} The comment -->...<!-- >> {#c1} -->
//
// None is written yet. It is room for what comments by more than one
// person will need -- who wrote one and when, and an id that pairs the two
// ends when passages commented by different people overlap -- so comments
// written today stay valid once those are added. A closing marker with an
// id closes the open passage with that id; one without closes the passage
// opened last.

// parseCommentMarker reads the text of an HTML comment -- what lies
// between `<!--` and `-->` -- and returns the comment marker it is, or null
// when it is an ordinary comment. textAt is where the comment's own text
// starts in data, after the marker, its attributes, and one space.
function parseCommentMarker(data) {
  var m = /^\s*(<<|>>)[ \t]?/.exec(data);
  if (!m) {
    return null;
  }
  var at = m[0].length;
  var parsed = { id: '', keys: {} };
  var attrs = /^[ \t]*\{([^{}]*)\}[ \t]?/.exec(data.slice(at));
  if (attrs) {
    parsed = parseAttrs(attrs[1]);
    at += attrs[0].length;
  }
  return {
    kind: m[1] === '<<' ? 'start' : 'end',
    id: parsed.id,
    author: parsed.keys.author || '',
    date: parsed.keys.date || '',
    text: data.slice(at).trim(),
    textAt: at
  };
}

// commentStart and commentEnd spell the two markers out, the start as the
// text before and after the comment's own, so the editor can put the
// cursor between them. meta may name an id, an author, and a date; left
// empty, the markers carry no attribute block at all.
function commentAttrs(meta) {
  var attrs = [];
  if (meta && meta.id) {
    attrs.push('#' + meta.id);
  }
  ['author', 'date'].forEach(function (key) {
    if (meta && meta[key]) {
      attrs.push(key + '="' + String(meta[key]).replace(/"/g, "'") + '"');
    }
  });
  return attrs.length ? '{' + attrs.join(' ') + '} ' : '';
}

function commentStart(meta) {
  return { before: '<!-- << ' + commentAttrs(meta), after: ' -->' };
}

function commentEnd(meta) {
  return '<!-- >> ' + commentAttrs(meta) + '-->';
}

// maskCode blanks out what Markdown reads as code -- fenced code blocks and
// inline code spans -- and the first frontLines lines, keeping every
// offset where it was, so a comment written inside code is not taken for
// one. Only the text left over becomes HTML comments in the preview.
function maskCode(text, frontLines) {
  var fence = null;
  function blank(s) {
    return s.replace(/[^\n]/g, ' ');
  }
  return text.split('\n').map(function (line, n) {
    if (n < frontLines) {
      return blank(line);
    }
    var code = line.match(codeFence);
    if (fence !== null) {
      if (code && code[1].charAt(0) === fence.charAt(0) &&
        code[1].length >= fence.length && code[2].trim() === '') {
        fence = null;
      }
      return blank(line);
    }
    if (code) {
      fence = code[1];
      return blank(line);
    }
    return line.replace(/(`+)[\s\S]*?\1/g, blank);
  }).join('\n');
}

// findCommentMarkers lists the comment markers of a page in the order they
// stand, each with where it sits in text: from and to around the whole
// HTML comment, textFrom where the comment's own text starts. It is the
// order the preview meets them in, which is what ties a highlight there to
// its marker here.
function findCommentMarkers(text) {
  var masked = maskCode(text, splitFrontmatter(text).offset);
  var found = [];
  var re = /<!--([\s\S]*?)-->/g;
  var m;
  while ((m = re.exec(masked)) !== null) {
    var marker = parseCommentMarker(text.slice(m.index + 4, m.index + m[0].length - 3));
    if (marker) {
      marker.from = m.index;
      marker.to = m.index + m[0].length;
      marker.textFrom = m.index + 4 + marker.textAt;
      found.push(marker);
    }
  }
  return found;
}

// pairCommentMarkers matches each closing marker with the start it closes,
// setting start.end and end.start: the one with the same id, or, without
// an id, the passage opened last. A marker without a partner is left
// unpaired.
function pairCommentMarkers(markers) {
  var open = [];
  markers.forEach(function (marker) {
    if (marker.kind === 'start') {
      open.push(marker);
      return;
    }
    for (var i = open.length - 1; i >= 0; i--) {
      if (!marker.id || open[i].id === marker.id) {
        open[i].end = marker;
        marker.start = open[i];
        open.splice(i, 1);
        return;
      }
    }
  });
}

// unblockComments keeps a comment marker at the start of a line from
// turning the line into an HTML block. CommonMark reads a line that opens
// with `<!--` as raw HTML, the rest of it included, so the passage after a
// starting marker -- `<!-- << Check -->**Total** cost` -- would lose its
// Markdown and its paragraph. A zero-width space in front makes the line
// an ordinary paragraph line again, holding the marker as inline HTML. A
// marker alone on its line is left as it is: there is no text to lose.
function unblockComments(src) {
  var masked = maskCode(src, 0);
  var re = /(^|\n) {0,3}<!--([\s\S]*?)-->(?=[ \t]*\S)/g;
  var out = '';
  var last = 0;
  var m;
  while ((m = re.exec(masked)) !== null) {
    if (!parseCommentMarker(m[2])) {
      continue;
    }
    var at = m.index + m[1].length;
    out += src.slice(last, at) + '​';
    last = at;
  }
  return out + src.slice(last);
}

// commentTooltip is what hovering a commented passage shows: the comment,
// who wrote it and when if the marker says, and what a click does.
function commentTooltip(marker) {
  var tip = marker.text || '(empty comment)';
  if (marker.author || marker.date) {
    tip += '\n— ' + [marker.author, marker.date].filter(Boolean).join(', ');
  }
  return tip + '\nClick to edit the comment in the editor';
}

// markComments highlights every commented passage in root. The markers
// survive marked as comment nodes, so the passage is the text between a
// start and the end it pairs with, however many blocks it spans; each of
// its text nodes is wrapped in a span carrying the comment as its tooltip,
// and the number of the start marker among all start markers on the page,
// which is how a click finds the marker in the editor (see focusComment).
// Nested passages get nested spans, the inner one answering a hover. The
// comment nodes are left in place.
function markComments(root) {
  var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_COMMENT);
  var nodes = [];
  var markers = [];
  var starts = 0;
  while (walker.nextNode()) {
    var node = walker.currentNode;
    if (node.nodeType === Node.COMMENT_NODE) {
      var marker = parseCommentMarker(node.data);
      if (marker) {
        if (marker.kind === 'start') {
          marker.index = starts++;
        }
        markers.push(marker);
        nodes.push(marker);
      }
    } else if (node.data.trim() !== '') {
      nodes.push(node);
    }
  }
  if (!markers.length) {
    return;
  }
  pairCommentMarkers(markers);

  var active = [];
  nodes.forEach(function (item) {
    if (item.kind === 'start') {
      if (item.end) {
        active.push(item);
      }
      return;
    }
    if (item.kind === 'end') {
      var i = active.indexOf(item.start);
      if (i >= 0) {
        active.splice(i, 1);
      }
      return;
    }
    if (!active.length) {
      return;
    }
    var inner = item;
    active.slice().reverse().forEach(function (marker) {
      var span = document.createElement('span');
      span.className = 'qm-comment';
      span.setAttribute('data-comment', String(marker.index));
      span.title = commentTooltip(marker);
      inner.parentNode.replaceChild(span, inner);
      span.appendChild(inner);
      inner = span;
    });
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

// Callouts
//
// A Quarto callout (`::: {.callout-note}`) renders as a box in its type's
// color: a header on the type's shaded color with an icon and a title, and
// the body below it. buildCallouts gives the rendered div that shape, after
// the Markdown inside it has been rendered like any other. The colors are
// the ones Quarto gives each type, the same the native PDF draws with (see
// internal/docrender); app.css holds them.
//
// calloutKeys are the attributes of a callout's fence the preview reads:
//
//   - title, the header's text; without it a level-one-to-six heading
//     opening the callout becomes the title, and failing that the type's
//     name ("Note") does, as in Quarto,
//   - appearance: "default" (the shaded header), "simple" (no shading, the
//     icon beside the body when there is no title), or "minimal" (no
//     shading and no icon),
//   - icon="false", which leaves the icon out,
//   - collapse: "true" or "false" make the callout fold up under its header
//     at a click, folded or open to begin with.
var calloutKeys = ['title', 'appearance', 'icon', 'collapse'];

var calloutTypes = {
  note: 'Note',
  tip: 'Tip',
  warning: 'Warning',
  important: 'Important',
  caution: 'Caution'
};

// calloutType returns the type of the callout el, or "" when el is no
// callout.
function calloutType(el) {
  for (var type in calloutTypes) {
    if (el.classList.contains('callout-' + type)) {
      return type;
    }
  }
  return '';
}

// buildCallouts lays out every callout in root as Quarto does: the div
// gets a header and a body, and the classes the stylesheet draws it by.
// The innermost callouts go first, so an outer one moves finished boxes.
function buildCallouts(root) {
  var divs = Array.prototype.slice.call(root.querySelectorAll('div.quarto')).reverse();
  divs.forEach(function (div) {
    var type = calloutType(div);
    if (type === '') {
      return;
    }
    var appearance = div.getAttribute('data-callout-appearance') || 'default';
    if (appearance !== 'simple' && appearance !== 'minimal') {
      appearance = 'default';
    }
    var icon = appearance !== 'minimal' && div.getAttribute('data-callout-icon') !== 'false';
    var collapse = div.getAttribute('data-callout-collapse');
    var foldable = collapse === 'true' || collapse === 'false';

    // The title: the fence's, else an opening heading's, else the type's
    // name -- the last only for the default appearance, which always has
    // a header.
    var title = document.createElement('div');
    title.className = 'callout-title';
    var given = div.getAttribute('data-callout-title');
    var first = div.firstElementChild;
    if (given !== null) {
      // textContent, not innerHTML: the title is the page's text.
      title.textContent = given;
    } else if (first && /^H[1-6]$/.test(first.tagName)) {
      if (first.hasAttribute('data-line')) {
        title.setAttribute('data-line', first.getAttribute('data-line'));
      }
      while (first.firstChild) {
        title.appendChild(first.firstChild);
      }
      first.remove();
    } else if (appearance === 'default' || foldable) {
      title.textContent = calloutTypes[type];
    } else {
      title = null;
    }

    var body = document.createElement('div');
    body.className = 'callout-body';
    while (div.firstChild) {
      body.appendChild(div.firstChild);
    }

    var header = null;
    if (title !== null) {
      header = document.createElement(foldable ? 'summary' : 'div');
      header.className = 'callout-header';
      if (icon) {
        header.appendChild(calloutIcon());
      }
      header.appendChild(title);
    } else if (icon) {
      div.appendChild(calloutIcon()); // simple, untitled: the icon beside the body
    }

    if (foldable) {
      var fold = document.createElement('details');
      fold.className = 'callout-fold';
      fold.open = collapse !== 'true';
      fold.appendChild(header);
      fold.appendChild(body);
      div.appendChild(fold);
    } else {
      if (header !== null) {
        div.appendChild(header);
      }
      div.appendChild(body);
    }

    div.classList.add('callout', 'callout-style-' + appearance);
    div.classList.toggle('callout-titled', title !== null);
  });
}

function calloutIcon() {
  var icon = document.createElement('span');
  icon.className = 'callout-icon';
  return icon;
}

// foldStates and restoreFolds carry the callouts a user opened or folded
// across a render: the preview renders anew as the user types, and a
// callout that snapped back to its fence's state at every keystroke could
// not be kept open while its text is edited. Callouts are told apart by
// their order on the page.
function foldStates(root) {
  return Array.prototype.map.call(root.querySelectorAll('details.callout-fold'), function (fold) {
    return fold.open;
  });
}

function restoreFolds(root, states) {
  root.querySelectorAll('details.callout-fold').forEach(function (fold, i) {
    if (i < states.length) {
      fold.open = states[i];
    }
  });
}

// Source lines
//
// The editor and the preview scroll together, and for that the preview has
// to know which line of the page each of its blocks was made from. Every
// top-level block marked reads is rendered on its own, behind a comment
// naming the page line it starts on; once the HTML is in place, the comment
// hands its line to the element after it as data-line, and goes. The
// blocks inside a fenced div are top-level blocks too -- convertDivs turns
// the fences into HTML blocks of their own -- so a callout's paragraphs are
// marked like any other.
var lineMark = 'qm-line:';

// renderBlocks renders src, the converted body, block by block. lineMap
// takes a line of src back to a line of the body (see convertDivs), and
// offset a line of the body to a line of the page.
function renderBlocks(src, lineMap, offset) {
  var tokens = marked.lexer(unblockComments(src)); // keeps the lines where they are
  var html = '';
  var line = 0;
  tokens.forEach(function (token) {
    if (token.type !== 'space') {
      var at = line < lineMap.length ? lineMap[line] : lineMap.length;
      html += '<!--' + lineMark + (offset + at) + '-->';
    }
    var block = [token];
    block.links = tokens.links; // reference links resolve against the whole page
    html += marked.parser(block);
    line += (token.raw.match(/\n/g) || []).length;
  });
  return html;
}

// markSourceLines turns the comments renderBlocks left into data-line
// attributes on the elements they precede. A comment with no element after
// it -- the one in front of a closing </div> -- marks nothing.
function markSourceLines(root) {
  var walker = document.createTreeWalker(root, NodeFilter.SHOW_COMMENT);
  var marks = [];
  while (walker.nextNode()) {
    if (walker.currentNode.data.indexOf(lineMark) === 0) {
      marks.push(walker.currentNode);
    }
  }
  marks.forEach(function (mark) {
    var next = mark.nextSibling;
    while (next && next.nodeType !== Node.ELEMENT_NODE) {
      next = next.nextSibling;
    }
    if (next && !next.hasAttribute('data-line')) {
      next.setAttribute('data-line', mark.data.slice(lineMark.length));
    }
    mark.remove();
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
    html += '<pre class="preview-frontmatter" data-line="0">' + escapeHTML(page.front) + '</pre>';
  }
  var lineMap = [];
  var body = renderBlocks(convertDivs(stripHeadingAttrs(page.body), lineMap), lineMap, page.offset);
  var group = targetGroupOf(pagePath);
  if (group !== '') {
    // The frontmatter stays outside the wrapper: it directs the render, it
    // is not content the group's div would hold.
    body = '<div class="quarto ' + escapeHTML(group) + '">' + body + '</div>';
  }
  var folds = foldStates(el);
  el.innerHTML = html + body;
  // Order matters: sanitize drops the sources that must never be fetched,
  // and only what survives is worth pointing at /media or captioning.
  sanitize(el);
  resolveMedia(el, pagePath);
  captionFigures(el);
  markComments(el); // the line comments are no comment markers, so they may stay
  markSourceLines(el); // after the figures, so a figure carries its line
  // After the source lines, so that the line marks are attributes that
  // move along with the blocks into a callout's body.
  buildCallouts(el);
  restoreFolds(el, folds);
}
