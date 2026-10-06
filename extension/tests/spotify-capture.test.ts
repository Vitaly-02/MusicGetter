import test from 'node:test';
import assert from 'node:assert/strict';
import { SpotifyAdapter } from '../src/sources/spotify/adapter';
import { selectors as s } from '../src/sources/spotify/selectors';
import { fixture, virtualized, drain, options, timing, id, albumPath } from './spotify-dom';

test('Spotify virtualized list: rewind, recycled nodes, overlapping scroll, progress and backpressure', async t => {
  const f = virtualized(); t.after(() => f.dom.window.close());
  const progress: number[] = [];
  const result = await drain(f.adapter, { ...options(), onProgress: value => progress.push(value.collected) });
  assert.deepEqual(result.tracks.map(track => track.source_track_key), Array.from({ length: 10 }, (_, i) => id(i + 1)));
  assert.deepEqual(result.tracks.map(track => track.position), Array.from({ length: 10 }, (_, i) => i));
  assert.equal(f.rows[0], f.doc.querySelector(s.row)); assert.equal(result.summary.completeness, 'complete');
  assert.equal(progress.at(-1), 10); assert.ok(progress.every((n, i) => i === 0 || n >= progress[i - 1]!));
  const iterator = f.adapter.collectAllTracks(options()); await iterator.next();
  const scroll = f.viewport.scrollTop;
  await new Promise(resolve => setTimeout(resolve, 10)); assert.equal(f.viewport.scrollTop, scroll);
  await iterator.return({ completeness: 'partial', reason: 'user_stopped', observedCount: 2 });
});
test('Spotify infinite list grows after initial bottom, collector waits and continues', async t => {
  const f = virtualized(true); t.after(() => f.dom.window.close());
  const result = await drain(f.adapter);
  assert.equal(result.tracks.length, 10); assert.equal(result.summary.completeness, 'complete');
  assert.deepEqual(result.tracks.map(track => track.source_track_key), Array.from({ length: 10 }, (_, i) => id(i + 1)));
});
test('Spotify cancellation before start, during wait and after first batch returns partial', async t => {
  const f = virtualized(); t.after(() => f.dom.window.close());
  const controller = new AbortController();
  const result = await drain(f.adapter, { ...options(), signal: controller.signal, onProgress: value => { if (value.collected) controller.abort(); } });
  assert.equal(result.summary.reason, 'user_stopped'); assert.equal(f.viewport.scrollTop, 0);
  assert.equal((await drain(f.adapter, { ...options(), signal: controller.signal })).tracks.length, 0);
  const waiting = new SpotifyAdapter(new URL(f.doc.location.href), f.doc, { ...timing, settleMs: 1000 });
  const cancelWait = new AbortController();
  const iterator = waiting.collectAllTracks({ ...options(), signal: cancelWait.signal });
  const pending = iterator.next(); cancelWait.abort(); const done = await pending;
  assert.equal(done.done, true); if (done.done) assert.equal(done.value.reason, 'user_stopped');
});
test('Spotify SPA navigation, root/list replacement and count change stop capture', async t => {
  for (const change of ['route', 'root', 'list', 'count', 'title']) {
    const f = virtualized(); t.after(() => f.dom.window.close()); let mutated = false;
    const result = await drain(f.adapter, { ...options(), onProgress: value => {
      if (!value.collected || mutated) return; mutated = true;
      if (change === 'route') f.dom.window.history.pushState({}, '', albumPath);
      if (change === 'root') f.doc.querySelector('main')!.replaceWith(f.doc.querySelector('main')!.cloneNode(true));
      if (change === 'list') f.list.replaceWith(f.list.cloneNode(true));
      if (change === 'count') f.doc.querySelector(s.count)!.textContent = '20 songs';
      if (change === 'title') f.doc.querySelector('h1')!.textContent = 'Another collection';
    } });
    assert.equal(result.summary.reason, 'dom_changed', change); assert.ok(result.tracks.length < 10);
  }
});
test('Spotify quiet bottom, mismatched count, loading, missing artists cannot declare complete', async t => {
  for (const mode of ['no-count', 'mismatch', 'loading', 'missing']) {
    const f = fixture('album', albumPath); t.after(() => f.dom.window.close());
    if (mode === 'no-count') f.doc.querySelector(s.count)!.remove();
    if (mode === 'mismatch') f.doc.querySelector(s.count)!.textContent = '3 songs';
    if (mode === 'loading') f.doc.querySelector('main')!.setAttribute('aria-busy', 'true');
    if (mode === 'missing') for (const link of f.doc.querySelector(s.row)!.querySelectorAll('a[href*="/artist/"]')) link.remove();
    assert.equal((await drain(f.adapter)).summary.completeness, 'partial', mode);
  }
});
test('Spotify visible end without count works only when not truncated or hidden', async t => {
  for (const mode of ['visible', 'hidden', 'cap', 'recommendation']) {
    const f = fixture('album', albumPath); t.after(() => f.dom.window.close());
    f.doc.querySelector(s.count)!.remove();
    const end = f.doc.createElement('div'); end.setAttribute('data-testid', 'track-list-end'); end.textContent = 'No more tracks'; end.hidden = mode === 'hidden';
    if (mode === 'recommendation') end.setAttribute('data-testid', 'recommendations');
    f.doc.querySelector(s.list)!.append(end);
    assert.equal((await drain(f.adapter, { ...options(), maxTracks: mode === 'cap' ? 1 : 1000 })).summary.completeness, mode === 'visible' ? 'complete' : 'partial');
  }
});
test('Spotify dedup keeps different IDs with identical metadata, marks changed metadata partial', async t => {
  const f = fixture('album', albumPath); t.after(() => f.dom.window.close());
  const rows = [...f.doc.querySelectorAll(s.row)];
  rows[1]!.querySelector(s.title)!.textContent = 'Morning';
  const result = await drain(f.adapter);
  assert.equal(result.tracks.length, 2); assert.equal(result.summary.completeness, 'complete');
  const duplicate = rows[0]!.cloneNode(true); f.doc.querySelector(s.list)!.append(duplicate);
  assert.equal((await drain(f.adapter)).tracks.length, 2);
  let changed = false;
  const mutation = await drain(f.adapter, { ...options(), onProgress: value => {
    if (value.collected && !changed) { changed = true; rows[0]!.querySelector(s.title)!.textContent = 'Renamed'; }
  } });
  assert.equal(mutation.summary.completeness, 'partial');
});
test('Spotify metadata-only dedup includes album, duration and version qualifiers', async t => {
  const f = fixture('liked', '/collection/tracks'); t.after(() => f.dom.window.close());
  const first = f.doc.querySelector(s.row)!, list = f.doc.querySelector(s.list)!;
  list.append(first.cloneNode(true));
  for (const [selector, value] of [[s.albumText, 'Different album'], [s.duration, '2:03']]) {
    const copy = first.cloneNode(true) as Element; copy.querySelector(selector!)!.textContent = value!; list.append(copy);
  }
  const result = await drain(f.adapter);
  assert.equal(result.tracks.length, 5); assert.equal(result.summary.completeness, 'partial');
});
test('Spotify hidden, clipped, outside viewport and recommendation rows are excluded', t => {
  const f = fixture('album', albumPath); t.after(() => f.dom.window.close());
  const row = f.doc.querySelector(s.row)!;
  for (const style of ['display:none', 'visibility:hidden', 'opacity:0']) {
    row.setAttribute('style', style); assert.equal(f.adapter.collectVisibleTracks().length, 1);
  }
  row.removeAttribute('style'); row.getBoundingClientRect = () => f.rect(10000);
  assert.equal(f.adapter.collectVisibleTracks().length, 1);
  row.getBoundingClientRect = () => f.rect();
  const wrapper = f.doc.createElement('div'); wrapper.style.overflowY = 'hidden'; wrapper.getBoundingClientRect = () => f.rect(0, 0);
  row.replaceWith(wrapper); wrapper.append(row); assert.equal(f.adapter.collectVisibleTracks().length, 1);
});
test('Spotify collector validates limits and allows a new capture after generator return', async t => {
  const f = virtualized(); t.after(() => f.dom.window.close());
  for (const limit of [0, -1, 1.5, 100001]) await assert.rejects(() => drain(f.adapter, { ...options(), maxTracks: limit }));
  const limited = await drain(f.adapter, { ...options(), maxTracks: 3 }); assert.equal(limited.tracks.length, 3); assert.equal(limited.summary.completeness, 'partial');
  const iterator = f.adapter.collectAllTracks(options()); await iterator.next();
  await assert.rejects(() => f.adapter.collectAllTracks(options()).next());
  await iterator.return({ completeness: 'partial', reason: 'user_stopped', observedCount: 2 });
  assert.equal((await drain(f.adapter)).summary.completeness, 'complete');
});
test('Spotify batches obey both 200 track and UTF-8 byte budgets', async t => {
  for (const large of [false, true]) {
    const f = fixture('liked', '/collection/tracks'); t.after(() => f.dom.window.close());
    const list = f.doc.querySelector(s.list)!, template = f.doc.querySelector(s.row)!;
    list.replaceChildren(); const total = large ? 9 : 201;
    for (let n = 0; n < total; n++) {
      const row = template.cloneNode(true) as Element;
      row.querySelector(s.title)!.textContent = (large ? '🎵'.repeat(1000) : 'Song') + n;
      if (large) {
        row.querySelector(s.artistText)!.remove();
        for (let a = 0; a < 32; a++) { const artist = f.doc.createElement('span'); artist.setAttribute('data-testid', 'track-artist'); artist.textContent = '🎵'.repeat(500) + a; row.append(artist); }
      }
      list.append(row);
    }
    const iterator = f.adapter.collectAllTracks(options()); let received = 0, batches = 0;
    for (;;) {
      const next = await iterator.next(); if (next.done) break;
      batches++; received += next.value.tracks.length;
      assert.ok(next.value.tracks.length <= 200); assert.ok(new TextEncoder().encode(JSON.stringify(next.value)).length < 512 * 1024 - 1024);
      if (received === total) { await iterator.return({ completeness: 'partial', reason: 'user_stopped', observedCount: received }); break; }
    }
    assert.equal(received, total); assert.ok(batches >= 2);
  }
});
