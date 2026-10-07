import test from 'node:test';
import assert from 'node:assert/strict';
import 'fake-indexeddb/auto';
import { IDBFactory } from 'fake-indexeddb';
import { fixture } from './spotify-dom';
import { SelectionDatabase } from '../src/storage/selection-database';
import { SelectionProducer } from '../src/core/selection-producer';
import { SelectionController } from '../src/content/selection-controller';
import { CaptureCommands } from '../src/background/capture-commands';
import { Commands } from '../src/background/commands';
import { Dispatcher } from '../src/background/dispatcher';
import { Engine } from '../src/core/engine';
import { Sessions } from '../src/core/session';
import { Client } from '../src/api/client';
import { safeError } from '../src/core/errors';
import type { BrowserAPI } from '../src/core/browser';
import { MemoryStore, FakeAPI, origin, importID } from './helpers';
const wait=async(check:()=>boolean|Promise<boolean>)=>{const end=Date.now()+5000;while(!await check()){if(Date.now()>end)throw Error('Capture flow timed out');await new Promise(r=>setTimeout(r,10));}};
function harness() {
  const f=fixture('playlist');const store=new MemoryStore();store.j=undefined;
  const db=new SelectionDatabase(new IDBFactory()),api=new FakeAPI(),engine=new Engine(store,origin,()=>api,new SelectionProducer(db));
  const nonce=crypto.randomUUID();let ui:SelectionController|undefined,shadow:ShadowRoot|undefined,dispatcher:Dispatcher;
  const attach=f.dom.window.Element.prototype.attachShadow;f.dom.window.Element.prototype.attachShadow=function(init){const value=attach.call(this,init);shadow=value;return value;};
  const popup={id:'own',url:'chrome-extension://own/popup.html'};
  const sender={id:'own',url:f.doc.location.href,tab:{id:7},frameId:0};
  const browser={
    runtime:{id:'own',getURL:(path:string)=>'chrome-extension://own/'+path,sendMessage:async(message:unknown)=>{
      try{const data=await dispatcher.run(message,sender);void engine.tick();return {ok:true,data};}catch(error){return {ok:false,error:safeError(error)};}
    }},
    permissions:{contains:async()=>true},scripting:{executeScript:async()=>undefined},
    tabs:{query:async()=>[{id:7,url:f.doc.location.href}],sendMessage:async(_id:number,m:{type:string;id:string;mode:'all'|'selected'})=>{
      if(m.type==='capture.describe')return {source:'spotify',document:nonce,collection:f.adapter.getCollectionMetadata()};
      if(m.type==='capture.init'){ui=new SelectionController(f.doc,browser,f.adapter,m.id,nonce,m.mode);return {ok:true};}
      if(m.type==='capture.close'){ui?.dispose();return {ok:true};}
      if(m.type==='page.info')return {page:f.adapter.detectPage()};
      throw Error('Unexpected tab message');
    }},
  } as unknown as BrowserAPI;
  const captures=new CaptureCommands(browser,store,db,engine,origin,'http://127.0.0.1/*');
  const commands=new Commands(browser,store,engine,new Sessions(store,origin,t=>new Client(origin,t)),origin,'http://127.0.0.1/*');
  dispatcher=new Dispatcher(browser,commands,captures);
  return {...f,store,db,api,engine,dispatcher,popup,get shadow(){return shadow!;},close:()=>{ui?.dispose();f.dom.window.close();}};
}
test('popup Import all runs real DOM capture through serialized RPC and durable upload without deadlock',async t=>{
  const f=harness();t.after(f.close);
  await f.dispatcher.run({type:'capture.open',mode:'all',profileKey:'personal',destinationId:importID},f.popup);
  await wait(async()=>{await f.engine.tick();return f.store.j?.stage==='done';});
  assert.equal(f.store.j?.accepted,2);assert.equal(f.api.completes[0]?.completeness,'complete');
  assert.equal(f.api.requests[0]?.tracks[0]?.artists.length,2);
});
test('Select all stages metadata but waits for explicit start; clear and logout remove the snapshot',async t=>{
  const f=harness();t.after(f.close);
  await f.dispatcher.run({type:'capture.open',mode:'selected',profileKey:'personal',destinationId:importID},f.popup);
  const buttons=f.shadow.querySelectorAll('button');buttons[0]!.click();
  await wait(async()=> (await f.db.current())?.count===2 && !buttons[0]!.disabled);
  assert.equal(f.store.j,undefined);
  buttons[1]!.click();await wait(async()=> (await f.db.current())?.count===0 && !buttons[0]!.disabled);
  buttons[0]!.click();await wait(async()=> (await f.db.current())?.count===2 && !buttons[2]!.disabled);
  buttons[2]!.click();await wait(async()=>{await f.engine.tick();return f.store.j?.stage==='done';});
  assert.equal(f.api.completes[0]?.completeness,'partial');
  await f.dispatcher.run({type:'logout'},f.popup);assert.equal(await f.db.current(),undefined);assert.equal(await f.store.session(),undefined);
});

test('logout removes credentials/job even when selection database cleanup fails',async t=>{
  const f=harness();t.after(f.close);
  await f.dispatcher.run({type:'capture.open',mode:'selected',profileKey:'personal',destinationId:importID},f.popup);
  t.mock.method(f.db,'reset',async()=>{throw Error('storage unavailable');});
  await assert.rejects(()=>f.dispatcher.run({type:'logout'},f.popup));
  assert.equal(await f.store.session(),undefined);assert.equal(await f.store.job(),undefined);
});
