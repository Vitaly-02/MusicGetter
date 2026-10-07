import test from 'node:test';
import assert from 'node:assert/strict';
import 'fake-indexeddb/auto';
import { IDBFactory } from 'fake-indexeddb';
import { SelectionEngine, selectionKey } from '../src/core/selection';
import { SelectionDatabase } from '../src/storage/selection-database';
import type { Capture } from '../src/core/capture';
import type { Track } from '../src/core/adapter';
import { session, origin, importID } from './helpers';
export function capture(): Capture { return {id:crypto.randomUUID(),owner:session.owner,origin,tabId:7,document:crypto.randomUUID(),source:'spotify',collection:{key:'/playlist/test',title:'Fixture',kind:'playlist',provisional:false},mode:'selected',profile:'personal',destination:importID,state:'editing',count:0}; }
const track: Track = {title:' Song ',artists:['ARTIST','Guest'],album:'Album',duration_ms:123000,position:0};
test('selection identity normalizes metadata, ignores node/position, preserves recording distinctions and source', async () => {
  const key = await selectionKey('spotify',track);
  assert.equal(key,await selectionKey('spotify',{...track,title:'ＳＯＮＧ',artists:['guest','artist','ARTIST'],position:999}));
  for (const changed of [{...track,album:'Other'},{...track,duration_ms:123001},{...track,version:'Live'},{...track,source_track_key:'known'}]) assert.notEqual(key,await selectionKey('spotify',changed));
  assert.notEqual(key,await selectionKey('vk',track));
  assert.equal(await selectionKey('spotify',{...track,source_track_key:'id'}),await selectionKey('spotify',{...track,title:'Renamed',source_track_key:'id'}));
  assert.notEqual(await selectionKey('spotify',{...track,source_track_key:'id1'}),await selectionKey('spotify',{...track,source_track_key:'id2'}));
});
test('durable selection survives node removal and background restart; concurrent repeats do not increment count', async () => {
  const factory = new IDBFactory(), first = new SelectionDatabase(factory), c = capture(); await first.reset(c);
  const engine = new SelectionEngine(c.source,{contains:keys=>first.contains(c.id,keys),change:async(tracks,selected)=>first.change(c.id,await Promise.all(tracks.map(async t=>({key:await selectionKey(c.source,t),track:t}))),selected),clear:async()=>{await first.edit(c.id,c=>({...c,count:0}),true);return 0;}});
  await Promise.all(Array.from({length:20},()=>engine.set(track,true)));
  assert.equal((await first.current())?.count,1);
  const second = new SelectionDatabase(factory);
  assert.deepEqual(await second.contains(c.id,[await engine.key({...track,position:100})]),[true]);
  await engine.set({...track,position:100},false); assert.equal((await second.current())?.count,0);
  await engine.selectBatch([track,{...track,title:'Other'}]); assert.equal((await second.current())?.count,2);
  await engine.clear(); assert.equal((await second.current())?.count,0);
});
test('freeze creates immutable selection and keyset pagination honors count and byte bounds', async () => {
  const db = new SelectionDatabase(new IDBFactory()), c = capture(); await db.reset(c);
  const entries = await Promise.all(Array.from({length:450},async(_,i)=>{const t={...track,source_track_key:String(i)};return {key:await selectionKey(c.source,t),track:t};}));
  for (let i=0;i<entries.length;i+=100) await db.change(c.id,entries.slice(i,i+100),true);
  await db.edit(c.id,c=>({...c,state:'frozen'}));
  await assert.rejects(()=>db.change(c.id,[entries[0]!],false));
  let after='',total=0; const keys=new Set<string>();
  for (;;) { const page=await db.page(c.id,after); if(!page.length)break;assert.ok(page.length<=200);for(const e of page)keys.add(e.key);total+=page.length;after=page.at(-1)!.key; }
  assert.equal(total,450);assert.equal(keys.size,450);
  await db.reset(); await assert.rejects(()=>db.change(c.id,[entries[0]!],true)); assert.equal(await db.current(),undefined);
});

test('selection change versus freeze is transactional; post-seal changes invalidate completeness',async()=>{
  const db=new SelectionDatabase(new IDBFactory()), c=capture();await db.reset({...c,mode:'all'});
  const entry={key:await selectionKey(c.source,track),track};await db.change(c.id,[entry],true);
  await db.edit(c.id,c=>({...c,summary:{completeness:'complete',reason:'visible_end_confirmed',observedCount:1}}));
  await db.change(c.id,[entry],false);assert.equal((await db.current())?.summary,undefined);
  const outcomes=await Promise.allSettled([db.change(c.id,[entry],true),db.edit(c.id,c=>({...c,state:'frozen'}))]);
  assert.equal(outcomes[1]?.status,'fulfilled');
  const frozen=await db.current();assert.equal((await db.page(c.id)).length,frozen?.count);
  await assert.rejects(()=>db.change(c.id,[entry],true));
});
test('selection keyset page byte budget works for maximum-size Unicode metadata',async()=>{
  const db=new SelectionDatabase(new IDBFactory()), c=capture();await db.reset(c);
  for(let i=0;i<10;i++){
    const t={title:'🎵'.repeat(1000),artists:Array.from({length:32},(_,n)=>'🎵'.repeat(500)+n),source_track_key:String(i),position:i};
    await db.change(c.id,[{key:await selectionKey(c.source,t),track:t}],true);
  }
  await db.edit(c.id,c=>({...c,state:'frozen'}));
  const first=await db.page(c.id);assert.ok(first.length>0 && first.length<10);
  assert.ok(new TextEncoder().encode(JSON.stringify({tracks:first.map(e=>e.track)})).length<512*1024-512);
  assert.equal(first.length+(await db.page(c.id,first.at(-1)!.key)).length,10);
});
