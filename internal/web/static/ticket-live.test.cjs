const {test} = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');

function harness(visibility = 'visible') {
  function node(tag) { return {tag, children: [], attrs: {}, appendChild(n){this.children.push(n);},setAttribute(k,v){this.attrs[k]=v;},getAttribute(k){return this.attrs[k];},querySelectorAll(){return [];}}; }
  const chat = node('chat'); chat.attrs = {'data-ticket':'42','data-csrf':'csrf-token'};
  const compose=node('form'),resolve=node('form'),requests=[],listeners={};
  const document={visibilityState:visibility,getElementById(id){return {chat,'ticket-compose':compose,'ticket-resolve':resolve}[id];},createElement:node,addEventListener(k,f){listeners[k]=f;}};
  let stream;
  vm.runInNewContext(fs.readFileSync(__dirname+'/ticket-live.js','utf8'), {document,Date,JSON,Array,Number,Math,encodeURIComponent,fetch(url,options){requests.push({url,options});return Promise.resolve({ok:true});},EventSource:class{constructor(){stream=this;}}});
  return {chat,compose,resolve,requests,document,listeners,send(payload){stream.onmessage({data:JSON.stringify(payload)});}};
}

test('live history preserves chronology, attachments and deduplicates replay',()=>{
  const h=harness();const messages=[{id:2,text:'second',html:'second',attachment:true},{id:1,text:'first',html:'first'}];
  h.send({open:true,messages});h.send({open:true,messages});
  assert.deepEqual(h.chat.children.map(n=>n.attrs['data-id']),['1','2']);
  const file=h.chat.children[1].children.find(n=>n.tag==='a');
  assert.equal(file.href,'/support/tickets/42/files/2');
  assert.equal(file.textContent,'Скачать вложение');
});

test('closed tickets hide stale reply and resolution forms',()=>{
  const h=harness();h.send({open:false,messages:[]});
  assert.equal(h.compose.hidden,true);assert.equal(h.resolve.hidden,true);
});

test('background tabs do not acknowledge unread answers until visible',async()=>{
  const h=harness('hidden');h.send({open:true,messages:[{id:9,html:'answer'}]});assert.equal(h.requests.length,0);
  h.document.visibilityState='visible';h.listeners.visibilitychange();await Promise.resolve();
  assert.equal(h.requests.length,1);assert.equal(h.requests[0].url,'/support/tickets/42/read');assert.match(h.requests[0].options.body,/csrf=csrf-token&through=9/);
  h.listeners.visibilitychange();assert.equal(h.requests.length,1);
});
