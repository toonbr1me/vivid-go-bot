(() => {
  'use strict';
  const form = document.querySelector('.buy-form');
  const summary = document.getElementById('checkout-summary');
  if (!form || !summary) return;
  const submit = form.querySelector('button[type="submit"]');
  function update() {
    const plan = form.querySelector('input[name="plan"]:checked');
    const option = form.querySelector('input[name="option"]:checked');
    if (!plan || plan.disabled) { summary.textContent = 'Выберите доступный тариф'; if (submit) submit.disabled = true; return; }
    const d = plan.dataset;
    const amount = option?.value?.startsWith('stars.') && Number(d.summaryStars) > 0 ? `${d.summaryStars} Stars` : d.summaryAmount;
    summary.textContent = `К оплате: ${amount} · ${d.summaryDays} дней · ${d.summaryDevices} устройств · ориентировочно до ${d.summaryUntil}`;
    if (submit) submit.disabled = !option || option.disabled;
  }
  form.addEventListener('change', update);
  window.addEventListener('pageshow', update);
  form.addEventListener('submit', event => {
    if (form.dataset.submitting === 'true') { event.preventDefault(); return; }
    form.dataset.submitting = 'true';
    if (submit) { submit.disabled = true; submit.textContent = 'Открываем оплату…'; }
  });
  window.addEventListener('pageshow', () => { delete form.dataset.submitting; update(); });
  update();
})();
