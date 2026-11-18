const test=require('node:test');const assert=require('node:assert/strict');const fs=require('node:fs');const vm=require('node:vm');
const code=fs.readFileSync(require('node:path').join(__dirname,'checkout-summary.js'),'utf8');
function fixture(){
 const plan={disabled:false,dataset:{summaryAmount:'799 ₽',summaryDays:'90',summaryDevices:'3',summaryUntil:'20.02.2027'}};
 const option={disabled:false},submit={disabled:false,textContent:'Оплатить'},summary={textContent:''},events={},windowEvents=[];
 const form={dataset:{},querySelector(s){return s.startsWith('button')?submit:s.includes('plan')?plan:option;},addEventListener(name,fn){events[name]=fn;}};
 vm.runInNewContext(code,{document:{querySelector(){return form;},getElementById(){return summary;}},window:{addEventListener(name,fn){if(name==='pageshow')windowEvents.push(fn);}}});
 return {plan,option,submit,summary,events,form,pageshow(){windowEvents.forEach(fn=>fn());}};
}
test('summary follows the selected server quote',()=>{const f=fixture();assert.match(f.summary.textContent,/799 ₽.*90 дней.*3 устройств.*20.02.2027/);f.plan.dataset.summaryAmount='299 ₽';f.events.change();assert.match(f.summary.textContent,/299 ₽/);});
test('Stars checkout shows the invoiced currency',()=>{const f=fixture();f.plan.dataset.summaryStars='42';f.option.value='stars.stars';f.events.change();assert.match(f.summary.textContent,/42 Stars/);assert.doesNotMatch(f.summary.textContent,/799 ₽/);});
test('unavailable payment disables checkout and duplicate submits are blocked',()=>{const f=fixture();f.option.disabled=true;f.events.change();assert.equal(f.submit.disabled,true);f.option.disabled=false;f.events.change();let prevented=false;f.events.submit({preventDefault(){prevented=true;}});assert.equal(f.submit.disabled,true);f.events.submit({preventDefault(){prevented=true;}});assert.equal(prevented,true);f.pageshow();assert.equal(f.submit.disabled,false);assert.equal(f.form.dataset.submitting,undefined);});
