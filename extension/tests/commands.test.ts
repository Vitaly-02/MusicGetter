import test from 'node:test';
import assert from 'node:assert/strict';
import { Commands } from '../src/background/commands';
import { Engine } from '../src/core/engine';
import { Sessions } from '../src/core/session';
import { DemoProducer } from '../src/core/demo';
import { Client } from '../src/api/client';
import { MemoryStore, FakeAPI, origin, session } from './helpers';
import type { BrowserAPI } from '../src/core/browser';
test('popup status never serializes session token or chunk metadata',async()=>{
 const store=new MemoryStore();const engine=new Engine(store,origin,()=>new FakeAPI(),new DemoProducer());
 const sessions=new Sessions(store,origin,t=>new Client(origin,t));
 const commands=new Commands({} as BrowserAPI,store,engine,sessions,origin,'http://127.0.0.1/*');
 const output=JSON.stringify(await commands.run({type:'state'}));
 assert.equal(output.includes(session.token),false);assert.equal(output.includes('"token"'),false);assert.equal(output.includes('client_request_id'),false);
});
test('unknown commands and excessive message sizes are rejected',async()=>{
 const store=new MemoryStore();const engine=new Engine(store,origin,()=>new FakeAPI(),new DemoProducer());
 const commands=new Commands({} as BrowserAPI,store,engine,new Sessions(store,origin,t=>new Client(origin,t)),origin,'http://127.0.0.1/*');
 await assert.rejects(async()=>commands.run({type:'fetch',url:'https://open.spotify.com/private-api'}));
 await assert.rejects(()=>commands.run({type:'pair',code:'a'.repeat(5000)}));
});
