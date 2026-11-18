(() => {
  'use strict';
  const csrf = document.body.dataset.csrf;
  const feedback = document.getElementById('customer-feedback');
  function track(step, platform = '') {
    if (!csrf) return;
    const body = new URLSearchParams({csrf, step, platform});
    fetch('/account/journey', {method: 'POST', credentials: 'same-origin', body, keepalive: true}).catch(() => {});
  }
  document.querySelectorAll('[data-journey]').forEach(link => link.addEventListener('click', () => track(link.dataset.journey, link.dataset.platform)));
  document.querySelectorAll('[data-copy]').forEach(button => button.addEventListener('click', async () => {
    const input = document.getElementById(button.dataset.copy);
    if (!input) return;
    try {
      await navigator.clipboard.writeText(input.value);
      if (feedback) feedback.textContent = 'Ссылка скопирована';
      if (input.id === 'connection-url') track('manual_copy_requested', button.dataset.platform);
    } catch (_) {
      input.focus(); input.select();
      if (feedback) feedback.textContent = 'Выделили ссылку. Скопируйте её через меню устройства.';
    }
  }));
  document.querySelectorAll('form[data-confirm]').forEach(form => {
    form.addEventListener('submit', event => { if (!window.confirm(form.dataset.confirm)) event.preventDefault(); });
  });
  const suggestion = document.getElementById('platform-suggestion');
  if (suggestion) {
    const ua = navigator.userAgent || '';
    let platform = /android/i.test(ua) ? 'android' : /iphone|ipad|ipod/i.test(ua) || (/macintosh/i.test(ua) && navigator.maxTouchPoints > 1) ? 'ios' : /windows/i.test(ua) ? 'windows' : /macintosh|mac os/i.test(ua) ? 'macos' : '';
    const link = document.querySelector(`[data-platform-choice="${platform}"]`);
    if (link) { link.setAttribute('aria-describedby', 'platform-suggestion'); suggestion.textContent = `На этом устройстве, вероятно, ${link.textContent.replace('→', '').trim()}. Можно выбрать другое.`; }
  }
})();
