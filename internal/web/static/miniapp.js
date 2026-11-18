(() => {
  'use strict';
  const app = window.Telegram && window.Telegram.WebApp;
  const form = document.getElementById('miniapp-auth');
  const status = document.getElementById('miniapp-status');
  const active = app && app.initData;
  if (active) {
    try {
      app.ready();
      app.expand();
      if (app.BackButton && !form) {
        if (['/account', '/account/devices', '/account/profile', '/support'].includes(window.location.pathname)) app.BackButton.hide();
        else { app.BackButton.show(); app.BackButton.onClick(() => {
          if (window.location.pathname === '/connect' && new URLSearchParams(window.location.search).has('platform')) {
            const params = new URLSearchParams(window.location.search);
            if (params.get('step') === 'finish') params.set('step', 'import');
            else params.delete('step');
            window.location.href = window.location.search.includes('step=') ? '/connect?' + params.toString() : '/connect';
          }
          else if (window.location.pathname.startsWith('/support/')) window.location.href = '/support';
          else window.location.href = '/account';
        }); }
      }
    } catch (_) { /* Native chrome is optional; authentication still works. */ }
    document.querySelectorAll('a[data-miniapp-external]').forEach(link => {
      link.addEventListener('click', event => {
        if (typeof app.openLink !== 'function') return;
        event.preventDefault();
        app.openLink(link.href);
      });
    });
  }
  if (!form) return;
  const button = form.querySelector('button');
  let busy = false;
  async function login(event) {
    if (event) event.preventDefault();
    if (busy) return;
    if (!active) {
      status.textContent = 'Откройте приложение кнопкой в меню бота или войдите по коду ниже.';
      button.hidden = true;
      return;
    }
    busy = true; button.disabled = true;
    status.textContent = 'Входим в кабинет…';
    try {
      const body = new URLSearchParams();
      body.set('csrf', form.elements.csrf.value);
      body.set('next', form.elements.next.value);
      body.set('init_data', app.initData);
      const response = await fetch('/auth/telegram', {method:'POST', credentials:'same-origin', body});
      if (!response.ok) throw new Error('auth');
      const result = await response.json();
      if (typeof result.next !== 'string' || !result.next.startsWith('/') || result.next.startsWith('//') || result.next.includes(String.fromCharCode(92)) || /[\x00-\x1f]/.test(result.next)) throw new Error('redirect');
      window.location.replace(result.next);
    } catch (_) {
      status.textContent = 'Не удалось войти. Закройте приложение и откройте его заново из меню бота или повторите вход.';
      busy = false; button.disabled = false;
    }
  }
  form.addEventListener('submit', login);
  login();
})();
