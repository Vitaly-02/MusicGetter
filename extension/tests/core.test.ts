import test from 'node:test';
import assert from 'node:assert/strict';
import { chunkTracks, jsonBytes, MAX_BYTES, validateTrack } from '../src/core/validation';
import { adapterFor } from '../src/sources';
import { trustedPopup } from '../src/background/commands';
import { retryDelay } from '../src/core/retry';
import { AppError } from '../src/core/errors';

test('large Unicode metadata split by bytes, not only count',async()=>{
 async function* tracks(){for(let i=0;i<201;i++)yield {title:'🎵'.repeat(1000),artists:Array(32).fill('🎵'.repeat(500)) as string[],position:i};}
 let count=0,batches=0;for await(const c of chunkTracks(tracks())){count+=c.tracks.length;batches++;assert.ok(c.tracks.length<=200);assert.ok(jsonBytes(c)<=MAX_BYTES);}
 assert.equal(count,201);assert.ok(batches>2);
});
test('credential fields, unsafe links and metadata rejected',()=>{
 const good={title:'Song',artists:['Artist'],position:0};
 for(const value of [{...good,cookies:'s'},{...good,access_token:'s'},{...good,source_url:'https://open.spotify.com/track/1?token=s'},{...good,source_url:'https://user:secret@open.spotify.com/track/1'},{...good,source_url:'https://evil.example/track/1'},{...good,duration_ms:-1},{...good,position:Infinity}])assert.throws(()=>validateTrack(value));
});
test('source detection is exact and stubs never claim a complete collection',async()=>{
 for(const [url,source] of [['https://open.spotify.com/collection/tracks','spotify'],['https://music.yandex.ru/users/x','yandex'],['https://vk.com/music','vk']]){
 const adapter=adapterFor(new URL(url!));assert.equal(adapter?.detectPage()?.source,source);assert.equal(adapter?.detectPage()?.supported,false);assert.equal(adapter?.getCollectionMetadata(),null);assert.deepEqual(adapter?.collectVisibleTracks(),[]);
 await assert.rejects(()=>adapter!.collectAllTracks({signal:new AbortController().signal,maxTracks:200}).next());
 }
 assert.equal(adapterFor(new URL('https://open.spotify.com.evil.example/')),undefined);
 assert.equal(adapterFor(new URL('http://open.spotify.com/')),undefined);
});
test('only exact popup sender can request session operations',()=>{
 const browser={runtime:{id:'own',getURL:(p:string)=>'chrome-extension://own/'+p}} as Parameters<typeof trustedPopup>[1];
 assert.equal(trustedPopup({id:'own',url:'chrome-extension://own/popup.html'},browser),true);
 for(const sender of [{id:'other',url:'chrome-extension://own/popup.html'},{id:'own',url:'https://open.spotify.com/'},{id:'own',url:'chrome-extension://own/popup.html',tab:{id:1}},{id:'own',url:'chrome-extension://own/popup.html?x=1'}])assert.equal(trustedPopup(sender,browser),false);
});
test('backoff respects server delay and caps exponential growth',()=>{
 assert.equal(retryDelay(new AppError('network',true,120000),0,()=>0),120000);
 assert.ok(retryDelay(new AppError('network',true),20,()=>0.5)<=60000);
});
