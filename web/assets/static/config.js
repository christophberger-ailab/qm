// Quarto Manager - configuration page behavior (plain ES6, no build step)

// initPreviewCSSEditor mounts CodeMirror on the custom preview stylesheet
// textarea. The textarea remains the form field, so submitting the form
// keeps working even if CodeMirror failed to load.
function initPreviewCSSEditor() {
  var area = document.querySelector('textarea.preview-css-content');
  if (!area || typeof CodeMirror === 'undefined') {
    return;
  }

  var editor = CodeMirror.fromTextArea(area, {
    mode: 'css',
    lineWrapping: true
  });

  if (area.form) {
    area.form.addEventListener('submit', function () {
      editor.save();
    });
  }
}

// A Delete button on a config page removes something the user wrote --
// an editing task, an API connection with its key -- and a form posts the
// moment it is clicked. The confirmation is what the page tree's own
// delete asks for through hx-confirm; these pages carry no htmx, so they
// ask for it here.
document.addEventListener('click', function (evt) {
  var button = evt.target.closest('button.danger');
  if (!button) {
    return;
  }
  var item = button.closest('.config-item');
  var name = item ? (item.querySelector('input[name="title"], input[name="name"]') || {}).value : '';
  if (!window.confirm('Delete ' + (name ? '"' + name + '"' : 'this entry') + '?')) {
    evt.preventDefault();
  }
});

// The base URL belongs to the two HTTP kinds and to neither the GitHub
// Copilot kind nor the user: Copilot is reached through the CLI, which
// knows where GitHub is. So the field follows the API dropdown rather
// than standing there taking a value that would be ignored. The server
// clears it either way; this only saves the user from filling it in.
function showBaseURLFor(select) {
  var form = select.closest('form');
  var field = form ? form.querySelector('.connection-base-url') : null;
  if (field) {
    field.hidden = select.value === 'copilot';
  }
}

document.addEventListener('change', function (evt) {
  if (evt.target.classList && evt.target.classList.contains('connection-kind')) {
    showBaseURLFor(evt.target);
  }
});

document.addEventListener('DOMContentLoaded', initPreviewCSSEditor);
document.addEventListener('DOMContentLoaded', function () {
  document.querySelectorAll('select.connection-kind').forEach(showBaseURLFor);
});
