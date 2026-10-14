// "Salin rekap": fetch the WhatsApp text from the server and put it on the clipboard.
(function () {
  var menu = document.getElementById('copy-menu');
  var status = document.getElementById('copy-status');
  if (!menu) return;

  menu.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-copy]');
    if (!btn) return;
    menu.removeAttribute('open');
    fetch(btn.dataset.base + '?range=' + encodeURIComponent(btn.dataset.copy), { credentials: 'same-origin' })
      .then(function (r) { if (!r.ok) throw new Error(r.status); return r.text(); })
      .then(function (text) { return navigator.clipboard.writeText(text); })
      .then(function () {
        status.classList.remove('hidden');
        setTimeout(function () { status.classList.add('hidden'); }, 3000);
      })
      .catch(function () { alert('Gagal menyalin rekap.'); });
  });
})();
