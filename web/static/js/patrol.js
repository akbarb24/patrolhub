// Feedback for the report page: a short toast for saves and errors, undoing optimistic checkbox changes on error.
(function () {
  var timer;

  function toast(msg, ok) {
    var el = document.getElementById('toast');
    if (!el) return;
    el.textContent = msg;
    if (ok) {
      var check = document.createElement('span');
      check.setAttribute('aria-hidden', 'true');
      check.textContent = '✓ ';
      el.prepend(check);
    }
    el.classList.toggle('bg-error', !ok);
    el.classList.toggle('text-error-content', !ok);
    el.classList.toggle('bg-primary', !!ok);
    el.classList.toggle('text-primary-content', !!ok);
    el.classList.remove('hidden');
    clearTimeout(timer);
    timer = setTimeout(function () { el.classList.add('hidden'); }, ok ? 2500 : 6000);
  }
  window.patrolToast = toast;

  function revert(el) {
    if (el && el.type === 'checkbox') el.checked = !el.checked;
  }

  // Elements opt in with data-toast; the note autosave has none, so typing stays quiet.
  document.body.addEventListener('htmx:afterRequest', function (e) {
    var msg = e.detail.elt && e.detail.elt.dataset.toast;
    if (e.detail.successful && msg) toast(msg, true);
  });

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
