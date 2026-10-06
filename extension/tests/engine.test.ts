import test from 'node:test';
import assert from 'node:assert/strict';
import { Engine } from '../src/core/engine';
import { DemoProducer } from '../src/core/demo';
import { AppError } from '../src/core/errors';
import { MemoryStore, FakeAPI, origin, session } from './helpers';
import type { ChunkReceipt, ImportChunk, CreateImportRequest } from '../src/core/contracts';

test('450 demo tracks use three chunks, durable counters and explicit partial seal',async()=>{
 const store=new MemoryStore(),api=new FakeAPI(),engine=new Engine(store,origin,()=>api,new DemoProducer());
 for(let i=0;i<5;i++)await engine.tick();
 assert.deepEqual(api.requests.map(c=>c.tracks.length),[200,200,50]);assert.equal(store.j?.accepted,450);assert.equal(store.j?.stage,'done');assert.equal(store.j?.pending,undefined);
 assert.deepEqual(api.completes,[{last_sequence:2,observed_count:450,completeness:'partial',reason:'unknown_end'}]);
});
test('lost ACK and worker restart resend identical stored key and body',async()=>{
 const store=new MemoryStore();let now=1_000_000;
 class FailOnce extends FakeAPI { override async append(id:string,c:ImportChunk):Promise<ChunkReceipt>{ const ack=await super.append(id,c);if(this.requests.length===1)throw new AppError('network',true,5000);return {...ack,replay:this.requests.length===2}; } }
 const api=new FailOnce();let engine=new Engine(store,origin,()=>api,new DemoProducer(),()=>now);
 await engine.tick();assert.equal(store.j?.stage,'uploading');assert.equal(store.j?.accepted,0);assert.ok(store.j?.pending);assert.ok(store.j.retryAt>=now+5000);
 const pending=structuredClone(store.j.pending);
 engine=new Engine(store,origin,()=>api,new DemoProducer(),()=>now);await engine.tick();assert.equal(api.requests.length,1);
 now+=100_000;await engine.tick();assert.deepEqual(api.requests[1],pending);assert.equal(store.j?.accepted,400);
});
test('parallel wakeups do not execute a job twice',async()=>{
 const store=new MemoryStore(),api=new FakeAPI(),engine=new Engine(store,origin,()=>api,new DemoProducer());
 await Promise.all(Array.from({length:20},()=>engine.tick()));assert.equal(api.creates.length,1);assert.equal(api.requests.length,1);
});
test('cancel recovers uncertain create ID and stops further chunks',async()=>{
 const store=new MemoryStore();
 class UncertainCreate extends FakeAPI { override async create(c:CreateImportRequest){const value=await super.create(c);if(this.creates.length===1)throw new AppError('network',true);return value;} }
 const api=new UncertainCreate(),engine=new Engine(store,origin,()=>api,new DemoProducer());
 await engine.tick();await engine.cancel();await engine.tick();
 assert.deepEqual(api.creates[0],api.creates[1]);assert.equal(api.cancels,1);assert.equal(api.requests.length,0);assert.equal(store.j?.stage,'cancelled');
});
test('logout during request never resurrects local queue',async()=>{
 const store=new MemoryStore();let done!:()=>void;const wait=new Promise<void>(r=>done=r);
 class Slow extends FakeAPI { override async create(c:CreateImportRequest){await wait;return super.create(c);} }
 const engine=new Engine(store,origin,()=>new Slow(),new DemoProducer());const running=engine.tick();
 await new Promise(resolve=>setImmediate(resolve));await store.clearAll();done();await running;assert.equal(await store.job(),undefined);
});
test('new account or backend origin cannot replay old queue',async()=>{
 for(const change of ['owner','origin'] as const){const store=new MemoryStore(),api=new FakeAPI();store.s={...session,[change]:'other'};const engine=new Engine(store,origin,()=>api,new DemoProducer());await engine.tick();assert.equal(api.creates.length,0);assert.equal(store.j?.error,'account_mismatch');}
});
test('permanent conflicts pause; bounded retry budget stops offline loops',async()=>{
 for(const error of [new AppError('conflict'),new AppError('network',true)]){
 const store=new MemoryStore();class Fails extends FakeAPI{override async create():Promise<never>{throw error;}}
 let now=1;const api=new Fails(),engine=new Engine(store,origin,()=>api,new DemoProducer(),()=>now);
 for(let i=0;i<12;i++){await engine.tick();now+=8_000_000;}
 assert.equal(store.j?.blocked,true);assert.equal(store.j.attempts,error.retryable?8:1);
 }
});
test('unsafe producer output is not persisted or uploaded',async()=>{
 const store=new MemoryStore(),api=new FakeAPI();
 const producer={next:async()=>({schema_version:1 as const,sequence:0,idempotency_key:'k',tracks:[{title:'a',artists:['b'],position:0,cookies:'SECRET'}]})};
 const engine=new Engine(store,origin,()=>api,producer);await engine.tick();assert.equal(api.requests.length,0);assert.equal(store.j?.error,'invalid_data');assert.equal(store.j.pending,undefined);
});
