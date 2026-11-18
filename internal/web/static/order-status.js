(() => {
  'use strict';
  const card = document.querySelector('[data-order-status]');
  if (!card) return;
  const label = card.querySelector('[data-status-label]');
  const detail = card.querySelector('[data-status-detail]');
  const next = card.querySelector('[data-status-next]');
  const refresh = card.querySelector('[data-status-refresh]');
  const feedback = card.querySelector('[data-status-feedback]');
  let timer, busy = false, attempts = 0, finished = false;
  const safeNext = new Set(['/connect', '/gifts', '/account/devices', '/account/profile']);
  async function update(manual = false) {
    if (busy || document.hidden || (finished && !manual)) return;
    clearTimeout(timer); busy = true;
    if (refresh) refresh.disabled = true;
    try {
      const response = await fetch(card.dataset.orderStatus, {credentials: 'same-origin', cache: 'no-store'});
      if (!response.ok) throw new Error(String(response.status));
      const state = await response.json();
      if (typeof state.label !== 'string' || typeof state.detail !== 'string' || typeof state.ready !== 'boolean' || typeof state.poll !== 'boolean') throw new Error('invalid state');
      if (label) label.textContent = state.label;
      const fact = card.querySelector('[data-status-fact]');
      if (fact) fact.textContent = state.label;
      const pay = card.querySelector('[data-status-pay]');
      if (pay) pay.hidden = state.can_pay !== true;
      if (detail) detail.textContent = state.detail;
      if (next) { next.hidden = !state.ready || !safeNext.has(state.next); if (!next.hidden) next.href = state.next; }
      finished = !state.poll;
      feedback.textContent = state.refresh_error ? 'Последний сохранённый статус. Не удалось обновить данные, повторим позже.' : state.ready ? 'Готово. Можно продолжить.' : state.poll ? 'Проверяем автоматически. Повторно платить не нужно, если вы уже оплатили.' : 'Статус обновлён';
    } catch (error) {
      feedback.textContent = error.message === '401' ? 'Войдите в кабинет, чтобы проверить заказ' : 'Не удалось обновить статус. Повторите проверку позже.';
      if (['401', '403', '404'].includes(error.message)) finished = true;
    } finally {
      busy = false; attempts++;
      if (refresh) refresh.disabled = false;
      if (!finished && attempts < 24) timer = setTimeout(() => update(), Math.min(30000, 4000 + attempts * 2000));
      else if (!finished) feedback.textContent = 'Автопроверка приостановлена. Нажмите «Проверить статус» или вернитесь позже.';
    }
  }
  if (refresh) refresh.addEventListener('click', () => { attempts = 0; update(true); });
  document.addEventListener('visibilitychange', () => { if (document.hidden) clearTimeout(timer); else { attempts = 0; update(); } });
  window.addEventListener('pageshow', () => update());
  update();
})();
