const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const code=fs.readFileSync(require('node:path').join(__dirname,'miniapp.js'),'utf8');
function fixture({signed=true,ok=true,next='/account'}={}) {
 const state={calls:[],redirects:[]};
 const button={disabled:false,hidden:false},status={textContent:''};
 const form={elements:{csrf:{value:'csrf-value'},next:{value:'/history'}},querySelector(){return button;},addEventListener(_,callback){state.submit=callback;}};
 const app={initData:signed?'signed-raw-query':'',initDataUnsafe:{user:{id:999}},ready(){},expand(){}};
 vm.runInNewContext(code,{URLSearchParams,document:{getElementById(id){return id==='miniapp-auth'?form:status;},querySelectorAll(){return [];}},window:{Telegram:{WebApp:app},location:{pathname:'/app',replace(url){state.redirects.push(url);}}},fetch:async(path,options)=>{state.calls.push({path,options});return {ok,json:async()=>({next})};}});
 return {...state,button,status,calls:state.calls,redirects:state.redirects,submit:()=>state.submit({preventDefault(){}})};
}
const settle=()=>new Promise(resolve=>setImmediate(resolve));
test('signed initData and csrf reach same-origin auth before navigation',async()=>{const f=fixture();await settle();assert.equal(f.calls.length,1);assert.equal(f.calls[0].path,'/auth/telegram');assert.equal(f.calls[0].options.credentials,'same-origin');assert.equal(f.calls[0].options.body.get('init_data'),'signed-raw-query');assert.equal(f.calls[0].options.body.get('csrf'),'csrf-value');assert.deepEqual(f.redirects,['/account']);});
test('untrusted initDataUnsafe alone cannot log in',async()=>{const f=fixture({signed:false});await settle();assert.equal(f.calls.length,0);assert.equal(f.button.hidden,true);});
test('failed authentication can be retried without claiming success',async()=>{const f=fixture({ok:false});await settle();assert.equal(f.button.disabled,false);assert.deepEqual(f.redirects,[]);await f.submit();assert.equal(f.calls.length,2);});
test('a second tap cannot start a concurrent login',async()=>{const f=fixture();await f.submit();await settle();assert.equal(f.calls.length,1);});
test('external redirects from a malformed response are refused',async()=>{const f=fixture({next:'//evil.invalid/'});await settle();assert.deepEqual(f.redirects,[]);assert.equal(f.button.disabled,false);});

test('backslashes and control characters cannot turn a local response into an external URL',async()=>{for(const next of ['/\\evil.invalid/','/\n/evil.invalid/']) {const f=fixture({next});await settle();assert.deepEqual(f.redirects,[]);}});
