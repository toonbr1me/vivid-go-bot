const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const code = fs.readFileSync(require('node:path').join(__dirname, 'order-status.js'), 'utf8');
const settle = () => new Promise(resolve => setImmediate(resolve));
function fixture(responses) {
  const elements = Object.fromEntries(['label','detail','next','refresh','feedback','fact','pay'].map(key => [key, {textContent:'', hidden:true, href:'', disabled:false, listeners:{}, addEventListener(name,fn){this.listeners[name]=fn;}}]));
  const doc = {hidden:false, listeners:{}, addEventListener(name,fn){this.listeners[name]=fn;}, querySelector(){return card;}};
  const card = {dataset:{orderStatus:'/account/orders/owned/status'},querySelector(selector){return elements[selector.replace('[data-status-','').replace(']','')];}};
  let calls = 0; const timers = [];
  vm.runInNewContext(code, {document:doc, window:{addEventListener(){}}, Set, Error, Math, clearTimeout(){}, setTimeout(fn){timers.push(fn);}, fetch:async()=>{
    const state=responses[Math.min(calls++,responses.length-1)];
    if (state instanceof Error) throw state;
    return {ok:!state.http,status:state.http||200,json:async()=>state};
  }});
  return {elements,doc,timers,calls:()=>calls};
}
test('pending payment becomes ready only after server confirmation',async()=>{
 const f=fixture([{status:'pending',label:'Ожидаем оплату',detail:'Ждём подтверждения',ready:false,poll:true,next:'/connect'},{status:'provisioned',label:'Доступ выдан',detail:'Готово',ready:true,poll:false,next:'/connect'}]);
 await settle();assert.equal(f.elements.next.hidden,true);assert.equal(f.calls(),1);
 await f.timers.shift()();await settle();assert.equal(f.elements.next.hidden,false);assert.equal(f.elements.next.href,'/connect');assert.equal(f.elements.fact.textContent,'Доступ выдан');assert.equal(f.elements.pay.hidden,true);assert.equal(f.timers.length,0);
});
test('foreign or expired sessions stop background polling',async()=>{for(const http of [401,403,404]){const f=fixture([{http}]);await settle();assert.equal(f.timers.length,0);assert.equal(f.elements.next.hidden,true);}});
test('invalid destinations never become actionable',async()=>{const f=fixture([{status:'provisioned',label:'Готово',detail:'Готово',ready:true,poll:false,next:'https://evil.invalid/'}]);await settle();assert.equal(f.elements.next.hidden,true);});
test('expired invoices hide the pay action even while late payments are checked',async()=>{const f=fixture([{status:'pending',label:'Счёт истёк',detail:'Проверьте оплату',can_pay:false,ready:false,poll:true,next:'/connect'}]);await settle();assert.equal(f.elements.pay.hidden,true);});
test('temporary failures retain status and pause while hidden',async()=>{const f=fixture([new Error('offline')]);await settle();assert.equal(f.elements.label.textContent,'');assert.match(f.elements.feedback.textContent,/Не удалось/);f.doc.hidden=true;await f.timers.shift()();assert.equal(f.calls(),1);f.doc.hidden=false;await f.doc.listeners.visibilitychange();await settle();assert.equal(f.calls(),2);});
test('automatic checks are bounded and manual retry remains possible',async()=>{const f=fixture([{status:'paid',label:'Выдаём',detail:'Подождите',ready:false,poll:true,next:'/connect'}]);await settle();for(let i=0;i<23;i++){await f.timers.shift()();await settle();}assert.equal(f.calls(),24);assert.equal(f.timers.length,0);assert.match(f.elements.feedback.textContent,/приостановлена/);await f.elements.refresh.listeners.click();await settle();assert.equal(f.calls(),25);});
