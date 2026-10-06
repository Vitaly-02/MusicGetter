import test from 'node:test';
import assert from 'node:assert/strict';
import { Client } from '../src/api/client';
import { Sessions } from '../src/core/session';
import { AppError } from '../src/core/errors';
import { MemoryStore, origin, session, importID } from './helpers';

test('HTTP transports only own bearer, without cookies/referrer/redirects',async()=>{
 const requests:Array<{url:string;options:RequestInit}>=[];
 const fetcher:typeof fetch=async(url,options)=>{requests.push({url:String(url),options:options!});return new Response(JSON.stringify({id:session.owner,telegram_user_id:42}),{status:200});};
 const client=new Client(origin,session.token,fetcher);await client.me();
 assert.equal(requests[0]?.url,origin+'/v1/me');const opts=requests[0]!.options;
 assert.equal(opts.credentials,'omit');assert.equal(opts.redirect,'error');assert.equal(opts.referrerPolicy,'no-referrer');assert.equal((opts.headers as Record<string,string>).Authorization,'Bearer '+session.token);
});
test('API honors 429 and hides backend response/transport secrets',async()=>{
 const client=new Client(origin,session.token,async()=>new Response('SECRET',{status:429,headers:{'Retry-After':'120'}}));
 await assert.rejects(()=>client.me(),(error:unknown)=>error instanceof AppError&&error.retryable&&error.retryAfterMs===120000&&!error.message.includes('SECRET'));
 const offline=new Client(origin,session.token,async()=>{throw Error('SECRET credential URL');});
 await assert.rejects(()=>offline.me(),(error:unknown)=>error instanceof AppError&&error.code==='network'&&error.message==='network');
});
test('one-time claim has no Authorization and is not retried automatically',async()=>{
 const store=new MemoryStore();store.j=undefined;let attempts=0;
 const auth=new Sessions(store,origin,token=>new Client(origin,token,async(_url,options)=>{attempts++;assert.equal((options?.headers as Record<string,string>).Authorization,undefined);throw Error('lost ACK');}));
 await assert.rejects(()=>auth.pair('A'.repeat(26)),(error:unknown)=>error instanceof AppError&&error.code==='pairing_uncertain');assert.equal(attempts,1);assert.deepEqual(await store.session(),session);
});
test('pairing validates owner before storing token; reconnect 401 clears only current credential',async()=>{
 const store=new MemoryStore();store.j=undefined;
 const auth=new Sessions(store,origin,token=>new Client(origin,token,async(url)=>new Response(JSON.stringify(String(url).endsWith('/claim')?{token:session.token,expires_at:session.expiresAt}:{id:session.owner,telegram_user_id:42}),{status:200})));
 await auth.pair('a'.repeat(26));assert.deepEqual(await store.session(),session);
 const revoked=new Sessions(store,origin,token=>new Client(origin,token,async()=>new Response('',{status:401})));
 await assert.rejects(()=>revoked.reconnect());assert.equal(await store.session(),undefined);
});
test('bad ACK never clears a pending batch',async()=>{
 const client=new Client(origin,session.token,async()=>new Response(JSON.stringify({sequence:99,idempotency_key:'wrong',received:1,added:1}),{status:200}));
 await assert.rejects(()=>client.append(importID,{schema_version:1,idempotency_key:'right',sequence:0,tracks:[{title:'Song',artists:['A'],position:0}]},new AbortController().signal));
});
