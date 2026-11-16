(function () {
  'use strict';
  const form = document.querySelector('.buy-form');
  const recurring = document.getElementById('checkout-recurring');
  if (!form || !recurring) return;
  const terms = document.getElementById('recurring-terms');
  const submit = form.querySelector('button[type="submit"]');
  const options = Array.from(form.querySelectorAll('input[name="option"]'));
  function update() {
    const plan = form.querySelector('input[name="plan"]:checked');
    const supported = (plan?.dataset.recurringOptions || '').split(',').filter(Boolean);
    recurring.disabled = !supported.length;
    if (recurring.disabled) recurring.checked = false;
    for (const option of options) {
      const allowed = !recurring.checked || supported.includes(option.value);
      option.disabled = !allowed;
      option.closest('label').hidden = !allowed;
      if (!allowed) option.checked = false;
    }
    if (!options.some(option => option.checked && !option.disabled)) {
      const first = options.find(option => !option.disabled);
      if (first) first.checked = true;
    }
    terms.textContent = recurring.checked
      ? `${plan.dataset.recurringAmount} каждые ${plan.dataset.recurringDays} дней. Отключение — в кабинете.`
      : '';
    if (submit) submit.textContent = recurring.checked ? 'Оплатить и включить автопродление' : 'Перейти к оплате';
  }
  form.addEventListener('change', event => {
    if (event.target.name === 'plan') recurring.checked = false;
    update();
  });
  // Consent is never restored by browser form-state restoration.
  window.addEventListener('pageshow', () => { recurring.checked = false; update(); });
  recurring.checked = false;
  update();
}());
