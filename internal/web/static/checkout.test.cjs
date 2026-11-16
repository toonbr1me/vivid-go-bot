const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function fixture() {
  const plan = { dataset: { recurringOptions: 'yookassa.card', recurringAmount: '199 ₽', recurringDays: '30' } };
  const recurring = { checked: true, disabled: false };
  const terms = { textContent: '' };
  const submit = { textContent: '' };
  const options = ['balance.balance', 'yookassa.card', 'yookassa.sbp'].map((value, i) => ({
    value, checked: i === 0, disabled: false, label: { hidden: false }, closest() { return this.label; }
  }));
  let change, pageshow;
  const form = {
    querySelector(selector) { return selector.startsWith('button') ? submit : plan; },
    querySelectorAll() { return options; },
    addEventListener(name, callback) { change = callback; }
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, 'checkout.js'), 'utf8'), {
    document: { querySelector() { return form; }, getElementById(id) { return id === 'checkout-recurring' ? recurring : terms; } },
    window: { addEventListener(name, callback) { pageshow = callback; } }
  });
  return { plan, recurring, terms, submit, options, change: name => change({ target: { name } }), pageshow: () => pageshow() };
}

test('consent starts off and only compatible methods remain when enabled', () => {
  const f = fixture();
  assert.equal(f.recurring.checked, false);
  f.recurring.checked = true;
  f.change('recurring');
  assert.equal(f.options[0].disabled, true);
  assert.equal(f.options[1].checked, true);
  assert.equal(f.options[2].label.hidden, true);
  assert.match(f.terms.textContent, /199 ₽ каждые 30 дней/);
});

test('changing plan and restoring a page clear consent', () => {
  const f = fixture();
  f.recurring.checked = true;
  f.change('plan');
  assert.equal(f.recurring.checked, false);
  assert.ok(f.options.every(option => !option.disabled));
  f.recurring.checked = true;
  f.pageshow();
  assert.equal(f.recurring.checked, false);
});

test('free or incompatible plans cannot enable recurring', () => {
  const f = fixture();
  f.plan.dataset.recurringOptions = '';
  f.recurring.checked = true;
  f.change('recurring');
  assert.equal(f.recurring.disabled, true);
  assert.equal(f.recurring.checked, false);
  assert.equal(f.terms.textContent, '');
});
