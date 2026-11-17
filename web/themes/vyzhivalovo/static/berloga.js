(function () {
  'use strict';
  const root = document.documentElement;
  const key = 'berloga-color-scheme';
  let mode = 'dark';
  try { const saved = localStorage.getItem(key); if (saved === 'light' || saved === 'dark') mode = saved; } catch (_) {}
  root.dataset.berlogaTheme = mode;
  document.querySelectorAll('.nav a').forEach(link => {
    if (new URL(link.href, location.href).pathname === location.pathname) link.setAttribute('aria-current', 'page');
  });
  const mount = document.querySelector('.header-actions') || document.querySelector('.admin-topbar > div');
  if (!mount) return;
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'theme-toggle';
  button.innerHTML = '<span aria-hidden="true">◐</span><span class="theme-toggle-label"></span>';
  const label = button.querySelector('.theme-toggle-label');
  function paint() {
    const next = mode === 'dark' ? 'Светлая тема' : 'Тёмная тема';
    label.textContent = next;
    button.setAttribute('aria-label', next);
    button.title = next;
    button.setAttribute('aria-pressed', String(mode === 'light'));
  }
  button.addEventListener('click', () => {
    mode = mode === 'dark' ? 'light' : 'dark';
    root.dataset.berlogaTheme = mode;
    try { localStorage.setItem(key, mode); } catch (_) {}
    paint();
  });
  mount.prepend(button);
  paint();
}());
