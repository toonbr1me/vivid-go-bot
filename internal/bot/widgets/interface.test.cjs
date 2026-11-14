const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const source=fs.readFileSync(__dirname+'/interface.html','utf8');

function runtime(state){
 let gesture=false,queuedTimer;
 class Element {
  constructor(tag='div'){this.tag=tag;this.style={};this.children=[];this.listeners={};this.disabled=false;this._text='';}
  set textContent(value){this._text=value;this.children=[];}
  get textContent(){return this._text+this.children.map(x=>x.textContent).join('');}
  appendChild(child){this.children.push(child);}
  setAttribute(name,value){this[name]=value;}
  addEventListener(name,fn){this.listeners[name]=fn;}
  click(){if(this.disabled)return;gesture=true;try{this.listeners.click?.()}finally{gesture=false}}
 }
 const nodes=Object.fromEntries([...source.matchAll(/id="([^"]+)"/g)].map(m=>[m[1],new Element()]));
 const events={},requests=[],copied=[],opened=[];
 const W={chatData:JSON.stringify(state),onEvent:(name,fn)=>events[name]=fn,ready(){},
  copyText(text){assert.equal(gesture,true,'copyText requires the original synchronous tap');copied.push(text)},
  openLink(url){assert.equal(gesture,true,'openLink requires the original synchronous tap');opened.push(url)},
  openTelegramLink(url){assert.equal(gesture,true);opened.push(url)},
  request(data){assert.equal(gesture,true);return new Promise((resolve,reject)=>requests.push({data,resolve,reject}))},
 };
 vm.runInNewContext(source.match(/<script>([\s\S]*?)<\/script>/)[1],{window:{TelegramWidget:W},document:{getElementById:id=>nodes[id],createElement:tag=>new Element(tag)},setTimeout:fn=>(queuedTimer=fn,1),clearTimeout:()=>queuedTimer=null});
 return {nodes,requests,copied,opened,update(s){W.chatData=JSON.stringify(s);events.chatDataChanged()},updateRaw(raw){W.chatData=raw;events.chatDataChanged()},timeout(){queuedTimer?.()}};
}
const initial={t:'Vivid',v:'Доступ',tab:'subscription',r:'revision1',p:0,z:1,b:[]};

test('links use the original tap without a network request',()=>{
 const r=runtime({...initial,b:[{t:'Открыть',i:1,k:'u',p:'https://example.com/subscription'}]});
 r.nodes.actions.children[0].click();assert.equal(r.opened.length,1);assert.equal(r.requests.length,0);
});
test('copy uses a native handoff and never claims clipboard success',async()=>{
 const r=runtime({...initial,b:[{t:'Получить ссылку',i:0,k:'c',p:'old-private-value'}]});r.nodes.actions.children[0].click();assert.equal(r.copied.length,0);assert.equal(r.requests.length,1);
 r.requests[0].resolve({result:JSON.stringify({notice:'Сообщение отправлено ниже'})});await Promise.resolve();assert.equal(r.nodes.notice.textContent,'Сообщение отправлено ниже');assert.equal(r.nodes.actions.children[0].disabled,false);
});
test('navigation stays locked until the new state arrives, then recovers',async()=>{
 const r=runtime(initial);r.nodes.devices.click();r.nodes.devices.click();assert.equal(r.requests.length,1);
 r.requests[0].resolve({result:'{}'});await Promise.resolve();assert.equal(r.nodes.devices.disabled,true);
 r.update({...initial,r:'revision2',tab:'devices'});assert.equal(r.nodes.devices.disabled,false);assert.equal(r.nodes.devices.className,'active');
});
test('state arriving before acknowledgement does not lock the new screen',async()=>{
 const r=runtime(initial);r.nodes.more.click();r.update({...initial,r:'revision2',tab:'more'});r.requests[0].resolve({result:'{}'});await Promise.resolve();
 assert.equal(r.nodes.more.disabled,false);assert.equal(r.nodes.notice.textContent,'');
});
test('errors and missing updates restore controls with visible feedback',async()=>{
 const r=runtime(initial);r.nodes.devices.click();r.requests[0].resolve({result:JSON.stringify({error:'Попробуйте позже'})});await Promise.resolve();
 assert.equal(r.nodes.devices.disabled,false);assert.equal(r.nodes.notice.className,'error');assert.equal(r.nodes.notice.textContent,'Попробуйте позже');
 r.nodes.devices.click();r.requests[1].resolve({result:'{}'});await Promise.resolve();r.timeout();assert.equal(r.nodes.devices.disabled,false);assert.match(r.nodes.notice.textContent,/Нет обновления/);
});
test('back and refresh use the issued utility indices',()=>{
 const r=runtime({...initial,back:4,f:7});r.nodes.back.click();assert.equal(r.requests[0].data,'ui:revision1:a:3');
 r.update({...initial,r:'revision2',f:7});r.nodes.refresh.click();assert.equal(r.requests[1].data,'ui:revision2:a:6');
});
test('an old acknowledgement cannot unlock a newer navigation request',async()=>{
 const r=runtime(initial);r.nodes.devices.click();r.update({...initial,r:'revision2',tab:'devices'});r.nodes.more.click();
 r.requests[0].resolve({result:'{}'});await Promise.resolve();assert.equal(r.nodes.more.disabled,true);
 r.requests[1].resolve({result:JSON.stringify({error:'Позже'})});await Promise.resolve();assert.equal(r.nodes.more.disabled,false);
});

test('automatic renewal checkbox changes only after server confirmation',async()=>{
 const r=runtime({...initial,b:[{t:'Продлевать автоматически?',s:'checkbox',checked:false,i:0,d:'Разовая оплата'}]});
 let checkbox=r.nodes.actions.children[0];
 assert.equal(checkbox.role,'checkbox');assert.equal(checkbox['aria-checked'],'false');
 checkbox.click();checkbox.click();assert.equal(r.requests.length,1);
 assert.equal(checkbox['aria-checked'],'false','tap alone is not saved consent');
 r.requests[0].resolve({result:'{}'});await Promise.resolve();
 r.update({...initial,r:'revision2',b:[{t:'Продлевать автоматически?',s:'checkbox',checked:true,i:0,d:'100 ₽ каждые 30 дней'}]});
 checkbox=r.nodes.actions.children[0];assert.equal(checkbox['aria-checked'],'true');assert.match(checkbox.textContent,/каждые 30 дней/);
});

test('failed checkbox update restores controls without claiming consent',async()=>{
 const r=runtime({...initial,b:[{t:'Продлевать автоматически?',s:'checkbox',checked:false,i:0}]});
 r.nodes.actions.children[0].click();r.requests[0].reject({code:'RATE_LIMITED'});await Promise.resolve();
 assert.equal(r.nodes.actions.children[0].disabled,false);assert.equal(r.nodes.actions.children[0]['aria-checked'],'false');assert.match(r.nodes.notice.textContent,/Подождите/);
});

test('broken chat state does not destroy the last usable screen',()=>{
 const r=runtime({...initial,b:[{t:'Моя подписка',i:0}]});
 for(const raw of ['{broken','null','{}','[]'])assert.doesNotThrow(()=>r.updateRaw(raw));
 assert.equal(r.nodes.title.textContent,'Vivid');assert.equal(r.nodes.actions.children.length,1);
});

test('device slots show occupancy and reset between screens',()=>{
 const r=runtime({...initial,used:2,limit:5});assert.equal(r.nodes.slots.children.length,5);
 assert.equal(r.nodes.slots.children.filter(x=>x.className==='used').length,2);
 assert.match(r.nodes['slot-note'].textContent,/2 из 5/);
 r.update({...initial,r:'revision2',used:1,limit:3});assert.equal(r.nodes.slots.children.length,3);
 assert.equal(r.nodes.slots.children.filter(x=>x.className==='used').length,1);
 r.update({...initial,r:'revision3'});assert.equal(r.nodes['slot-group'].style.display,'none');
});

test('untrusted labels and support text stay literal DOM text',()=>{
 const attack='<img src=x onerror=alert(1)>';
 const r=runtime({...initial,t:attack,x:attack,b:[{t:attack,i:0}]});
 assert.equal(r.nodes.title.textContent,attack);assert.equal(r.nodes.body.textContent,attack);assert.equal(r.nodes.actions.children[0].textContent,attack);
 assert.equal(r.opened.length,0);assert.equal(r.requests.length,0);
});

test('rejected transport requests can be retried and paging boundaries stay disabled',async()=>{
 const r=runtime({...initial,p:0,z:2});assert.equal(r.nodes.prev.disabled,true);assert.equal(r.nodes.next.disabled,false);
 r.nodes.next.click();r.requests[0].reject(new Error('offline'));await Promise.resolve();
 assert.equal(r.nodes.prev.disabled,true);assert.equal(r.nodes.next.disabled,false);r.nodes.next.click();assert.equal(r.requests.length,2);
 r.update({...initial,r:'revision2',p:1,z:2});assert.equal(r.nodes.prev.disabled,false);assert.equal(r.nodes.next.disabled,true);
});
