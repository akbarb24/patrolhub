// Error handling for the report page: show a short message and undo optimistic checkbox changes.
(function () {
  var timer;

  function toast(msg) {
    var el = document.getElementById('toast');
    if (!el) return;
    el.textContent = msg;
    el.classList.remove('hidden');
    clearTimeout(timer);
    timer = setTimeout(function () { el.classList.add('hidden'); }, 6000);
  }
  window.patrolToast = toast;

  function revert(el) {
    if (el && el.type === 'checkbox') el.checked = !el.checked;
  }

  document.body.addEventListener('htmx:responseError', function (e) {
    var xhr = e.detail.xhr;
    var text = (xhr.responseText || '').trim();
    var known = xhr.status >= 400 && xhr.status < 500 && text && text.length < 200;
    toast(known ? text : 'Gagal menyimpan, coba lagi.');
    revert(e.detail.elt);
  });

  document.body.addEventListener('htmx:sendError', function (e) {
    toast('Gagal menyimpan, coba lagi.');
    revert(e.detail.elt);
  });
})();
