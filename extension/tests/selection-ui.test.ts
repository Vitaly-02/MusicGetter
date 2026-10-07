import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import 'fake-indexeddb/auto';
import { IDBFactory } from 'fake-indexeddb';
import { fixture as spotifyFixture, playlistPath } from './spotify-dom';
import { SelectionController } from '../src/content/selection-controller';
import { SelectionDatabase } from '../src/storage/selection-database';
import { selectionKey } from '../src/core/selection';
import { SpotifyAdapter } from '../src/sources/spotify/adapter';
import { YandexAdapter } from '../src/sources/yandex/adapter';
import { VKAdapter } from '../src/sources/vk/adapter';
import type { Track } from '../src/core/adapter';
import type { BrowserAPI } from '../src/core/browser';
import { session, origin, importID } from './helpers';
async function until(check:()=>boolean|Promise<boolean>):Promise<void>{const deadline=Date.now()+3000;while(!await check()){if(Date.now()>deadline)throw Error('UI condition timeout');await new Promise(r=>setTimeout(r,10));}}
for(const service of ['spotify','yandex','vk'] as const) test(`${service}: checkbox selection survives node removal/recycling, clear/start/cancel use stable identity`,async t=>{
  const f=spotifyFixture('playlist');t.after(()=>f.dom.window.close());
  const fixture=service==='vk'?'saved':'playlist';
  f.doc.body.innerHTML=readFileSync(`tests/fixtures/${service}/${fixture}.html`,'utf8');
  const url=service==='spotify'?'https://open.spotify.com'+playlistPath:service==='yandex'?'https://music.yandex.ru/users/public/playlists/4':'https://vk.com/audios42';
  f.dom.reconfigure({url});
  const adapter=service==='spotify'?new SpotifyAdapter(new URL(url),f.doc):service==='yandex'?new YandexAdapter(new URL(url),f.doc):new VKAdapter(new URL(url),f.doc);
  const db=new SelectionDatabase(new IDBFactory()), id=crypto.randomUUID(), nonce=crypto.randomUUID();
  await db.reset({id,owner:session.owner,origin,tabId:7,document:nonce,source:service,collection:adapter.getCollectionMetadata()!,mode:'selected',profile:'personal',destination:importID,state:'editing',count:0});
  let shadow:ShadowRoot|undefined;
  const attach=f.dom.window.Element.prototype.attachShadow;
  f.dom.window.Element.prototype.attachShadow=function(init){const root=attach.call(this,init);shadow=root;return root;};
  let started=false,cancelled=false;
  const browser={runtime:{sendMessage:async(m:{type:string;keys:string[];tracks:Track[];selected:boolean})=>{
    try{let data:unknown;
      if(m.type==='selection.contains')data={flags:await db.contains(id,m.keys),count:(await db.current())!.count};
      else if(m.type==='selection.change')data=await db.change(id,await Promise.all(m.tracks.map(async track=>({key:await selectionKey(service,track),track}))),m.selected);
      else if(m.type==='selection.clear'){await db.edit(id,c=>({...c,count:0}),true);data=0;}
      else if(m.type==='capture.start'){started=true;await db.edit(id,c=>({...c,state:'frozen'}));}
      else if(m.type==='capture.cancel')cancelled=true;
      return {ok:true,data};
    }catch{return {ok:false,error:'conflict'};}
  }}} as unknown as BrowserAPI;
  const controller=new SelectionController(f.doc,browser,adapter,id,nonce,'selected');t.after(()=>controller.dispose());
  await until(()=>!!shadow?.querySelector('input'));
  const original=adapter.selectionRows()[0]!, markup=original.element.innerHTML;
  const input=shadow!.querySelector('input')!;input.checked=true;input.dispatchEvent(new f.dom.window.Event('change',{bubbles:true}));
  await until(async()=> (await db.current())?.count===1);
  assert.equal(original.element.innerHTML,markup); // no site-row mutations
  const clone=original.element.cloneNode(true) as Element; original.element.replaceWith(clone);
  await until(()=>!!shadow!.querySelector<HTMLInputElement>('input:checked'));
  const link=clone.querySelector('a')!;
  const oldHref=link.getAttribute('href');link.setAttribute('href',service==='spotify'?'/track/9999999999999999999999':service==='yandex'?'/track/9999':'/audio-999_99');
  await until(()=>shadow!.querySelectorAll('input:checked').length===0);
  assert.equal((await db.current())?.count,1);
  if(oldHref)link.setAttribute('href',oldHref);
  await until(()=>shadow!.querySelectorAll('input:checked').length===1);
  const buttons=shadow!.querySelectorAll('button');
  await until(()=>!buttons[1]!.disabled);buttons[1]!.click();await until(async()=> (await db.current())?.count===0);
  await until(()=>!shadow!.querySelector<HTMLInputElement>('input')!.disabled);
  const again=shadow!.querySelector('input')!;again.checked=true;again.dispatchEvent(new f.dom.window.Event('change',{bubbles:true}));
  await until(()=>!buttons[2]!.disabled);buttons[2]!.click();await until(()=>started);
  assert.equal((await db.current())?.count,1);await until(()=>!buttons[3]!.disabled);buttons[3]!.click();await until(()=>cancelled);
});
