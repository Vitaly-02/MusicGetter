import test from 'node:test';
import assert from 'node:assert/strict';
import { VKAdapter } from '../src/sources/vk/adapter';
import { selectors as s } from '../src/sources/vk/selectors';
import { fixture, virtualized, drain, options, timing, id } from './vk-dom';

test('VK recycled rows and growing infinite list dedup across overlapping scroll', async t => {
  for (const infinite of [false,true]) {
    const f = virtualized(infinite); t.after(() => f.dom.window.close());
    const progress: number[] = [];
    const result = await drain(f.adapter, { ...options(), onProgress: value => progress.push(value.collected) });
    assert.deepEqual(result.tracks.map(track => track.source_track_key), Array.from({ length: 10 }, (_, i) => id(i+1)));
    assert.equal(result.summary.completeness, 'complete'); assert.equal(f.rows[0], f.doc.querySelector(s.row));
    assert.equal(progress.at(-1), 10); assert.ok(progress.every((n,i) => i === 0 || n >= progress[i-1]!));
  }
});
test('VK traverses 1,200 virtual tracks while retaining only three rendered row nodes', async t => {
  const f = virtualized(false, 1200); t.after(() => f.dom.window.close());
  const result = await drain(new VKAdapter(new URL(f.doc.location.href), f.doc, { ...timing, maxSteps: 1200 }), { ...options(), maxTracks: 2000 });
  assert.equal(result.tracks.length, 1200); assert.equal(new Set(result.tracks.map(track => track.source_track_key)).size, 1200);
  assert.equal(f.doc.querySelectorAll(s.row).length, 3); assert.equal(result.summary.completeness, 'complete');
  assert.ok(result.sizes.every(n => n <= 200));
});
test('VK cancellation before start/during wait/after batch prevents further scrolling', async t => {
  const f = virtualized(); t.after(() => f.dom.window.close()); const controller = new AbortController();
  const result = await drain(f.adapter, { ...options(), signal: controller.signal, onProgress: value => { if (value.collected) controller.abort(); } });
  assert.equal(result.summary.reason, 'user_stopped'); assert.equal(f.viewport.scrollTop, 0);
  assert.equal((await drain(f.adapter, { ...options(), signal: controller.signal })).tracks.length, 0);
  const waiting = new VKAdapter(new URL(f.doc.location.href), f.doc, { ...timing, settleMs: 1000 });
  const cancel = new AbortController(); const pending = waiting.collectAllTracks({ ...options(), signal: cancel.signal }).next(); cancel.abort();
  const done = await pending; assert.equal(done.done, true); if (done.done) assert.equal(done.value.reason, 'user_stopped');
});
test('VK query-only modal navigation, list replacement and changed title/count abort collection', async t => {
  for (const change of ['query','list','title','count']) {
    const f = fixture('playlist', '/audios42?z=audio_playlist-42_9'); t.after(() => f.dom.window.close()); let mutated = false;
    const result = await drain(f.adapter, { ...options(), onProgress: value => {
      if (!value.collected || mutated) return; mutated = true;
      const dialog = f.doc.querySelector('[role="dialog"]')!;
      if (change === 'query') f.dom.window.history.pushState({}, '', '/audios42?z=audio_playlist-42_10');
      if (change === 'list') { const list = dialog.querySelector(s.list)!; list.replaceWith(list.cloneNode(true)); }
      if (change === 'title') dialog.querySelector(s.heading)!.textContent = 'Другой';
      if (change === 'count') dialog.querySelector(s.count)!.textContent = '3 трека';
    } });
    assert.equal(result.summary.reason, 'dom_changed', change);
  }
});
test('VK end requires evidence; unknown count/loading/incomplete metadata stay partial', async t => {
  for (const mode of ['unknown','loading','missing','mismatch']) {
    const f = fixture('saved', '/audios42'); t.after(() => f.dom.window.close());
    if (mode === 'unknown') f.doc.querySelector(s.count)!.remove();
    if (mode === 'mismatch') f.doc.querySelector(s.count)!.textContent = '99 треков';
    if (mode === 'loading') f.doc.querySelector('main')!.setAttribute('aria-busy','true');
    if (mode === 'missing') f.doc.querySelector(s.artistText)!.remove();
    assert.equal((await drain(f.adapter)).summary.completeness, 'partial');
  }
});
test('VK explicit end cannot override cap or hidden marker', async t => {
  for (const mode of ['visible','cap','hidden']) {
    const f = fixture('saved', '/audios42'); t.after(() => f.dom.window.close()); f.doc.querySelector(s.count)!.remove();
    const end = f.doc.createElement('div'); end.className = 'audio_page__list_end'; end.textContent = 'Конец списка'; end.hidden = mode === 'hidden'; f.doc.querySelector(s.list)!.append(end);
    assert.equal((await drain(f.adapter, { ...options(), maxTracks: mode === 'cap' ? 1 : 1000 })).summary.completeness, mode === 'visible' ? 'complete' : 'partial');
  }
});
test('VK ID-less dedup preserves distinct metadata and known IDs remain distinct', async t => {
  const f = fixture('album', '/music/album/-42_9'); t.after(() => f.dom.window.close());
  const list = f.doc.querySelector(s.list)!, row = list.querySelectorAll(s.row)[1]!;
  list.append(row.cloneNode(true));
  const different = row.cloneNode(true) as Element; different.querySelector(s.duration)!.textContent = '2:03'; list.append(different);
  const result = await drain(f.adapter); assert.equal(result.tracks.length, 3); assert.equal(result.summary.completeness, 'partial');
});
test('VK consumer backpressure, concurrent capture lock and observer cleanup', async t => {
  const f = virtualized(); t.after(() => f.dom.window.close());
  const iterator = f.adapter.collectAllTracks(options()); await iterator.next(); const top = f.viewport.scrollTop;
  await new Promise(resolve => setTimeout(resolve,10)); assert.equal(f.viewport.scrollTop,top);
  await assert.rejects(() => f.adapter.collectAllTracks(options()).next());
  await iterator.return({ completeness:'partial',reason:'user_stopped',observedCount:2 });
  let calls = 0; const observer = f.adapter.observe(() => calls++);
  f.doc.querySelector(s.title)!.textContent = 'Changed'; await new Promise(resolve => setTimeout(resolve,0)); assert.ok(calls > 0);
  observer.dispose(); const before = calls; f.doc.querySelector(s.title)!.textContent = 'Again'; await new Promise(resolve => setTimeout(resolve,0)); assert.equal(calls,before);
  assert.equal((await drain(f.adapter)).summary.completeness,'complete');
});
test('VK limits bound capture and reject invalid options', async t => {
  const f = virtualized(); t.after(() => f.dom.window.close());
  for (const maxTracks of [0,1.5,100001]) await assert.rejects(() => drain(f.adapter,{ ...options(),maxTracks }));
  const result = await drain(f.adapter,{ ...options(),maxTracks:3 }); assert.equal(result.tracks.length,3); assert.equal(result.summary.completeness,'partial');
});
