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

  // Reload when the window closes so the lock modal appears. Server-sent seconds and a
  // deadline check (not one long setTimeout), so a wrong or sleeping phone clock still works.
  var reload = document.querySelector('[data-reload-in]');
  if (reload) {
    var deadline = Date.now() + (parseInt(reload.dataset.reloadIn, 10) || 0) * 1000;
    var check = setInterval(function () {
      if (Date.now() < deadline) return;
      clearInterval(check);
      // A small margin so the server is past the closing time too.
      setTimeout(function () { location.reload(); }, 1500);
    }, 1000);
  }
})();
