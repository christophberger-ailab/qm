// Quarto Manager - app behavior (plain ES6, no build step)

// Open field
//
// The field shows the last element of the open project's path: the folder
// name is what tells one project from another, and the whole path only
// makes the field too narrow to read. The full path rides along in
// data-full and is what the submit sends, so the short label still opens
// the right project. Typing or pasting drops the full path, which is what
// lets a pasted path be opened as written.

function openPathInput() {
  return document.getElementById('open-path');
}

// showRecent opens or closes the dropdown of recently opened projects.
function showRecent(on) {
  var menu = document.getElementById('open-recent');
  var toggle = document.getElementById('open-recent-toggle');
  if (menu) {
    menu.hidden = !on;
  }
  if (toggle) {
    toggle.setAttribute('aria-expanded', on ? 'true' : 'false');
  }
}

// pickRecent puts a chosen project into the field -- short label, full
// path, full path as the tooltip -- and opens it. The dropdown itself lists
// the full paths, so the label rides along on each entry.
function pickRecent(path, label) {
  var input = openPathInput();
  if (input) {
    input.value = label;
    input.dataset.full = path;
    input.title = path;
  }
  showRecent(false);
  htmx.ajax('POST', '/open', {
    target: '#main',
    swap: 'innerHTML',
    values: { path: path }
  });
}

document.body.addEventListener('click', function (evt) {
  var toggle = evt.target.closest('#open-recent-toggle');
  if (toggle) {
    var menu = document.getElementById('open-recent');
    showRecent(!!menu && menu.hidden);
    return;
  }
  var entry = evt.target.closest('.path-recent');
  if (entry) {
    pickRecent(entry.dataset.path, entry.dataset.label || entry.dataset.path);
    return;
  }
  // A click anywhere else closes the dropdown, the way a menu closes.
  if (!evt.target.closest('.path-field')) {
    showRecent(false);
  }
});

// A field the user has typed or pasted into no longer stands for the
// project it was showing, so the full path behind it is dropped and the
// text itself is what gets opened.
document.body.addEventListener('input', function (evt) {
  if (evt.target.id === 'open-path') {
    evt.target.dataset.full = '';
    evt.target.title = evt.target.value;
  }
});

var sortableInstances = [];

// Collapsed branches, keyed by each node's data-key. #tree is fully
// re-rendered on every move/create/delete/save, so this module-level state
// is what keeps collapsed branches collapsed across those re-renders. It is
// mirrored into localStorage (see load/saveCollapsed) so the expand/collapse
// state also survives a full page reload and future sessions.
var collapsed = new Set();

var COLLAPSED_KEY = 'collapsedKeys';

// loadCollapsed seeds the in-memory set from localStorage on startup.
function loadCollapsed() {
  try {
    var raw = localStorage.getItem(COLLAPSED_KEY);
    if (raw) {
      JSON.parse(raw).forEach(function (key) {
        collapsed.add(key);
      });
    }
  } catch (e) {
    // Ignore malformed or unavailable storage; start from an empty set.
  }
}

// saveCollapsed persists the current set after every change to it.
function saveCollapsed() {
  try {
    localStorage.setItem(COLLAPSED_KEY, JSON.stringify(Array.from(collapsed)));
  } catch (e) {
    // Ignore storage quota/availability errors; in-memory state still works.
  }
}

// applyCollapsed re-applies the persisted collapsed state to the freshly
// rendered tree.
function applyCollapsed(tree) {
  tree.querySelectorAll('li.page.has-children').forEach(function (li) {
    li.classList.toggle('collapsed', collapsed.has(li.dataset.key));
  });
}

function initTree() {
  // Destroy any stale Sortable instances before re-initializing.
  sortableInstances.forEach(function (inst) {
    inst.destroy();
  });
  sortableInstances = [];

  var tree = document.getElementById('tree');
  if (!tree) {
    return;
  }

  applyCollapsed(tree);

  var lists = tree.querySelectorAll('ul.children');
  lists.forEach(function (list) {
    var inst = Sortable.create(list, {
      group: 'pages',
      handle: '.drag-handle',
      animation: 150,
      fallbackOnBody: true,
      swapThreshold: 0.65,
      // Inverted swap makes the outer band of a row insert next to it, so
      // hovering the lower edge of the last subentry (in the gutter left of
      // its child list) inserts AFTER it — the "1.2" drop position that a
      // plain swap zone never offers with nested lists.
      invertSwap: true,
      invertedSwapThreshold: 0.65,
      // Only treat a list as an empty drop target when the pointer is right
      // inside it, so the empty child list under the last row does not grab
      // drops meant for the parent list's bottom strip.
      emptyInsertThreshold: 3,
      ghostClass: 'drag-ghost',
      onEnd: function (evt) {
        var sameList = evt.from === evt.to;
        var sameIndex = evt.oldIndex === evt.newIndex;
        if (sameList && sameIndex) {
          return;
        }

        var src = evt.item.dataset.path;
        var parent = evt.to.dataset.parent;
        var pos = evt.newIndex;

        // The page the editor has open travels with the move: moving into
        // another book renames files, and if the moved page is the open one
        // -- or sits inside a moved section -- the server answers with the
        // path the editor has to autosave to from now on.
        htmx.ajax('POST', '/move', {
          target: '#tree',
          swap: 'innerHTML',
          values: { src: src, parent: parent, pos: pos, open: currentPath || '' }
        });
      }
    });
    sortableInstances.push(inst);
  });
}

// initDivider makes a vertical divider draggable: dragging it resizes the
// pane next to it, which sits on the divider's left ('left') or right
// ('right') side. The chosen width is kept in localStorage under key so it
// survives reloads and the re-renders that replace the pane.
function initDivider(dividerID, paneID, key, side) {
  var divider = document.getElementById(dividerID);
  var pane = document.getElementById(paneID);
  if (!divider || !pane) {
    return;
  }

  var saved = localStorage.getItem(key);
  if (saved) {
    pane.style.width = saved;
  }

  divider.addEventListener('pointerdown', function (evt) {
    evt.preventDefault();
    divider.setPointerCapture(evt.pointerId);
    divider.classList.add('dragging');

    function onMove(e) {
      var panes = pane.parentElement.getBoundingClientRect();
      // Keep the pane on the other side usable; the resized pane's CSS
      // min-width provides the lower bound.
      var raw = side === 'right' ? panes.right - e.clientX : e.clientX - panes.left;
      var width = Math.max(Math.min(raw, panes.width - 200), 0);
      pane.style.width = width + 'px';
      refreshEditor(); // CodeMirror measures its own width
    }

    function onUp() {
      divider.removeEventListener('pointermove', onMove);
      divider.removeEventListener('pointerup', onUp);
      divider.classList.remove('dragging');
      localStorage.setItem(key, pane.style.width);
    }

    divider.addEventListener('pointermove', onMove);
    divider.addEventListener('pointerup', onUp);
  });
}

// The panes beside the editor
//
// The Markdown preview and the copyediting tool both work on the text the
// editor holds; the help stands beside them. Each is shown or hidden on its
// own; those shown stand side by side, in the order the user dragged them
// into. Which are
// shown, their order, and their widths are kept in localStorage: the editor
// pane is re-rendered on every page switch, and the layout should outlive
// that -- and the session.
//
// The panes and their dividers never move in the DOM; the CSS order
// property places them. Each pane's divider sits on its left and resizes
// it against whatever stands left of it: the editor, or another pane.

var SIDE_PANES = {
  preview: { pane: 'preview-pane', divider: 'preview-divider', open: 'previewOpen', width: 'previewPaneWidth' },
  copyedit: { pane: 'copyedit', divider: 'copyedit-divider', open: 'copyeditOpen', width: 'copyeditPaneWidth' },
  help: { pane: 'help-pane', divider: 'help-divider', open: 'helpOpen', width: 'helpPaneWidth' }
};

var PANE_ORDER_KEY = 'paneOrder';

var paneOpen = {
  preview: localStorage.getItem(SIDE_PANES.preview.open) !== 'closed',
  // Before the panes were separate, the copyedit pane was a tab beside the
  // preview's; one left up is taken as the wish to see it.
  copyedit: localStorage.getItem(SIDE_PANES.copyedit.open) === null
    ? localStorage.getItem('paneTab') === 'copyedit'
    : localStorage.getItem(SIDE_PANES.copyedit.open) === 'open',
  help: localStorage.getItem(SIDE_PANES.help.open) === 'open'
};

// paneOrder is the stored order, with a pane it does not name -- one added
// since the order was saved -- appended at the end.
var paneOrder = (function () {
  var order = (localStorage.getItem(PANE_ORDER_KEY) || '').split(',').filter(function (name, i, all) {
    return SIDE_PANES[name] && all.indexOf(name) === i;
  });
  Object.keys(SIDE_PANES).forEach(function (name) {
    if (order.indexOf(name) < 0) {
      order.push(name);
    }
  });
  return order;
})();

var previewTimer = null;

function sidePane(name) {
  return document.getElementById(SIDE_PANES[name].pane);
}

function visiblePanes() {
  return paneOrder.filter(function (name) { return paneOpen[name]; });
}

// paneWidth is the saved width of a pane, in percent of the split.
function paneWidth(name) {
  var saved = parseFloat(localStorage.getItem(SIDE_PANES[name].width));
  return saved > 0 ? saved : 0;
}

// applyLayout shows the panes that are open, in their order and at their
// widths, and marks the toggle buttons to match.
function applyLayout() {
  var visible = visiblePanes();
  // Widths saved while one pane stood alone may not leave the editor room
  // once both are shown; they are scaled down together rather than
  // crowding it out.
  var widths = {};
  var total = 0;
  visible.forEach(function (name) {
    widths[name] = paneWidth(name) || (visible.length > 1 ? 30 : 45);
    total += widths[name];
  });
  var scale = total > 70 ? 70 / total : 1;
  paneOrder.forEach(function (name, i) {
    var pane = sidePane(name);
    var divider = document.getElementById(SIDE_PANES[name].divider);
    var open = paneOpen[name];
    if (pane) {
      pane.hidden = !open;
      pane.style.order = String(2 * i + 2);
      if (open) {
        pane.style.width = (widths[name] * scale) + '%';
      }
    }
    if (divider) {
      divider.hidden = !open;
      divider.style.order = String(2 * i + 1);
    }
  });
  document.querySelectorAll('.pane-toggle').forEach(function (button) {
    button.setAttribute('aria-pressed', paneOpen[button.dataset.pane] ? 'true' : 'false');
  });
}

// applyPreview brings the panes and their toggle buttons in line with the
// layout. It runs after every swap that replaces the editor, both on
// afterSwap (so nothing flashes) and on afterSettle (which restores the
// swapped-in elements' attributes, including style and aria-pressed).
function applyPreview() {
  applyLayout();
  applyTaskSelection(); // the pane comes with the editor, ticks and all
  if (paneOpen.preview) {
    updatePreview();
  }
  if (paneOpen.help) {
    showHelp(helpPage, false); // the pane came back empty with the editor
  }
}

// togglePane shows or hides one of the panes beside the editor.
function togglePane(name) {
  if (!SIDE_PANES[name]) {
    return;
  }
  paneOpen[name] = !paneOpen[name];
  localStorage.setItem(SIDE_PANES[name].open, paneOpen[name] ? 'open' : 'closed');
  applyPreview();
  refreshEditor(); // the editor just gained or lost room
}

// Dragging a pane's divider resizes the pane against its left neighbour:
// against the editor, which takes up the difference, or against the other
// pane, the two then trading width while the editor keeps its own.
document.addEventListener('pointerdown', function (evt) {
  var divider = evt.target.closest && evt.target.closest('.split-divider');
  if (!divider || evt.button !== 0) {
    return;
  }
  var name = divider.dataset.pane;
  var pane = sidePane(name);
  var split = document.getElementById('editor-split');
  if (!pane || !split) {
    return;
  }
  var visible = visiblePanes();
  var at = visible.indexOf(name);
  var leftName = at > 0 ? visible[at - 1] : null;
  var left = leftName ? sidePane(leftName) : split.querySelector('.edit-form');
  if (!left) {
    return;
  }

  evt.preventDefault();
  divider.setPointerCapture(evt.pointerId);
  divider.classList.add('dragging');

  var splitWidth = split.getBoundingClientRect().width;
  var startX = evt.clientX;
  var startPane = pane.getBoundingClientRect().width;
  var startLeft = left.getBoundingClientRect().width;
  var minPane = parseFloat(getComputedStyle(pane).minWidth) || 0;
  var minLeft = parseFloat(getComputedStyle(left).minWidth) || 0;

  function onMove(e) {
    var dx = e.clientX - startX;
    var total = startLeft + startPane;
    var leftWidth = Math.max(minLeft, Math.min(startLeft + dx, total - minPane));
    pane.style.width = ((total - leftWidth) / splitWidth * 100) + '%';
    if (leftName) {
      left.style.width = (leftWidth / splitWidth * 100) + '%';
    }
    refreshEditor(); // CodeMirror measures its own width
  }

  function onUp() {
    divider.removeEventListener('pointermove', onMove);
    divider.removeEventListener('pointerup', onUp);
    divider.removeEventListener('pointercancel', onUp);
    divider.classList.remove('dragging');
    localStorage.setItem(SIDE_PANES[name].width, parseFloat(pane.style.width));
    if (leftName) {
      localStorage.setItem(SIDE_PANES[leftName].width, parseFloat(left.style.width));
    }
  }

  divider.addEventListener('pointermove', onMove);
  divider.addEventListener('pointerup', onUp);
  divider.addEventListener('pointercancel', onUp);
});

// Dragging one pane's head onto another pane swaps the two.
var draggedPane = null;

function clearPaneDrag() {
  document.querySelectorAll('.side-pane.pane-dragging, .side-pane.pane-drop-target').forEach(function (el) {
    el.classList.remove('pane-dragging', 'pane-drop-target');
  });
}

document.addEventListener('dragstart', function (evt) {
  var head = evt.target.closest && evt.target.closest('.side-pane-head');
  if (!head) {
    return;
  }
  var pane = head.closest('.side-pane');
  draggedPane = pane.dataset.pane;
  evt.dataTransfer.effectAllowed = 'move';
  // A type of its own, so the editor does not take the drop for text.
  evt.dataTransfer.setData('application/x-qm-pane', draggedPane);
  pane.classList.add('pane-dragging');
});

function paneDropTarget(evt) {
  var target = draggedPane && evt.target.closest && evt.target.closest('.side-pane');
  return target && target.dataset.pane !== draggedPane ? target : null;
}

document.addEventListener('dragover', function (evt) {
  var target = paneDropTarget(evt);
  if (target) {
    evt.preventDefault();
    evt.dataTransfer.dropEffect = 'move';
    target.classList.add('pane-drop-target');
  }
});

document.addEventListener('dragleave', function (evt) {
  var target = paneDropTarget(evt);
  if (target && !target.contains(evt.relatedTarget)) {
    target.classList.remove('pane-drop-target');
  }
});

document.addEventListener('drop', function (evt) {
  var target = paneDropTarget(evt);
  if (!target) {
    return;
  }
  evt.preventDefault();
  var from = paneOrder.indexOf(draggedPane);
  var to = paneOrder.indexOf(target.dataset.pane);
  paneOrder[from] = target.dataset.pane;
  paneOrder[to] = draggedPane;
  localStorage.setItem(PANE_ORDER_KEY, paneOrder.join(','));
  draggedPane = null;
  clearPaneDrag();
  applyLayout();
  refreshEditor();
});

document.addEventListener('dragend', function () {
  draggedPane = null;
  clearPaneDrag();
});

// The help pane
//
// The help is a set of Markdown pages embedded in the binary, in one or
// more languages. /help lists the languages and the topics of one of them,
// /help/<lang>/<name> serves a page, and the pane renders it with the
// preview's renderer. A link to another help page opens that page in the
// pane; a link out of the help opens in a new tab, so the app stays put.
//
// The help is shown in the language the user picked in the pane, else in
// the first of the browser's languages -- which follow the system's -- the
// help is written in, else in English. The page shown and the language
// picked are remembered across sessions; what was fetched is kept for the
// session, since the pane is rebuilt on every page switch and the help does
// not change while the app runs.

var HELP_PAGE_KEY = 'helpPage';
var HELP_LANG_KEY = 'helpLang';
var HELP_INDEX = 'index.md';
var helpPageName = /^[A-Za-z0-9_-]+\.md$/;

// HELP_LABELS are the pane's own buttons in the languages the help is
// written in; a language without an entry keeps the English ones.
var HELP_LABELS = {
  en: { back: '◀ Back', home: 'Contents', error: 'The help could not be loaded.' },
  de: { back: '◀ Zurück', home: 'Inhalt', error: 'Die Hilfe konnte nicht geladen werden.' }
};

var helpPage = localStorage.getItem(HELP_PAGE_KEY) || HELP_INDEX;
var helpHistory = [];
var helpCache = {};
var helpContents = null;
var helpScroll = 0;

// helpWanted is the language list the server picks the help's language
// from: the one picked in the pane, else the browser's own.
function helpWanted() {
  var picked = localStorage.getItem(HELP_LANG_KEY);
  if (picked) {
    return picked;
  }
  var langs = navigator.languages && navigator.languages.length
    ? navigator.languages : [navigator.language || ''];
  return langs.join(',');
}

// fetchHelpContents gets the languages and, in the language the help is
// shown in, its topics.
function fetchHelpContents() {
  if (helpContents) {
    return Promise.resolve(helpContents);
  }
  return fetch('/help?lang=' + encodeURIComponent(helpWanted())).then(function (resp) {
    if (!resp.ok) {
      throw new Error('no help');
    }
    return resp.json();
  }).then(function (contents) {
    helpContents = contents;
    return contents;
  });
}

// fetchHelp gets one help page's text, from the cache when it was fetched
// before.
function fetchHelp(lang, name) {
  var key = lang + '/' + name;
  if (helpCache[key] !== undefined) {
    return Promise.resolve(helpCache[key]);
  }
  return fetch('/help/' + encodeURIComponent(lang) + '/' + encodeURIComponent(name)).then(function (resp) {
    if (!resp.ok) {
      throw new Error('no help page ' + name);
    }
    return resp.text();
  }).then(function (text) {
    helpCache[key] = text;
    return text;
  });
}

// fillSelect replaces the options of the select with id by items, each
// made of value and label by the two functions, and selects current.
function fillSelect(id, items, value, label, current) {
  var select = document.getElementById(id);
  if (!select) {
    return;
  }
  select.innerHTML = '';
  items.forEach(function (item) {
    var option = document.createElement('option');
    option.value = value(item);
    option.textContent = label(item);
    select.appendChild(option);
  });
  select.value = current;
}

// fillHelpNav brings the pane's navigation in line with the page shown:
// the topics and the languages in their menus, the buttons in the
// language of the help.
function fillHelpNav(contents) {
  var get = function (key) { return function (item) { return item[key]; }; };
  fillSelect('help-topic', contents.topics, get('name'), get('title'), helpPage);
  var lang = document.getElementById('help-lang');
  if (lang) {
    fillSelect('help-lang', contents.languages, get('code'), get('name'), contents.lang);
    lang.hidden = contents.languages.length < 2;
  }
  var labels = HELP_LABELS[contents.lang] || HELP_LABELS.en;
  var back = document.getElementById('help-back');
  if (back) {
    back.textContent = labels.back;
    back.disabled = helpHistory.length === 0;
  }
  var home = document.getElementById('help-home');
  if (home) {
    home.textContent = labels.home;
  }
}

// linkHelp points the links of a rendered help page where they belong: a
// link to another help page opens it in the pane, anything else in a new
// tab.
function linkHelp(root) {
  root.querySelectorAll('a[href]').forEach(function (a) {
    var href = a.getAttribute('href');
    var name = href.replace(/#.*$/, '');
    if (helpPageName.test(name)) {
      a.setAttribute('data-help', name);
    } else if (href.charAt(0) !== '#') {
      a.setAttribute('target', '_blank');
      a.setAttribute('rel', 'noopener');
    }
  });
}

// showHelp renders the help page name in the pane. With remember, the page
// shown before is put on the Back button's history; without, the page is
// shown again where it was scrolled to, the pane having only been rebuilt.
function showHelp(name, remember) {
  var el = document.getElementById('help');
  if (!el) {
    return;
  }
  if (!helpPageName.test(name)) {
    name = HELP_INDEX;
  }
  var contents;
  fetchHelpContents().then(function (c) {
    contents = c;
    return fetchHelp(contents.lang, name).catch(function () {
      name = HELP_INDEX; // a page remembered from an older build may be gone
      return fetchHelp(contents.lang, name);
    });
  }).then(function (text) {
    if (remember && name !== helpPage) {
      helpHistory.push(helpPage);
    }
    var again = !remember && name === helpPage;
    helpPage = name;
    localStorage.setItem(HELP_PAGE_KEY, name);
    renderPreview(el, text, '');
    linkHelp(el);
    el.lang = contents.lang;
    el.scrollTop = again ? helpScroll : 0;
    helpScroll = el.scrollTop;
    fillHelpNav(contents);
  }).catch(function () {
    var labels = HELP_LABELS[contents ? contents.lang : 'en'] || HELP_LABELS.en;
    el.textContent = labels.error;
  });
}

document.addEventListener('scroll', function (evt) {
  if (evt.target && evt.target.id === 'help') {
    helpScroll = evt.target.scrollTop;
  }
}, true);

document.addEventListener('click', function (evt) {
  var link = evt.target.closest && evt.target.closest('#help a[data-help]');
  if (link) {
    evt.preventDefault();
    showHelp(link.getAttribute('data-help'), true);
    return;
  }
  if (evt.target.closest && evt.target.closest('#help-home')) {
    showHelp(HELP_INDEX, true);
    return;
  }
  if (evt.target.closest && evt.target.closest('#help-back') && helpHistory.length > 0) {
    var previous = helpHistory.pop();
    helpPage = previous; // not a step forward, so nothing goes on the history
    helpScroll = 0;
    showHelp(previous, false);
  }
});

document.addEventListener('change', function (evt) {
  if (evt.target.id === 'help-topic') {
    showHelp(evt.target.value, true);
  }
  if (evt.target.id === 'help-lang') {
    // The same page, in the language picked; the topics come along anew.
    localStorage.setItem(HELP_LANG_KEY, evt.target.value);
    helpContents = null;
    helpScroll = 0;
    showHelp(helpPage, false);
  }
});

// updatePreview re-renders the preview from what the editor currently holds.
// The page's path travels with the render: the preview resolves the page's
// image paths against it. It is read from the form rather than from
// currentPath so that it always describes the editor actually on screen.
function updatePreview() {
  var preview = document.getElementById('preview');
  var editor = document.querySelector('#content textarea.file-content');
  if (preview && editor) {
    var path = document.querySelector('#content input[name="path"]');
    renderPreview(preview, editor.value, path ? path.value : '');
    followEditor(); // the render may have moved the text under the top
  }
}

// schedulePreview coalesces the keystrokes of fast typing into one render.
function schedulePreview() {
  if (!paneOpen.preview) {
    return;
  }
  clearTimeout(previewTimer);
  previewTimer = setTimeout(updatePreview, 150);
}

document.body.addEventListener('input', function (evt) {
  if (evt.target.classList.contains('file-content')) {
    schedulePreview();
  }
  if (evt.target.id === 'search-input') {
    // The field asks for its own hits; this only remembers the query.
    localStorage.setItem(SEARCH_KEY, evt.target.value);
  }
});

// The stylesheet dropdown only shows up above the preview when more than
// one custom CSS file exists (see the "content" template). Switching it
// swaps the document-wide <link> to the chosen file, remembers the choice
// server-side so it survives a restart and a fresh /open, and re-renders
// the preview so it reflects the new styles immediately.
document.body.addEventListener('change', function (evt) {
  if (evt.target.id !== 'preview-css-select') {
    return;
  }
  var name = evt.target.value;
  var link = document.getElementById('preview-css-link');
  if (link) {
    link.href = '/config/preview.css?file=' + encodeURIComponent(name) + '&v=' + Date.now();
  }
  fetch('/config/active-css', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: 'file=' + encodeURIComponent(name)
  });
  updatePreview();
});

// Copyediting
//
// The copyedit pane lists the editing tasks the user configured. Picking
// one posts the page the editor holds to the selected model (htmx does
// that itself) and swaps the list for the suggestions it answered with.
// Each suggestion carries the place in the page it is about, and those
// places are what the editor marks, so the advice is read on the text it
// is about. The Back button asks for the task list again, which clears
// the marks along with the suggestions.

// The dropdown in the copyedit pane's head picks the connection a run goes
// to. The choice is remembered server-side, beside the connections
// themselves, so it survives a restart and a fresh /open.
document.body.addEventListener('change', function (evt) {
  if (evt.target.classList.contains('copyedit-select')) {
    applyTaskSelection();
    saveTaskSelection();
    return;
  }
  if (evt.target.id === 'copyedit-select-all') {
    toggleAllTasks(evt.target.checked);
    return;
  }
  if (evt.target.id !== 'copyedit-connection') {
    return;
  }
  fetch('/copyedit/active', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: 'connection=' + encodeURIComponent(evt.target.value)
  });
});

function suggestionEntries() {
  return Array.prototype.slice.call(document.querySelectorAll('#copyedit-body .copyedit-suggestion'));
}

function taskCheckboxes() {
  return Array.prototype.slice.call(document.querySelectorAll('#copyedit-body .copyedit-select'));
}

// applyTaskSelection brings the two things that describe the selection in
// line with the ticks: the Run button, which is only worth pressing with
// something ticked and says how many so a run's size is known before it
// is paid for, and the Select all box, which is ticked for all, clear for
// none, and in between for some -- the state a checkbox can only be put
// into from script.
function applyTaskSelection() {
  var boxes = taskCheckboxes();
  var picked = boxes.filter(function (box) {
    return box.checked;
  }).length;

  var button = document.getElementById('copyedit-run-selected');
  if (button) {
    button.disabled = picked === 0;
    button.textContent = picked > 1 ? 'Run ' + picked + ' tasks together' : 'Run selected';
  }
  var all = document.getElementById('copyedit-select-all');
  if (all) {
    all.checked = picked > 0 && picked === boxes.length;
    all.indeterminate = picked > 0 && picked < boxes.length;
  }
}

// saveTaskSelection tells the server which tasks are ticked. It is a
// setting like the model beside it, so it outlives the page switch that
// re-renders the pane and the restart that ends the session.
function saveTaskSelection() {
  var picked = taskCheckboxes().filter(function (box) {
    return box.checked;
  });
  var body = new URLSearchParams();
  picked.forEach(function (box) {
    body.append('selected', box.value);
  });
  fetch('/copyedit/selection', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: body.toString()
  });
}

// toggleAllTasks ticks every task, or clears them: whichever the box is
// not already showing. Partly ticked counts as not all, so the first
// click from there selects the rest rather than throwing away what is
// ticked.
function toggleAllTasks(on) {
  taskCheckboxes().forEach(function (box) {
    box.checked = on;
  });
  applyTaskSelection();
  saveTaskSelection();
}

// filterSuggestions shows one task's group, or all of them. It hides list
// entries only: the marks in the text stay where the run put them, and
// CodeMirror keeps them on their passages while the user edits. Clearing
// and re-laying them per filter would mean re-deriving positions that the
// edits have since moved.
function filterSuggestions(task) {
  document.querySelectorAll('#copyedit-body .copyedit-filter').forEach(function (chip) {
    chip.classList.toggle('active', chip.dataset.task === task);
  });
  document.querySelectorAll('#copyedit-body .copyedit-group').forEach(function (group) {
    group.hidden = task !== '' && group.dataset.task !== task;
  });
}

// readSuggestions hands the editor the passages the suggestions now on
// screen are about. A suggestion whose passage was not found in the page
// carries no position and is passed as a gap, so that the nth entry and
// the nth mark stay the same suggestion. The task list carries no entries
// at all, which is what takes the marks off the page again.
function readSuggestions() {
  setCopyeditMarks(suggestionEntries().map(function (li) {
    if (li.dataset.start === undefined) {
      return null;
    }
    return { start: Number(li.dataset.start), end: Number(li.dataset.end) };
  }));
}

// selectSuggestion singles out the entry the user clicked, in the list and
// in the text, and scrolls the editor to the passage it is about.
function selectSuggestion(entry) {
  var entries = suggestionEntries();
  entries.forEach(function (li) {
    li.classList.toggle('selected', li === entry);
  });
  focusCopyeditMark(entries.indexOf(entry));
}

// applySuggestion writes a suggestion into the page. The editor does the
// writing, at the mark rather than at the offsets the suggestion arrived
// with, so a passage that has moved under an edit is still the one
// replaced; from there the change is an ordinary edit, which the autosave
// and the preview pick up on their own, and which Ctrl-Z takes back.
//
// The entry stays in the list, marked as carried out: it says what was
// changed, and it is no longer something to click.
function applySuggestion(entry) {
  var index = suggestionEntries().indexOf(entry);
  var written = applyCopyeditMark(index, entry.dataset.replacement || '');
  if (!written) {
    // The passage is gone from the page -- deleted or rewritten since the
    // run -- so there is nothing to replace, and saying so beats leaving
    // a button that does nothing.
    settleSuggestion(entry, 'stale', 'Gone', 'This passage is no longer in the page');
    return;
  }
  settleSuggestion(entry, 'applied', 'Applied', 'Written into the page — undo it in the editor');
}

// finishSuggestion is the Done button: the user dealt with this one
// themselves, in the editor rather than by the Apply button, so the
// suggestion is finished with. Its mark comes off the text -- the passage
// is not waiting for anything any more -- and the entry stays in the list
// as a record of what was seen to.
//
// Done is offered on every suggestion, the ones with no replacement and
// the ones whose passage was never found included: those are exactly the
// ones that can only be carried out by hand.
function finishSuggestion(entry) {
  clearCopyeditMark(suggestionEntries().indexOf(entry));
  settleSuggestion(entry, 'done', 'Done', 'Dealt with by hand');
}

// settleSuggestion takes an entry out of use, however it was dealt with:
// the state goes on the entry, and both buttons stop offering what has
// already happened.
function settleSuggestion(entry, state, label, why) {
  entry.classList.add(state);
  entry.classList.remove('selected');
  var apply = entry.querySelector('.copyedit-apply');
  var done = entry.querySelector('.copyedit-done');
  if (apply) {
    apply.disabled = true;
    if (state !== 'done') {
      apply.textContent = label;
      apply.title = why;
    }
  }
  if (done) {
    done.disabled = true;
    done.textContent = state === 'done' ? label : '✓';
    done.title = why;
  }
}

// The chat
//
// The box at the foot of the copyedit pane talks to the selected model
// about the documentation. The conversation is written into the pane's
// body, which is what keeps it with the page across page switches (see
// stashSuggestions) and what Back clears: asked from the task list, it
// takes the list's place; asked while a run's suggestions are on screen,
// it continues below them, so the run can be asked about.
//
// The server keeps nothing between two messages. Each one is sent with the
// conversation so far, read from the turns on screen, and with the page the
// editor holds, which is what the model's suggested edits are located in.
// Those edits come back as suggestion entries like a run's, so marking,
// Apply, and Done work on them unchanged.

function chatLog() {
  var body = document.getElementById('copyedit-body');
  if (!body) {
    return null;
  }
  var log = body.querySelector('.copyedit-chat-log');
  if (log) {
    return log;
  }
  var run = body.querySelector('.copyedit-suggestions');
  if (run) {
    log = document.createElement('div');
    log.className = 'copyedit-chat-log';
    var head = document.createElement('h3');
    head.className = 'copyedit-group-name';
    head.textContent = 'Chat';
    run.appendChild(head);
    run.appendChild(log);
    return log;
  }
  var shell = document.getElementById('copyedit-chat-shell');
  if (!shell) {
    return null;
  }
  clearCopyeditMarks();
  body.innerHTML = '';
  body.appendChild(shell.content.cloneNode(true));
  htmx.process(body); // the Back button is an htmx one
  return body.querySelector('.copyedit-chat-log');
}

// chatHistory is the conversation on screen, as the server reads it. An
// answer that failed carries no role and is left out.
function chatHistory() {
  return Array.prototype.slice.call(document.querySelectorAll('#copyedit-body .chat-turn[data-role]')).map(function (turn) {
    return { role: turn.dataset.role, text: turn.dataset.history || '' };
  });
}

var chatPending = false;

function sendChat() {
  var field = document.getElementById('copyedit-chat-message');
  var message = field ? field.value.trim() : '';
  if (!message || chatPending) {
    return;
  }
  var log = chatLog();
  if (!log) {
    return;
  }
  var history = chatHistory();
  syncSuggestionOffsets(); // the marks are laid anew once the answer is in

  var turn = document.createElement('div');
  turn.className = 'chat-turn chat-user';
  turn.dataset.role = 'user';
  turn.dataset.history = message;
  var text = document.createElement('div');
  text.className = 'chat-text';
  text.textContent = message;
  turn.appendChild(text);
  log.appendChild(turn);

  var waiting = document.createElement('p');
  waiting.className = 'chat-waiting';
  waiting.textContent = 'Asking the model…';
  log.appendChild(waiting);
  waiting.scrollIntoView({ block: 'nearest' });

  field.value = '';
  setChatPending(true);
  var form = new URLSearchParams();
  form.append('message', message);
  form.append('history', JSON.stringify(history));
  form.append('path', editorPagePath());
  form.append('body', editorText());
  fetch('/copyedit/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: form.toString()
  }).then(function (resp) {
    return resp.text();
  }).then(function (html) {
    waiting.insertAdjacentHTML('beforebegin', html);
  }).catch(function (err) {
    var failed = document.createElement('div');
    failed.className = 'chat-turn chat-assistant';
    failed.innerHTML = '<div class="error"></div>';
    failed.firstChild.textContent = 'The question did not reach qm: ' + err;
    waiting.parentNode.insertBefore(failed, waiting);
  }).then(function () {
    waiting.remove();
    setChatPending(false);
    // The log may have gone meanwhile -- Back, or another page -- and
    // then there is nothing to mark.
    if (document.body.contains(log)) {
      readSuggestions();
      var last = log.lastElementChild;
      if (last) {
        last.scrollIntoView({ block: 'nearest' });
      }
    }
  });
}

function setChatPending(on) {
  chatPending = on;
  var send = document.getElementById('copyedit-chat-send');
  if (send) {
    send.disabled = on;
  }
}

document.body.addEventListener('submit', function (evt) {
  if (evt.target.id === 'copyedit-chat') {
    evt.preventDefault();
    sendChat();
  }
});

// Enter sends, as in any chat; Shift+Enter is the way to a new line.
document.body.addEventListener('keydown', function (evt) {
  if (evt.target.id === 'copyedit-chat-message' && evt.key === 'Enter' && !evt.shiftKey && !evt.isComposing) {
    evt.preventDefault();
    sendChat();
  }
});

// Suggestions kept across page switches
//
// The copyedit pane is part of the editor pane, and the pane is replaced
// whenever another page is opened -- which would throw away a run the user
// has not finished working through, and a run costs a model call. So the
// suggestions on screen are put aside, per page, as the pane goes, and put
// back when that page is opened again. sessionStorage carries them, so they
// also outlive the trip to the config pages and back.
//
// What is kept is the list as it stands, Applied and Done included. The
// offsets in it are brought up to date first: the marks have moved with
// the text since the run, and the page's saved text is what the offsets
// will be read against when it comes back.

var COPYEDIT_KEY = 'copyeditSuggestions';

function loadStash() {
  try {
    return JSON.parse(sessionStorage.getItem(COPYEDIT_KEY) || '{}') || {};
  } catch (e) {
    return {};
  }
}

function saveStash(stash) {
  try {
    sessionStorage.setItem(COPYEDIT_KEY, JSON.stringify(stash));
  } catch (e) {
    // Out of room or storage off: the suggestions last this pane only.
  }
}

function editorPagePath() {
  var input = document.getElementById('content-path');
  return input ? input.value : '';
}

// moveStash keeps the suggestions put aside for a page whose file was
// renamed under the name the page has now.
function moveStash(from, to) {
  if (!from || !to || from === to) {
    return;
  }
  var stash = loadStash();
  if (stash[from] !== undefined) {
    stash[to] = stash[from];
    delete stash[from];
    saveStash(stash);
  }
}

// stashSuggestions puts the suggestions now on screen aside for the page
// the editor holds, or forgets what was kept for it when the pane shows the
// task list: going back to the tasks is how a run is put away.
function stashSuggestions() {
  var path = editorPagePath();
  var body = document.getElementById('copyedit-body');
  if (!path || !body) {
    return;
  }
  var stash = loadStash();
  if (!body.querySelector('.copyedit-suggestions')) {
    delete stash[path];
    saveStash(stash);
    return;
  }
  syncSuggestionOffsets();
  suggestionEntries().forEach(function (li) {
    li.classList.remove('selected');
  });
  stash[path] = body.innerHTML;
  saveStash(stash);
}

// syncSuggestionOffsets writes where each suggestion's mark is now back
// into its entry. The marks have moved with the text since the offsets
// arrived, and the entries are what the marks are laid from again: when
// the list is put back after a page switch, and when a chat answer adds
// suggestions of its own to the ones already marked.
function syncSuggestionOffsets() {
  var ranges = copyeditMarkRanges();
  var text = editorText();
  suggestionEntries().forEach(function (li, i) {
    var at = ranges[i];
    if (at) {
      li.dataset.start = at.start;
      li.dataset.end = at.end;
      li.dataset.passage = text.slice(at.start, at.end);
    } else {
      // Applied, done, or gone: nothing in the text to point at any more.
      delete li.dataset.start;
      delete li.dataset.end;
      delete li.dataset.passage;
    }
  });
}

// restoreSuggestions puts back what was kept for the page now in the
// editor. The page may have changed in between -- on disk, or by a reload
// -- so every passage is checked against the text before it is marked: one
// that has moved is found again, one that is gone is no longer pointed at.
function restoreSuggestions() {
  var path = editorPagePath();
  var body = document.getElementById('copyedit-body');
  var html = path && body ? loadStash()[path] : null;
  if (!html) {
    return;
  }
  body.innerHTML = html;
  var text = editorText();
  suggestionEntries().forEach(function (li) {
    if (li.dataset.start === undefined) {
      return;
    }
    var passage = li.dataset.passage;
    var start = Number(li.dataset.start);
    if (passage === undefined || text.slice(start, Number(li.dataset.end)) === passage) {
      return;
    }
    var found = text.indexOf(passage);
    if (found >= 0) {
      li.dataset.start = found;
      li.dataset.end = found + passage.length;
      return;
    }
    delete li.dataset.start;
    delete li.dataset.end;
    li.classList.add('unlocated');
    var apply = li.querySelector('.copyedit-apply');
    if (apply) {
      apply.disabled = true;
      apply.title = 'This passage is no longer in the page';
    }
  });
  htmx.process(body); // the Back button is an htmx one
  readSuggestions();
}

// forgetSuggestions drops everything kept: another project is open, and
// its pages are not the ones the kept runs were about.
function forgetSuggestions() {
  saveStash({});
}

function editorText() {
  var editor = document.querySelector('#content textarea.file-content');
  return editor ? editor.value : '';
}

// currentPath is the page open in the editor; applySelection re-highlights
// it after every tree re-render (moves, saves, reloads).
var currentPath = null;

// syncCurrentPath reads the path out of the editor pane now on screen. The
// pane arrives either from a click on a tree entry or, when the app page is
// loaded, already filled with the page the project last had open; both must
// leave the same entry selected in the tree.
function syncCurrentPath() {
  var input = document.querySelector('#content input[name="path"]');
  currentPath = input ? input.value : null;
}

function applySelection() {
  document.querySelectorAll('#tree li.page').forEach(function (li) {
    li.classList.toggle('selected', !!currentPath && li.dataset.path === currentPath);
  });
  applySearchSelection();
  // The flip buttons name the pages around the selected one, so they are
  // answered from the same tree at the same moments.
  applyPageNav();
}

// Page flipping
//
// The two buttons below the preview open the page before and the page after
// the open one. The tree standing in the page is what they read: it holds
// the project in the order the book has it, it is refreshed every two
// seconds, and reading it there is what keeps the buttons right while pages
// are created, deleted, and dragged around under the editor.

// pageLinks are the tree's page entries, in the order the tree lists them.
// Only a page carries a link -- a folder that is not a page of its own is a
// span -- so this is exactly what can be flipped to.
function pageLinks() {
  return Array.prototype.slice.call(document.querySelectorAll('#tree a.title'));
}

// pageNeighbour is the tree entry step places away from the open page, or
// null when there is none: at either end of the book, and whenever the
// editor holds something the tree does not list, such as a file opened from
// the Git panel.
function pageNeighbour(step) {
  var links = pageLinks();
  var at = -1;
  for (var i = 0; i < links.length; i++) {
    var li = links[i].closest('li.page');
    if (li && currentPath && li.dataset.path === currentPath) {
      at = i;
      break;
    }
  }
  if (at < 0) {
    return null;
  }
  var to = at + step;
  return to >= 0 && to < links.length ? links[to] : null;
}

// applyPageNav offers each button the page it would open, by name, and
// takes it out of use where there is no such page.
function applyPageNav() {
  [['page-prev', -1, 'The page before this one'],
  ['page-next', 1, 'The page after this one']].forEach(function (nav) {
    var button = document.getElementById(nav[0]);
    if (!button) {
      return;
    }
    var link = pageNeighbour(nav[1]);
    button.disabled = !link;
    button.title = link ? link.textContent.trim() : nav[2] + ': there is none';
  });
}

// flipPage opens the neighbouring page through its own entry in the tree,
// so that flipping is the same act as clicking the page there -- the
// unsaved-edit confirmation and the tree's selection included. A page
// inside a collapsed branch is revealed the way a restored one is.
function flipPage(step) {
  var link = pageNeighbour(step);
  if (!link) {
    return;
  }
  link.click();
  revealSelection();
}

// Image freshness
//
// The preview's images are files the browser fetches and caches by URL, and
// an image replaced on disk keeps the URL the page names it by -- so the
// browser would go on showing the picture it cached. The URLs carry a stamp
// (see preview.js) that is renewed here: once at load, and again whenever
// the user asks for the page from disk again, which is what makes ↻ Reload
// reach the images as well as the text.
function bumpMediaVersion() {
  setMediaVersion(String(Date.now()));
}

// An image changed on disk -- the image editor saved it, most likely --
// is announced by /watch, the tree's poll, as an event. The preview then
// fetches its images again: a new stamp, and a render that puts it into
// the image URLs. The editor's text is left as it is.
document.body.addEventListener('qm-media-changed', function () {
  bumpMediaVersion();
  updatePreview();
});

// A double-click on an image of the project in the preview opens the
// image's file in the image editor set under Config. What the server says
// back -- nothing, or why the editor did not start -- is shown where the
// Editor button shows its own.
document.body.addEventListener('dblclick', function (evt) {
  var img = evt.target.closest && evt.target.closest('#preview img[data-media-path]');
  if (!img || !document.getElementById('external-editor-status')) {
    return;
  }
  evt.preventDefault();
  htmx.ajax('POST', '/editor/image', {
    target: '#external-editor-status',
    swap: 'innerHTML',
    values: { path: img.dataset.mediaPath }
  });
});

// revealSelection expands the collapsed branches above the selected page,
// so a restored page is not hidden inside one, and scrolls the tree pane
// to its row: flipping through a long book walks the selection off the
// visible part of the tree, where it would look as if it had been lost.
function revealSelection() {
  var li = currentPath && document.querySelector('#tree li.page.selected');
  if (!li) {
    return;
  }
  for (var node = li.parentElement; node; node = node.parentElement) {
    if (node.id === 'tree') {
      break;
    }
    if (node.classList && node.classList.contains('collapsed')) {
      node.classList.remove('collapsed');
      collapsed.delete(node.dataset.key);
      saveCollapsed();
    }
  }
  // The row, not the whole entry: an entry holds its subtree, which can be
  // taller than the pane.
  var row = li.querySelector(':scope > .row');
  if (row && row.scrollIntoView) {
    row.scrollIntoView({ block: 'nearest' });
  }
}

// Search
//
// The search field in the top bar is answered by /search with the pages
// that match, as data: the paths and their hit counts. While a query
// stands, the tree pane shows those pages as a list in place of the tree,
// in the order the tree has them and under the titles it gives them -- the
// tree stays in the page underneath, hidden, and keeps being refreshed, so
// the list is rebuilt from here after every swap of either, and clearing
// the search brings the tree back as the user left it.

var SEARCH_KEY = 'searchQuery';

// SEARCH_CASE_KEY remembers whether the case button is pressed.
var SEARCH_CASE_KEY = 'searchCase';

// searchHits maps a page's path to its number of hits, for the query the
// field currently holds.
var searchHits = new Map();

var searchRetry = null;

function searchQuery() {
  var input = document.getElementById('search-input');
  return input ? input.value : '';
}

// searchQuery is the query as the user wrote it; parseQuery below is what
// it means. The server reads it again for itself -- it has to, the index is
// there -- and the two readings have to agree, or the editor would paint
// something else than the tree counted. search.go holds the same rules in
// the same order, and its tests are the ones that pin them down.

// SEPARATORS is what stands between words: whitespace, punctuation of any
// script, and every ASCII character that is not a letter or a digit --
// Markdown and YAML are written entirely in those, so their syntax never
// ends up inside a word. Everything else belongs to one, emoji included.
var SEPARATORS = '\\s\\p{P}\\x00-\\x2F\\x3A-\\x40\\x5B-\\x60\\x7B-\\x7F';
var WORD_CHAR = '[^' + SEPARATORS + ']';
var NON_WORD_CHAR = '[' + SEPARATORS + ']';

// JOINER is what holds a compound word together. A single one of these
// between two word characters is part of the word -- "semi-wide" is one
// word -- while a run of them is the Markdown that it looks like.
var JOINER = '[-\\u2010\\u2011]';

var wordChar = new RegExp('^' + WORD_CHAR + '$', 'u');
var wordRun = new RegExp(WORD_CHAR + '+(?:' + JOINER + WORD_CHAR + '+)*', 'gu');

// QUOTE_ENDS pairs every quotation mark that can open a phrase with the one
// that closes it. The typographic closers are deliberately not openers, so
// that the apostrophe of "don’t" cannot start a phrase.
var QUOTE_ENDS = { '"': '"', "'": "'", '\u201C': '\u201D', '\u2018': '\u2019' };

// searchWords splits text into words the way the index splits page text.
// The words keep their case; whether it matters is the case button's call.
function searchWords(text) {
  return text.match(wordRun) || [];
}

// searchCased says whether the case button is pressed: the search then
// tells "Go" from "go".
function searchCased() {
  var button = document.getElementById('search-case');
  return !!button && button.getAttribute('aria-pressed') === 'true';
}

function setSearchCased(on) {
  var button = document.getElementById('search-case');
  if (button) {
    button.setAttribute('aria-pressed', on ? 'true' : 'false');
  }
}

// parseQuery reads a query into the terms it asks for: {words, exact}.
// Words between a pair of quotes are one term whose words have to stand
// together, everything outside is one term per word, and a closing quote
// also says the phrase is finished, which makes its last word exact.
function parseQuery(q) {
  var terms = [];
  // By code point, so that an emoji is one character and not two.
  var chars = Array.from(q);
  var loose = 0;
  for (var i = 0; i < chars.length; i++) {
    var end = QUOTE_ENDS[chars[i]];
    // A quote only quotes where a phrase can begin -- inside a word it is
    // an apostrophe.
    if (!end || (i > 0 && wordChar.test(chars[i - 1]))) {
      continue;
    }
    var close = chars.length; // an unclosed quote quotes the rest
    var closed = false;
    for (var j = i + 1; j < chars.length; j++) {
      if (chars[j] === end && (j + 1 === chars.length || !wordChar.test(chars[j + 1]))) {
        close = j;
        closed = true;
        break;
      }
    }
    terms = terms.concat(looseTerms(chars.slice(loose, i).join('')));
    terms.push({ words: searchWords(chars.slice(i + 1, close).join('')), exact: closed });
    i = close;
    loose = Math.min(close + 1, chars.length);
  }
  terms = terms.concat(looseTerms(chars.slice(loose).join('')));
  return terms.filter(function (term) {
    return !skipTerm(term);
  });
}

// looseTerms are the unquoted words of a query: one term each, every one of
// them matching by prefix.
function looseTerms(text) {
  return searchWords(text).map(function (word) {
    return { words: [word], exact: false };
  });
}

// skipTerm drops the terms the server drops: empty quotes, and the single
// letter or digit that starts almost every page and would light the whole
// tree up after the first keystroke. Any other single character -- an
// emoji, a symbol -- is rare enough to be exactly what was meant.
function skipTerm(term) {
  if (!term.words.length) {
    return true;
  }
  if (term.words.length > 1) {
    return false;
  }
  var chars = Array.from(term.words[0]);
  return chars.length === 1 && /[\p{L}\p{N}]/u.test(chars[0]);
}

// runSearch asks for the hits of whatever the field holds. The field posts
// itself as it is typed in; this is for the times the field did not change
// but the answer may have -- the query restored on startup, the retry while
// the index is still being built, a press of Enter or of the case button.
// fresh asks the server to read the project anew rather than trust its
// index: it is what Enter sends, since the user asking again for the same
// query is the user saying the answer looks out of date.
function runSearch(fresh) {
  var url = '/search?q=' + encodeURIComponent(searchQuery());
  if (searchCased()) {
    url += '&case=1';
  }
  if (fresh) {
    url += '&fresh=1';
  }
  htmx.ajax('GET', url, {
    target: '#search-results',
    swap: 'innerHTML'
  });
}

// readHits takes an answer apart and applies it to the tree pane and to
// the editor.
function readHits() {
  searchHits = new Map();
  document.querySelectorAll('#search-results .search-hits li').forEach(function (li) {
    searchHits.set(li.dataset.path, Number(li.dataset.count));
  });
  renderSearchList();
  setSearchTerms(parseQuery(searchQuery()), searchCased());

  // The index is rebuilt in the background whenever the project changed on
  // disk. Until the new one is in, the answer describes the project as it
  // was a moment ago, and is worth asking for again.
  clearTimeout(searchRetry);
  if (document.querySelector('#search-results [data-indexing]')) {
    searchRetry = setTimeout(runSearch, 400);
  }
}

// searchActive says whether the field holds a query that asks for
// anything. A lone letter does not -- the server ignores it too -- and
// replacing the tree with an empty list after the first keystroke would
// only make the tree flicker.
function searchActive() {
  return parseQuery(searchQuery()).length > 0;
}

// renderSearchList shows the pages that matched in place of the tree, each
// with its title, its path, and its number of hits. The pages come in the
// tree's order, which is the book's, and with the tree's titles; a hit the
// tree does not list -- the tree may be a moment behind the index -- is
// added at the end under its path.
function renderSearchList() {
  var pane = document.getElementById('tree-pane');
  var list = document.getElementById('search-list');
  if (!pane || !list) {
    return;
  }
  var active = searchActive();
  pane.classList.toggle('searching', active);
  list.hidden = !active;
  list.textContent = '';
  if (!active) {
    return;
  }

  var titles = new Map();
  document.querySelectorAll('#tree li.page').forEach(function (li) {
    var title = li.querySelector(':scope > .row > .title');
    if (li.dataset.path && title) {
      titles.set(li.dataset.path, title.textContent.trim());
    }
  });
  var paths = [];
  titles.forEach(function (title, path) {
    if (searchHits.has(path)) {
      paths.push(path);
    }
  });
  searchHits.forEach(function (count, path) {
    if (!titles.has(path)) {
      paths.push(path);
    }
  });

  if (!paths.length) {
    var empty = document.createElement('p');
    empty.className = 'empty';
    var summary = document.querySelector('#search-results .search-summary');
    empty.textContent = summary && summary.textContent === 'Indexing…'
      ? 'Indexing…'
      : 'No page matches the search.';
    list.appendChild(empty);
    return;
  }

  var ul = document.createElement('ul');
  ul.className = 'search-list-items';
  paths.forEach(function (path) {
    var li = document.createElement('li');
    li.className = 'search-item';
    li.dataset.path = path;
    var link = document.createElement('a');
    link.href = '#';
    link.className = 'search-link';
    // Opened the way the tree opens a page, so the guard for edits that
    // could not be saved asks here too.
    link.setAttribute('hx-get', '/content?path=' + encodeURIComponent(path));
    link.setAttribute('hx-target', '#content');
    var title = document.createElement('span');
    title.className = 'search-title';
    title.textContent = titles.get(path) || path;
    var where = document.createElement('span');
    where.className = 'search-path';
    where.textContent = path;
    link.appendChild(title);
    link.appendChild(where);
    var count = document.createElement('span');
    count.className = 'search-count';
    count.textContent = searchHits.get(path);
    count.title = searchHits.get(path) === 1 ? '1 hit' : searchHits.get(path) + ' hits';
    li.appendChild(link);
    li.appendChild(count);
    ul.appendChild(li);
  });
  list.appendChild(ul);
  htmx.process(list);
  applySearchSelection();
}

// applySearchSelection marks the open page in the result list, the way the
// tree marks it.
function applySearchSelection() {
  document.querySelectorAll('#search-list li.search-item').forEach(function (li) {
    li.classList.toggle('selected', !!currentPath && li.dataset.path === currentPath);
  });
}

// refreshHits rebuilds the result list from a tree that has just been
// rendered again, and asks for the hits anew: what the tree shows changed,
// which is why it was rendered again, so the counts may have changed too.
function refreshHits() {
  renderSearchList();
  if (searchQuery()) {
    runSearch();
  }
}

// clearSearch empties the field and asks for the answer to the empty
// query, which has no hits: that puts the tree back in place of the result
// list and takes the highlighting off the editor.
function clearSearch() {
  var input = document.getElementById('search-input');
  if (!input) {
    return;
  }
  input.value = '';
  localStorage.setItem(SEARCH_KEY, '');
  clearTimeout(searchRetry);
  runSearch();
  input.focus();
}

document.body.addEventListener('click', function (evt) {
  if (evt.target.closest('#search-clear')) {
    clearSearch();
    return;
  }
  if (evt.target.closest('#search-case')) {
    setSearchCased(!searchCased());
    localStorage.setItem(SEARCH_CASE_KEY, searchCased() ? '1' : '');
    clearTimeout(searchRetry);
    runSearch();
    return;
  }
  var link = evt.target.closest('#search-list a.search-link');
  if (link) {
    evt.preventDefault();
    var item = link.closest('li');
    currentPath = item ? item.dataset.path : null;
    applySelection();
  }
});

// Escape in the field clears it, as it does in a browser's own search
// fields -- not all of them do it for a page's field, so it is done here.
document.body.addEventListener('keydown', function (evt) {
  if (evt.target.id !== 'search-input') {
    return;
  }
  if (evt.key === 'Escape' && evt.target.value) {
    evt.preventDefault();
    clearSearch();
  }
  // Enter runs the search again even when the query did not change: the
  // field only posts itself when its value does, and the user pressing
  // Enter wants an answer that reflects the pages as they are now.
  if (evt.key === 'Enter') {
    evt.preventDefault();
    localStorage.setItem(SEARCH_KEY, evt.target.value);
    clearTimeout(searchRetry);
    runSearch(true);
  }
});

// initSearch puts the last query back into the field and runs it. Nothing
// re-renders the field, but a reload starts it empty, and a search the user
// never cleared is one they are still working with.
function initSearch() {
  var input = document.getElementById('search-input');
  if (!input) {
    return;
  }
  input.value = localStorage.getItem(SEARCH_KEY) || '';
  setSearchCased(localStorage.getItem(SEARCH_CASE_KEY) === '1');
  if (input.value) {
    runSearch();
  }
}

// Render log
//
// The log panel polls /render/status every second and htmx replaces the
// whole <pre> with the log as it now stands, so the element the user was
// looking at is gone after every poll and the fresh one starts scrolled to
// the top. The panel therefore follows its newest line by itself. It stops
// following as soon as the user scrolls up -- reading the middle of a long
// log is why anyone scrolls up -- and follows again once they scroll back
// to the bottom, or start another render.

var renderLogFollow = true;

// renderLogTop is where the log stood just before the last swap, so a
// panel the user scrolled up in can be put back where they left it.
var renderLogTop = 0;

function renderLogOutput() {
  return document.querySelector('#render-log .render-output');
}

// atBottom allows a pixel of slack: fractional line heights keep scrollTop
// from ever reaching scrollHeight - clientHeight exactly.
function atBottom(el) {
  return el.scrollHeight - el.scrollTop - el.clientHeight <= 2;
}

// Scroll events do not bubble, and the element they come from is replaced
// every second, so the log is listened to on the way down instead.
document.addEventListener('scroll', function (evt) {
  var el = evt.target;
  if (el && el.classList && el.classList.contains('render-output')) {
    renderLogFollow = atBottom(el);
  }
}, true);

document.body.addEventListener('htmx:beforeSwap', function (evt) {
  var id = evt.detail && evt.detail.target && evt.detail.target.id;
  if (id === 'content') {
    stashSuggestions(); // the pane, and the run in its copyedit pane, is about to go
  }
  if (id === 'main') {
    forgetSuggestions();
  }
  if (id === 'render-log') {
    var out = renderLogOutput();
    renderLogTop = out ? out.scrollTop : 0;
  }
});

document.addEventListener('DOMContentLoaded', function () {
  bumpMediaVersion(); // before the first preview, which builds image URLs
  loadCollapsed();
  initTree();
  initDivider('divider', 'tree-pane', 'treePaneWidth', 'left');
  // The editor pane comes with the page when a project has a page to
  // restore, so it needs the same setup a swapped-in one gets.
  syncCurrentPath();
  applySelection();
  revealSelection();
  initEditor();
  restoreSuggestions();
  applyPreview();
  refreshEditor();
  initSearch();
});

// The Git panel's filter. Every operation swaps the whole panel body, the
// field included, so the text lives here and is put back after each swap;
// a filter that emptied itself whenever a file was staged would have to
// be typed again for the next one.
var gitFilter = '';

// applyGitFilter hides the rows of both lists whose path does not contain
// the filter text, ignoring case, and says so in a list left with none.
function applyGitFilter() {
  var needle = gitFilter.trim().toLowerCase();
  document.querySelectorAll('#git-body .git-group').forEach(function (group) {
    var shown = 0;
    group.querySelectorAll('.git-file').forEach(function (row) {
      var match = !needle || (row.dataset.path || '').toLowerCase().indexOf(needle) >= 0;
      row.hidden = !match;
      if (match) {
        shown++;
      }
    });
    var none = group.querySelector('.git-nomatch');
    if (none) {
      none.hidden = shown > 0;
    }
  });
}

document.body.addEventListener('input', function (evt) {
  if (evt.target.id === 'git-filter') {
    gitFilter = evt.target.value;
    applyGitFilter();
  }
});

document.body.addEventListener('htmx:afterSwap', function (evt) {
  var target = evt.detail && evt.detail.target;
  if (target && target.id === 'git-body') {
    var field = document.getElementById('git-filter');
    if (field) {
      field.value = gitFilter;
    }
    applyGitFilter();
  }
});

document.body.addEventListener('htmx:afterSwap', function (evt) {
  var id = evt.detail && evt.detail.target && evt.detail.target.id;
  if (id === 'main') {
    // /open replaces both panes at once: the tree of the project just
    // opened, and the editor holding the page that project last had open.
    syncCurrentPath();
  }
  if (id === 'tree' || id === 'main') { // /open swaps #main, everything else #tree
    initTree();
    applySelection();
    refreshHits(); // the tree was rebuilt; the current search still stands
  }
  if (id === 'main') {
    revealSelection();
  }
  if (id === 'search-results') {
    readHits();
  }
  if (id === 'content') { // track whatever the editor now shows
    syncCurrentPath();
    applySelection();
    // A fresh pane holds the page as it is on disk, so whatever the last
    // one failed to save is no longer this editor's problem.
    saveFailed = false;
    rebaseNotice = '';
    setSaveStatus('');
    showOverwrite(false);
  }
  if (id === 'content' || id === 'main') {
    initEditor(); // mount before the preview reads the editor's text
    restoreSuggestions(); // and before the kept marks are painted into it
    applyPreview();
  }
  if (id === 'copyedit-body') {
    // Either the suggestions of a run just arrived, or the task list came
    // back; both are answered by marking exactly what is listed now.
    readSuggestions();
    applyTaskSelection();
  }
  if (id === 'render-log') {
    var out = renderLogOutput();
    if (out) {
      out.scrollTop = renderLogFollow ? out.scrollHeight : renderLogTop;
    }
  }
});

// The top-bar "＋ Page" form inserts the new page after the one selected in
// the tree; carry that selection along as the "after" parameter.
document.body.addEventListener('htmx:configRequest', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  if (elt && elt.classList && elt.classList.contains('new-file-form')) {
    evt.detail.parameters.after = currentPath || '';
  }
  // The search field posts its query as it is typed; the case button rides
  // along, as it does with every other search request.
  if (elt && elt.id === 'search-input' && searchCased()) {
    evt.detail.parameters['case'] = '1';
  }
  // The Open field posts the label it shows; the project it stands for is
  // the full path behind it, which is what the server has to be given.
  if (elt && elt.classList && elt.classList.contains('open-form')) {
    var input = openPathInput();
    if (input && input.dataset.full) {
      evt.detail.parameters.path = input.dataset.full;
    }
  }
});

// /open replaces the divider along with the panes. Re-init only after htmx
// has settled: settling restores the swapped-in attributes of elements whose
// id survived the swap, which would wipe a width set during afterSwap.
document.body.addEventListener('htmx:afterSettle', function (evt) {
  var id = evt.detail && evt.detail.target && evt.detail.target.id;
  if (id === 'main') {
    initDivider('divider', 'tree-pane', 'treePaneWidth', 'left');
  }
  if (id === 'content' || id === 'main') { // the editor/pane split comes with the editor
    applyPreview();
    refreshEditor(); // the saved pane widths have just been applied
  }
});

// Out-of-band swaps (e.g. /save refreshing #tree alongside #content) fire
// htmx:oobAfterSwap instead of htmx:afterSwap, so Sortable needs its own hook.
document.body.addEventListener('htmx:oobAfterSwap', function (evt) {
  var id = evt.detail && evt.detail.target && evt.detail.target.id;
  if (id === 'tree') {
    initTree();
    applySelection();
    refreshHits();
  }
  if (id === 'content-path') {
    // A move or a rename renamed the open page's file; the editor now posts
    // to the new path, so the tree entry to keep selected is the new one
    // too, and the copyedit suggestions kept for the page go along with it.
    var before = currentPath;
    syncCurrentPath();
    moveStash(before, currentPath);
    applySelection();
    // An autosave that the rename had already broken is retried at the new
    // path right away -- its text exists nowhere else. A save that was
    // fine is left alone: re-posting it would put the editor's text, which
    // still carries the frontmatter from before the move, back over what
    // the move just wrote.
    if (saveFailed) {
      saveNow();
    }
  }
});

document.body.addEventListener('click', function (evt) {
  // A new render starts a new log; follow it, whatever the last one was
  // left scrolled to. htmx posts the button itself, so this only marks it.
  if (evt.target.closest('#render-run')) {
    renderLogFollow = true;
  }

  // Reload asks the server for the page's text again; its images are the
  // browser's to fetch, and a renewed stamp is what makes it fetch them
  // instead of showing what it cached. htmx carries out the request
  // itself -- this only marks the images as due.
  if (evt.target.closest('#content-reload')) {
    bumpMediaVersion();
  }

  // The buttons below the preview: one page back, one page on.
  var flip = evt.target.closest('#page-prev, #page-next');
  if (flip) {
    flipPage(flip.id === 'page-prev' ? -1 : 1);
    return;
  }

  // The pencil after the path in the editor's heading.
  var renameButton = evt.target.closest('#content-title .rename-file');
  if (renameButton) {
    startRename(renameButton);
    return;
  }

  // Overwrite: the user answering a refused save with "mine wins".
  if (evt.target.closest('#save-overwrite')) {
    showOverwrite(false);
    saveNow(true);
    return;
  }

  // Apply and Done sit inside the entry, so they are asked about before
  // the entry's own click.
  var apply = evt.target.closest('.copyedit-apply');
  if (apply) {
    applySuggestion(apply.closest('.copyedit-suggestion'));
    return;
  }

  var finish = evt.target.closest('.copyedit-done');
  if (finish) {
    finishSuggestion(finish.closest('.copyedit-suggestion'));
    return;
  }

  // A filter chip: narrow the run to one task's findings.
  var chip = evt.target.closest('.copyedit-filter');
  if (chip) {
    filterSuggestions(chip.dataset.task);
    return;
  }

  // A suggestion: show which one is being read, and bring the passage it
  // is about into view.
  var suggestion = evt.target.closest('.copyedit-suggestion');
  if (suggestion) {
    selectSuggestion(suggestion);
    return;
  }

  // The Preview, Copyedit, and Help buttons: show or hide that pane.
  var toggle = evt.target.closest('.pane-toggle');
  if (toggle) {
    togglePane(toggle.dataset.pane);
    return;
  }

  // Vim toggle: switch the editor's keymap.
  if (evt.target.closest('#vim-toggle')) {
    toggleVim();
    return;
  }

  // A path in the Git panel: htmx fetches the diff into the pane; the
  // mark is what says whose diff is standing there, since the pane keeps
  // showing it while the user reads on down the list.
  var gitPath = evt.target.closest('.git-path');
  if (gitPath) {
    document.querySelectorAll('#git-body .git-path.selected').forEach(function (el) {
      el.classList.remove('selected');
    });
    gitPath.classList.add('selected');
    return;
  }

  // Close the diff, leaving the file lists as they are: nothing has to be
  // asked of the server for that, so nothing is.
  if (evt.target.closest('.git-diff-close')) {
    var pane = document.getElementById('git-diff');
    if (pane) {
      pane.innerHTML = '';
    }
    document.querySelectorAll('#git-body .git-path.selected').forEach(function (el) {
      el.classList.remove('selected');
    });
    return;
  }

  // "Open in editor": htmx puts the file in the editor pane, which the
  // popup covers, so the popup closes; the tree marks the page as the one
  // open, the way clicking it there would have.
  var openInEditor = evt.target.closest('.git-diff-open');
  if (openInEditor) {
    var details = document.getElementById('git-details');
    if (details) {
      details.open = false;
    }
    currentPath = openInEditor.dataset.path || null;
    applySelection();
    return;
  }

  // Expand all: forget every collapsed branch and reveal each subtree.
  if (evt.target.closest('#expand-all')) {
    collapsed.clear();
    document.querySelectorAll('#tree li.page.has-children').forEach(function (li) {
      li.classList.remove('collapsed');
    });
    saveCollapsed();
    return;
  }

  // Collapse all: remember every branch as collapsed and hide each subtree.
  if (evt.target.closest('#collapse-all')) {
    document.querySelectorAll('#tree li.page.has-children').forEach(function (li) {
      collapsed.add(li.dataset.key);
      li.classList.add('collapsed');
    });
    saveCollapsed();
    return;
  }

  var toggle = evt.target.closest('.toggle');
  if (toggle) {
    var node = toggle.closest('li.page');
    if (node && node.classList.contains('has-children')) {
      var key = node.dataset.key;
      if (collapsed.has(key)) {
        collapsed.delete(key);
      } else {
        collapsed.add(key);
      }
      node.classList.toggle('collapsed', collapsed.has(key));
      saveCollapsed();
    }
    return;
  }

  var link = evt.target.closest('a.title');
  if (!link) {
    return;
  }
  var item = link.closest('li');
  currentPath = item ? item.dataset.path : null;
  applySelection();
});

// Autosave feedback: the edit form posts /save with hx-swap="none", so the
// only visible trace is the status text next to the heading.
//
// A failed autosave is the one status that must not read as a detail: the
// text in the editor is then the only copy of the edit, and it is thrown
// away with the pane as soon as another page is opened. So a failure is
// shown as an error rather than as a note, and is remembered in saveFailed,
// which is what guards leaving the page below.
var saveFailed = false;

// rebaseNotice is what the last save had to put back: the server sends it
// as a qm:rebased event, which htmx fires before the request finishes, so
// it is held here until there is a "Saved" to say it alongside.
var rebaseNotice = '';

function setSaveStatus(text, kind) {
  var el = document.getElementById('save-status');
  if (el) {
    el.textContent = text;
    el.classList.toggle('failed', kind === 'failed');
    el.classList.toggle('notice', kind === 'notice');
  }
}

// showOverwrite offers, or withdraws, the way past a refused save.
function showOverwrite(on) {
  var button = document.getElementById('save-overwrite');
  if (button) {
    button.hidden = !on;
  }
}

// saveNow writes the editor's text without waiting out the edit form's
// one-second autosave delay. It is sourced from the form so that it reports
// through the same status as an ordinary autosave. force says the user
// answered a conflict with "mine wins".
function saveNow(force) {
  var form = document.querySelector('#content .edit-form');
  var path = document.getElementById('content-path');
  var area = document.querySelector('#content textarea.file-content');
  if (!form || !path || !area) {
    return;
  }
  var values = { path: path.value, body: area.value };
  if (force) {
    values.force = '1';
  }
  htmx.ajax('POST', '/save', { source: form, swap: 'none', values: values });
}

// File renaming
//
// The pencil after the path in the editor's heading turns the page's name,
// and only that, into a field; the rest of the path stays text, since a
// rename never moves a page out of its directory. A page's name is its
// file name, except for a section page -- name/index.qmd, which is how
// pages are created -- whose name is its directory's: there the field
// stands in for the directory and /index.qmd stays text after it. Enter
// renames the page; Esc, or
// leaving the field, gives up and puts the path back as it was. The server
// re-points the editor at the new path the way a move does, and answers a
// name it cannot take -- one already taken above all -- with the tree's
// error, which also opens the field again on the name typed, so it can be
// corrected rather than typed anew.
function startRename(button, typed, problem) {
  var span = button.parentElement.querySelector('.path');
  if (!span || span.querySelector('input')) {
    return;
  }
  var full = span.dataset.path;
  var tail = /\/index\.qmd$/.test(full) ? '/index.qmd' : '';
  var head = full.slice(0, full.length - tail.length);
  var cut = head.lastIndexOf('/') + 1;
  var name = head.slice(cut);
  var input = document.createElement('input');
  input.type = 'text';
  input.className = 'rename-input';
  input.value = typed || name;
  input.spellcheck = false;
  input.setAttribute('aria-label', tail ? 'Folder name' : 'File name');
  input.size = Math.max(input.value.length + 2, 12);
  if (problem) {
    input.classList.add('invalid');
    input.title = problem;
  }
  span.textContent = head.slice(0, cut);
  span.appendChild(input);
  span.appendChild(document.createTextNode(tail));
  button.hidden = true;
  input.focus();
  // The name without its extension is selected, so typing replaces the
  // name and leaves .qmd in place.
  var stem = input.value.lastIndexOf('.qmd');
  input.setSelectionRange(0, stem > 0 ? stem : input.value.length);

  var done = false;
  function finish(commit) {
    if (done) {
      return;
    }
    done = true;
    var next = input.value.trim();
    span.textContent = full;
    button.hidden = false;
    if (!commit || next === '' || next === name) {
      return;
    }
    htmx.ajax('POST', '/rename', {
      target: '#tree',
      swap: 'innerHTML',
      values: { path: full, name: next, open: currentPath || '' }
    }).then(function () {
      var error = document.querySelector('#tree > .error');
      var again = document.querySelector('#content-title .rename-file');
      if (error && again && editorPagePath() === full) {
        startRename(again, next, error.textContent);
      }
    });
  }
  input.addEventListener('keydown', function (e) {
    if (e.key === 'Enter') {
      e.preventDefault();
      finish(true);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      finish(false);
    }
  });
  input.addEventListener('input', function () {
    input.size = Math.max(input.value.length + 2, 12);
    input.classList.remove('invalid');
  });
  input.addEventListener('blur', function () {
    finish(false);
  });
}

// The page the editor holds was written on top of the page as it stood when
// it opened, and the tree may have written to the same file since -- a move
// or a create renumbers a sibling group, and a move to another depth shifts
// the headings. The server replays the edit onto those rather than let it
// undo them, and says so here: the editor still shows the text from before,
// and only a reload brings it in line.
document.body.addEventListener('qm:rebased', function (evt) {
  var d = evt.detail || {};
  rebaseNotice = d.headings
    ? ' — the move\'s heading levels were kept; ↻ Reload to see them'
    : ' — the move\'s numbering was kept; ↻ Reload to see it';
});

document.body.addEventListener('htmx:beforeRequest', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  if (elt && elt.classList && elt.classList.contains('edit-form')) {
    setSaveStatus('Saving…');
  }
});

document.body.addEventListener('htmx:afterRequest', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  if (!elt || !elt.classList || !elt.classList.contains('edit-form')) {
    return;
  }
  var notice = rebaseNotice;
  rebaseNotice = '';
  saveFailed = !evt.detail.successful;
  if (!saveFailed) {
    showOverwrite(false);
    setSaveStatus('Saved' + notice, notice ? 'notice' : '');
    return;
  }
  var xhr = evt.detail.xhr;
  // A conflict is not a broken save but a decision to make: somebody else
  // wrote the file, and only the user can say whose version wins.
  if (xhr && xhr.status === 409) {
    showOverwrite(true);
    setSaveStatus('NOT SAVED — the file changed on disk since this page was opened. Reload to take that version, or Overwrite to keep yours.', 'failed');
    return;
  }
  var why = (xhr && (xhr.responseText || xhr.statusText) || '').trim();
  setSaveStatus('NOT SAVED' + (why ? ' — ' + why : '') + ' — your edits are only in this editor', 'failed');
});

// Opening another page, or reloading this one from disk, replaces the
// editor and with it the only copy of an edit that could not be saved. Ask
// first rather than drop it silently.
document.body.addEventListener('htmx:confirm', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  var target = elt && elt.getAttribute && elt.getAttribute('hx-target');
  if (!saveFailed || target !== '#content') {
    return;
  }
  evt.preventDefault();
  if (window.confirm('This page has edits that could not be saved. Replacing the editor discards them. Continue?')) {
    evt.detail.issueRequest(true);
  }
});

// The same for a reload or a closed tab, which htmx never sees.
// The Render and Git popups hang below their own buttons. One that would
// reach past the window's right edge is pulled back to the left by as much,
// so it opens under its button where it can and inside the window always.
function fitPopup(details) {
  var body = details.querySelector(':scope > .render-body, :scope > .git-body');
  if (!body) {
    return;
  }
  body.style.left = '';
  var margin = 8;
  var rect = body.getBoundingClientRect();
  var over = rect.right - (document.documentElement.clientWidth - margin);
  if (over > 0) {
    body.style.left = -Math.min(over, rect.left - margin) + 'px';
  }
}

// toggle does not bubble, so it is listened for on the way down.
document.addEventListener('toggle', function (evt) {
  var details = evt.target;
  if (details.open && details.matches && details.matches('details.render, details.git')) {
    fitPopup(details);
  }
}, true);

// Leaving for the config pages reloads the app when coming back; the run in
// the copyedit pane is kept for that as for any other page switch.
window.addEventListener('pagehide', stashSuggestions);

window.addEventListener('beforeunload', function (evt) {
  if (saveFailed) {
    evt.preventDefault();
    evt.returnValue = '';
  }
});
