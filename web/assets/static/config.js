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

document.addEventListener('DOMContentLoaded', initPreviewCSSEditor);
