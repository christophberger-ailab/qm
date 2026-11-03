(function () {
  'use strict';
  var doc = document.documentElement;

  function toggle(btn, cls) {
    btn.addEventListener('click', function () {
      var open = doc.classList.toggle(cls);
      btn.setAttribute('aria-expanded', open ? 'true' : 'false');
    });
  }

  var sidebarBtn = document.querySelector('.sidebar-toggle');
  var menuBtn = document.querySelector('.menu-toggle');
  if (sidebarBtn) toggle(sidebarBtn, 'sidebar-open');
  if (menuBtn) toggle(menuBtn, 'menu-open');

  function closeAll() {
    doc.classList.remove('sidebar-open', 'menu-open');
    if (sidebarBtn) sidebarBtn.setAttribute('aria-expanded', 'false');
    if (menuBtn) menuBtn.setAttribute('aria-expanded', 'false');
  }
  var scrim = document.querySelector('.scrim');
  if (scrim) scrim.addEventListener('click', closeAll);
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeAll();
  });
  var sidebar = document.getElementById('sidebar');
  if (sidebar) sidebar.addEventListener('click', function (e) {
    if (e.target.closest && e.target.closest('a')) closeAll();
  });

  // Keep the current page visible in the scrolled sidebar.
  var current = document.querySelector('.sidebar [aria-current="page"]');
  if (current && sidebar) {
    var r = current.getBoundingClientRect(), s = sidebar.getBoundingClientRect();
    if (r.top < s.top || r.bottom > s.bottom) {
      sidebar.scrollTop += r.top - s.top - s.height / 3;
    }
  }

  // Scroll spy for the table of contents.
  var links = document.querySelectorAll('.toc a[href^="#"]');
  if (!links.length || !('IntersectionObserver' in window)) return;
  var byId = {}, targets = [];
  links.forEach(function (a) {
    var id = decodeURIComponent(a.getAttribute('href').slice(1));
    var el = document.getElementById(id);
    if (el) { byId[id] = a; targets.push(el); }
  });
  var visible = {};
  function update() {
    var active = null;
    for (var i = 0; i < targets.length; i++) {
      if (visible[targets[i].id]) { active = targets[i].id; break; }
    }
    if (active === null) return;
    links.forEach(function (a) { a.classList.remove('active'); a.removeAttribute('aria-current'); });
    var a = byId[active];
    if (a) { a.classList.add('active'); a.setAttribute('aria-current', 'location'); }
  }
  var io = new IntersectionObserver(function (entries) {
    entries.forEach(function (e) { visible[e.target.id] = e.isIntersecting; });
    update();
  }, { rootMargin: '-10% 0px -70% 0px' });
  targets.forEach(function (t) { io.observe(t); });
  // Highlight the first entry until something scrolls into view.
  if (targets.length) { visible[targets[0].id] = true; update(); visible[targets[0].id] = false; }
})();
