// Lock modal on the report page: count down to the next report window and reload
// when it opens. The server sends the seconds left, so a wrong phone clock does not matter.
(function () {
  var el = document.querySelector('[data-countdown]');
  if (!el) return;
  var deadline = Date.now() + (parseInt(el.dataset.countdown, 10) || 0) * 1000;

  function pad(n) { return (n < 10 ? '0' : '') + n; }

  function tick() {
    var left = Math.ceil((deadline - Date.now()) / 1000);
    if (left <= 0) {
      clearInterval(timer);
      // A small margin so the server is past the opening time too.
      setTimeout(function () { location.reload(); }, 1500);
      left = 0;
    }
    el.textContent = pad(Math.floor(left / 3600)) + ':' + pad(Math.floor(left / 60) % 60) + ':' + pad(left % 60);
  }

  var timer = setInterval(tick, 1000);
  tick();
})();
