// Take a photo, shrink it in the browser, attach the location if allowed, and upload it.
(function () {
  var MAX_SIDE = 1280;
  var MAX_BYTES = 500 * 1024;

  var input = document.getElementById('photo-input');
  var button = document.getElementById('photo-button');
  if (!input || !button) return;

  function csrfToken() {
    try { return JSON.parse(document.body.getAttribute('hx-headers'))['X-CSRF-Token']; } catch (e) { return ''; }
  }

  function toBlob(canvas, quality) {
    return new Promise(function (resolve) { canvas.toBlob(resolve, 'image/jpeg', quality); });
  }

  async function shrink(file) {
    var bmp = await createImageBitmap(file);
    var scale = Math.min(1, MAX_SIDE / Math.max(bmp.width, bmp.height));
    var canvas = document.createElement('canvas');
    canvas.width = Math.round(bmp.width * scale);
    canvas.height = Math.round(bmp.height * scale);
    canvas.getContext('2d').drawImage(bmp, 0, 0, canvas.width, canvas.height);
    if (bmp.close) bmp.close();
    var blob = await toBlob(canvas, 0.7);
    if (blob && blob.size > MAX_BYTES) blob = await toBlob(canvas, 0.5);
    return blob;
  }

  // The location is a bonus: it must never block or delay the report for long.
  function locate() {
    return new Promise(function (resolve) {
      if (!navigator.geolocation) return resolve(null);
      navigator.geolocation.getCurrentPosition(
        function (p) { resolve(p.coords); },
        function () { resolve(null); },
        { timeout: 5000, maximumAge: 60000 }
      );
    });
  }

  function say(msg, ok) {
    if (window.patrolToast) window.patrolToast(msg, ok);
  }

  input.addEventListener('change', async function () {
    var file = input.files && input.files[0];
    if (!file) return;
    var label = button.textContent;
    button.disabled = true;
    button.textContent = 'Mengunggah…';
    try {
      var results = await Promise.all([shrink(file), locate()]);
      var blob = results[0], coords = results[1];
      if (!blob) throw new Error('resize');

      var fd = new FormData();
      fd.append('photo', blob, 'photo.jpg');
      if (coords) {
        fd.append('lat', coords.latitude);
        fd.append('lng', coords.longitude);
      }
      var res = await fetch('/patrol/tonight/photos', {
        method: 'POST',
        body: fd,
        headers: { 'X-CSRF-Token': csrfToken(), 'HX-Request': 'true' },
        credentials: 'same-origin'
      });
      if (res.status === 409) { window.location.reload(); return; }
      var html = await res.text();
      if (!res.ok) { say(html.length < 200 ? html : 'Gagal mengunggah foto, coba lagi.'); return; }

      var list = document.getElementById('photo-list');
      list.outerHTML = html;
      htmx.process(document.getElementById('photo-list'));
      say('Foto terunggah', true);
    } catch (e) {
      say('Gagal mengunggah foto, coba lagi.');
    } finally {
      input.value = ''; // so the next photo triggers "change" again
      button.disabled = false;
      button.textContent = label;
    }
  });
})();
