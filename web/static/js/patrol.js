// Report page: toast for errors, the submit checklist, walk-ins added in the browser,
// a draft kept in localStorage until "Kirim Laporan", and the reload at window close.
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

  document.body.addEventListener('htmx:responseError', function (e) {
    var xhr = e.detail.xhr;
    var text = (xhr.responseText || '').trim();
    var known = xhr.status >= 400 && xhr.status < 500 && text && text.length < 200;
    toast(known ? text : 'Gagal menyimpan, coba lagi.');
  });
  document.body.addEventListener('htmx:sendError', function () {
    toast('Gagal mengirim, periksa koneksi lalu coba lagi.');
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

  var form = document.getElementById('report-form');
  if (!form) {
    clearOldDrafts(null);
    return;
  }

  var note = document.getElementById('situation');
  var submit = document.getElementById('submit-button');
  var list = document.getElementById('team-list');
  var minPhotos = parseInt(form.dataset.minPhotos, 10) || 0;
  var draftKey = 'patrol-draft:' + form.dataset.night;

  // --- walk-ins -------------------------------------------------------------
  var picker = document.getElementById('walk-in-picker');
  var select = document.getElementById('walk-in-select');
  var openBtn = document.getElementById('walk-in-open');
  if (openBtn) {
    openBtn.addEventListener('click', function () {
      picker.hidden = false;
      openBtn.hidden = true;
      select.focus();
    });
    document.getElementById('walk-in-add').addEventListener('click', function () {
      if (!select.value) return;
      addWalkIn(select.value);
      saveDraft();
      toast('Anggota pengganti ditambahkan', true);
    });
  }

  function addWalkIn(id) {
    var opt = select && select.querySelector('option[value="' + CSS.escape(id) + '"]');
    if (!opt) return;
    var li = document.createElement('li');
    li.className = 'flex min-h-14 items-center justify-between gap-3 py-2';
    li.dataset.walkIn = id;
    var name = document.createElement('span');
    name.textContent = opt.textContent + ' ';
    var badge = document.createElement('span');
    badge.className = 'badge badge-warning badge-sm ml-1';
    badge.textContent = 'pengganti';
    name.append(badge);
    var input = document.createElement('input');
    input.type = 'hidden';
    input.name = 'walk_in';
    input.value = id;
    var remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'btn btn-ghost btn-sm';
    remove.textContent = 'Hapus';
    remove.setAttribute('aria-label', 'Hapus pengganti: ' + opt.textContent);
    remove.addEventListener('click', function () {
      li.remove();
      opt.hidden = false;
      opt.disabled = false;
      saveDraft();
    });
    li.append(name, input, remove);
    list.append(li);
    opt.hidden = true;
    opt.disabled = true;
    select.value = '';
    var empty = document.getElementById('team-empty');
    if (empty) empty.hidden = true;
  }

  // --- draft ----------------------------------------------------------------
  function readDraft() {
    try { return JSON.parse(localStorage.getItem(draftKey) || 'null'); } catch (e) { return null; }
  }
  function saveDraft() {
    var data = {
      note: note.value,
      present: Array.prototype.map.call(form.querySelectorAll('input[name="present"]:checked'), function (el) { return el.value; }),
      walkIns: Array.prototype.map.call(form.querySelectorAll('input[name="walk_in"]'), function (el) { return el.value; })
    };
    try { localStorage.setItem(draftKey, JSON.stringify(data)); } catch (e) { /* draft is a convenience */ }
  }
  function clearOldDrafts(keep) {
    try {
      for (var i = localStorage.length - 1; i >= 0; i--) {
        var k = localStorage.key(i);
        if (k && k.indexOf('patrol-draft:') === 0 && k !== keep) localStorage.removeItem(k);
      }
    } catch (e) { /* ignore */ }
  }

  clearOldDrafts(draftKey);
  var draft = readDraft();
  if (draft) {
    if (!note.value && draft.note) note.value = draft.note;
    (draft.walkIns || []).forEach(addWalkIn);
    (draft.present || []).forEach(function (id) {
      var box = form.querySelector('input[name="present"][value="' + CSS.escape(id) + '"]');
      if (box) box.checked = true;
    });
  }
  form.addEventListener('change', saveDraft);
  note.addEventListener('input', saveDraft);

  // --- submit checklist -----------------------------------------------------
  function photoCount() {
    var el = document.getElementById('photo-list');
    return el ? parseInt(el.dataset.count, 10) || 0 : 0;
  }
  function mark(name, ok) {
    var li = document.querySelector('#submit-checks [data-check="' + name + '"]');
    if (!li) return;
    li.classList.toggle('font-semibold', !ok);
    li.classList.toggle('text-base-content/70', ok);
    li.dataset.ok = ok ? '1' : '';
    var icon = li.querySelector('.check-icon');
    if (!icon) {
      icon = document.createElement('span');
      icon.className = 'check-icon';
      icon.setAttribute('aria-hidden', 'true');
      li.prepend(icon);
    }
    icon.textContent = ok ? '✓ ' : '○ ';
  }
  function refresh() {
    var photosOk = photoCount() >= minPhotos;
    var noteOk = note.value.trim() !== '';
    mark('photos', photosOk);
    mark('note', noteOk);
    submit.disabled = !(photosOk && noteOk);
  }
  note.addEventListener('input', refresh);
  document.addEventListener('patrol:photos-changed', refresh);
  refresh();

  // The draft goes once the server has accepted the report (it answers with HX-Redirect).
  form.addEventListener('htmx:afterRequest', function (e) {
    if (e.detail.successful) {
      try { localStorage.removeItem(draftKey); } catch (err) { /* ignore */ }
    }
  });
})();
