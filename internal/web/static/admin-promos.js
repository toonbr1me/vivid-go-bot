(() => {
  'use strict';
  const select = document.querySelector('[data-promo-type]');
  if (!select) return;
  function update() {
    document.querySelectorAll('[data-promo-benefit]').forEach(group => {
      const active = group.dataset.promoBenefit === select.value;
      group.hidden = !active;
      const input = group.querySelector('input');
      input.disabled = !active;
      input.required = active;
      input.min = active ? '1' : '0';
    });
  }
  select.addEventListener('change', update);
  update();
  const search = document.querySelector('[data-promo-search]');
  const empty = document.querySelector('[data-promo-no-results]');
  if (search) search.addEventListener('input', () => {
    const query = search.value.trim().toUpperCase();
    let found = 0;
    document.querySelectorAll('[data-promo-code]').forEach(item => {
      item.hidden = !item.dataset.promoCode.toUpperCase().includes(query);
      if (!item.hidden) found++;
    });
    if (empty) empty.hidden = found > 0 || !query;
  });
})();
