import test from 'node:test';
import assert from 'node:assert/strict';
import 'fake-indexeddb/auto';
import { IDBFactory } from 'fake-indexeddb';
import { CaptureCommands } from '../src/background/capture-commands';
import { SelectionDatabase } from '../src/storage/selection-database';
import { SelectionProducer } from '../src/core/selection-producer';
import { Engine } from '../src/core/engine';
import { AppError } from '../src/core/errors';
import { MemoryStore, FakeAPI, origin, session, importID } from './helpers';
import type { BrowserAPI, Sender } from '../src/core/browser';
function setup() {
  const store=new MemoryStore();store.j=undefined;
  const db=new SelectionDatabase(new IDBFactory()), api=new FakeAPI();
  const engine=new Engine(store,origin,()=>api,new SelectionProducer(db));
  const nonce=crypto.randomUUID(); const sent: unknown[]=[];
  const browser={runtime:{id:'own'},permissions:{contains:async()=>true},tabs:{query:async()=>[{id:7,url:'https://open.spotify.com/playlist/test'}],sendMessage:async(_id:number,m:{type:string})=>{sent.push(m);return m.type==='capture.describe'?{source:'spotify',collection:{key:'/playlist/test',title:'Fixture',kind:'playlist',provisional:false},document:nonce}:{ok:true};}},scripting:{executeScript:async()=>undefined}} as unknown as BrowserAPI;
  const commands=new CaptureCommands(browser,store,db,engine,origin,'http://127.0.0.1/*');
  const sender:Sender={id:'own',tab:{id:7},frameId:0,url:'https://open.spotify.com/playlist/test'};
  const open=(mode='selected')=>commands.popup({type:'capture.open',mode,destinationId:importID,profileKey:'personal'});
  const send=async(type:string,fields:Record<string,unknown>={},who=sender)=>commands.content({type,id:(await db.current())?.id,document:nonce,...fields},who);
  return {store,db,api,engine,nonce,sent,commands,sender,open,send};
}
test('capture RPC pins extension, tab, top frame, document, owner and source; rejects extra credentials', async()=>{
  const f=setup();await f.open();
  const tracks=[{title:'Song',artists:['Artist'],position:0}];
  for(const sender of [{...f.sender,id:'other'},{...f.sender,frameId:1},{...f.sender,tab:{id:8}},{...f.sender,url:'https://evil.example/'},{id:'own'}])await assert.rejects(()=>f.send('selection.change',{tracks,selected:true},sender));
  await assert.rejects(()=>f.send('selection.change',{tracks,selected:true,document:crypto.randomUUID()}));
  await assert.rejects(()=>f.send('selection.change',{tracks:[{...tracks[0],cookies:'bad'}],selected:true}));
  await assert.rejects(()=>f.send('selection.change',{tracks:[{...tracks[0],source_url:'https://vk.com/audio1_2'}],selected:true}));
  assert.equal(await f.send('selection.change',{tracks,selected:true}),1);
  assert.ok(!JSON.stringify(f.sent).includes(session.token));
  f.store.s={...session,owner:crypto.randomUUID()};await assert.rejects(()=>f.send('selection.clear'));
});
test('selection start freezes data, replays lost start ACK and uploads exact chunks after worker restart',async()=>{
  const f=setup();await f.open();
  for(let start=0;start<450;start+=150)await f.send('selection.change',{selected:true,tracks:Array.from({length:150},(_,i)=>({title:`Song ${start+i}`,artists:['Artist'],source_track_key:String(start+i),position:start+i}))});
  await f.send('capture.start');const id=f.store.j!.id;
  await f.send('capture.start');assert.equal(f.store.j!.id,id);
  assert.match(f.store.j!.create.source.collection_key,/^selection:/);
  await assert.rejects(()=>f.send('selection.clear'));
  const original=f.api.append.bind(f.api);let lost=true;
  f.api.append=async(id,chunk)=>{const ack=await original(id,chunk);if(lost){lost=false;throw new AppError('network',true);}return ack;};
  await f.engine.tick();assert.equal(f.store.j!.pending?.tracks.length,200);
  const pending=structuredClone(f.store.j!.pending);
  const restarted=new Engine(f.store,origin,()=>f.api,new SelectionProducer(f.db));await restarted.resume();
  for(let i=0;i<8;i++)await restarted.tick();
  assert.deepEqual(f.api.requests[1],pending);assert.equal(f.store.j!.accepted,450);assert.equal(f.store.j!.stage,'done');
  assert.deepEqual(f.api.requests.slice(1).map(c=>c.tracks.length),[200,200,50]);
  assert.equal(f.api.completes[0]?.completeness,'partial');
});
test('Import all requires seal and propagates complete only with matching durable count',async()=>{
  const f=setup();await f.open('all');
  await f.send('selection.change',{selected:true,tracks:[{title:'One',artists:['Artist'],source_track_key:'one',position:0}]});
  await assert.rejects(()=>f.send('capture.start'));
  await f.send('capture.seal',{summary:{completeness:'complete',reason:'visible_end_confirmed',observedCount:1}});
  await f.send('capture.start');for(let i=0;i<3;i++)await f.engine.tick();
  assert.equal(f.api.completes[0]?.completeness,'complete');
});
test('cancel before import freezes out late messages; cancel after start cancels backend',async()=>{
  const f=setup();await f.open();await f.send('capture.cancel');
  await assert.rejects(()=>f.send('selection.change',{tracks:[{title:'Late',artists:['Artist'],position:0}],selected:true}));
  assert.equal((await f.db.current())?.count,0);
  await f.open();await f.send('selection.change',{tracks:[{title:'Song',artists:['Artist'],position:0}],selected:true});
  await f.send('capture.start');await f.send('capture.cancel');await f.engine.tick();assert.equal(f.api.cancels,1);assert.equal(f.store.j!.stage,'cancelled');
});

test('expired session can record local cancellation but cannot edit; new owner cannot cancel',async()=>{
  const f=setup();await f.open();f.store.s={...session,expiresAt:'2000-01-01T00:00:00Z'};
  await assert.rejects(()=>f.send('selection.change',{selected:true,tracks:[{title:'Song',artists:['Artist'],position:0}]}));
  await f.send('capture.cancel');assert.equal((await f.db.current())?.state,'cancelled');
  f.store.s={...session};await f.open();f.store.s={...session,owner:crypto.randomUUID(),expiresAt:'2000-01-01T00:00:00Z'};
  await assert.rejects(()=>f.send('capture.cancel'));
});

test('seal with mismatched count cannot claim full capture; clear and late messages do not resurrect data',async()=>{
  const f=setup();await f.open('all');const old=await f.db.current();
  await f.send('selection.change',{selected:true,tracks:[{title:'Song',artists:['Artist'],position:0}]});
  await f.send('capture.seal',{summary:{completeness:'complete',reason:'visible_end_confirmed',observedCount:2}});
  await f.send('capture.start');assert.equal(f.store.j!.summary?.completeness,'partial');
  await f.commands.clear();await f.store.clearAll();
  await assert.rejects(()=>f.commands.content({type:'selection.change',id:old!.id,document:f.nonce,selected:true,tracks:[{title:'Late',artists:['Artist'],position:0}]},f.sender));
  assert.equal(await f.db.current(),undefined);assert.equal(await f.store.job(),undefined);
});
