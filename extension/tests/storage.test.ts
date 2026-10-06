import test from 'node:test';
import assert from 'node:assert/strict';
import { IDBFactory } from 'fake-indexeddb';
import { Database } from '../src/storage/database';
import { job, session } from './helpers';
test('IndexedDB recovers credentials and pending state across service worker instances',async()=>{
 const idb=new IDBFactory();const first=new Database(idb);await first.setSession(session);const j=job();await first.setJob(j);
 const second=new Database(idb);assert.deepEqual(await second.session(),session);assert.deepEqual(await second.job(),j);
 await Promise.all(Array.from({length:20},()=>second.updateJob(j.id,current=>({...current,accepted:current.accepted+1}))));assert.equal((await first.job())?.accepted,20);
 await first.clearAll();await second.updateJob(j.id,current=>({...current,accepted:999}));assert.equal(await second.session(),undefined);assert.equal(await second.job(),undefined);
});
test('old request cannot clear a newly paired credential',async()=>{
 const db=new Database(new IDBFactory());await db.setSession(session);await db.clearSession('old-token');assert.deepEqual(await db.session(),session);await db.clearSession(session.token);assert.equal(await db.session(),undefined);
});
