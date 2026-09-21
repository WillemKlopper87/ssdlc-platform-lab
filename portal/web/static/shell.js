// Ctrl+K jump palette. Progressive enhancement: the sidebar links work
// without it. Items are read from the page itself (elements marked
// data-pal-item), and results are built with textContent, never innerHTML.
(function () {
  var bg = document.getElementById('pal-bg');
  var input = document.getElementById('pal-input');
  var list = document.getElementById('pal-list');
  if (!bg || !input || !list) return;

  var items = [].slice.call(document.querySelectorAll('[data-pal-item]')).map(function (a) {
    var label = a.getAttribute('data-pal-label') || a.textContent.replace(/↗/g, '').replace(/\d+$/, '').trim();
    return { label: label, sub: a.getAttribute('data-pal-sub') || '', href: a.getAttribute('href'), ext: a.target === '_blank' };
  });
  var sel = 0;
  var shown = [];

  function render() {
    var q = input.value.toLowerCase();
    shown = items.filter(function (i) { return !q || (i.label + ' ' + i.sub).toLowerCase().indexOf(q) > -1; }).slice(0, 9);
    if (sel >= shown.length) sel = Math.max(0, shown.length - 1);
    list.innerHTML = '';
    shown.forEach(function (i, n) {
      var a = document.createElement('a');
      a.className = 'ssdlc-pal-item' + (n === sel ? ' on' : '');
      a.href = i.href;
      if (i.ext) { a.target = '_blank'; a.rel = 'noopener'; }
      a.textContent = i.label;
      var s = document.createElement('span');
      s.textContent = i.sub;
      a.appendChild(s);
      list.appendChild(a);
    });
  }
  function open() { bg.classList.add('on'); input.value = ''; sel = 0; render(); input.focus(); }
  function close() { bg.classList.remove('on'); }

  document.addEventListener('keydown', function (e) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); open(); return; }
    if (!bg.classList.contains('on')) return;
    if (e.key === 'Escape') { close(); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); sel = Math.min(sel + 1, shown.length - 1); render(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); sel = Math.max(sel - 1, 0); render(); }
    else if (e.key === 'Enter' && list.children[sel]) { e.preventDefault(); list.children[sel].click(); }
  });
  input.addEventListener('input', function () { sel = 0; render(); });
  bg.addEventListener('mousedown', function (e) { if (e.target === bg) close(); });
  var btn = document.getElementById('pal-open');
  if (btn) btn.addEventListener('click', open);
})();
