import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
import { YandexAdapter } from '../src/sources/yandex/adapter';
import type { CollectOptions, Track } from '../src/core/adapter';
import { selectors as s } from '../src/sources/yandex/selectors';
import { duration } from '../src/sources/yandex/tracks';
const timing = { settleMs: 2, quietPasses: 3, maxSteps: 100 };
const options = (): CollectOptions => ({ signal: new AbortController().signal, maxTracks: 1000 });
function fixture(name: string, path: string) {
  // jsdom has no layout. Geometry below explicitly models a visible viewport;
  // scripts/resources are disabled, not a claim of browser layout coverage.
  const dom = new JSDOM(readFileSync(`tests/fixtures/yandex/${name}.html`, 'utf8'), { url: `https://music.yandex.ru${path}`, pretendToBeVisual: true });
  const { window } = dom;
  const rect = (top = 0, height = 20) => ({ top, bottom: top + height, left: 0, right: 300, width: 300, height, x: 0, y: top, toJSON() {} });
  window.Element.prototype.getBoundingClientRect = function () { return rect(); };
  window.Element.prototype.getClientRects = function () { return [this.getBoundingClientRect()] as unknown as DOMRectList; };
  const scroll = window.document.documentElement;
  Object.defineProperties(scroll, { clientHeight: { value: 600, configurable: true }, scrollHeight: { value: 600, configurable: true } });
  window.Element.prototype.scrollTo = function (value?: ScrollToOptions | number) { this.scrollTop = typeof value === 'object' ? value.top ?? 0 : 0; };
  const forbidden = () => { throw new Error('Forbidden source access'); };
  Object.defineProperties(window.document, { cookie: { get: forbidden } });
  Object.defineProperties(window, { fetch: { value: forbidden }, XMLHttpRequest: { value: forbidden }, localStorage: { get: forbidden }, sessionStorage: { get: forbidden } });
  for (const node of window.document.querySelectorAll('script')) Object.defineProperty(node, 'textContent', { get: forbidden });
  const adapter = new YandexAdapter(new URL(window.location.href), window.document, timing);
  return { dom, doc: window.document, adapter, rect };
}
async function drain(adapter: YandexAdapter, settings = options()) {
  const tracks: Track[] = [], sizes: number[] = [];
  const iterator = adapter.collectAllTracks(settings);
  for (;;) { const next = await iterator.next(); if (next.done) return { tracks, sizes, summary: next.value }; tracks.push(...next.value.tracks); sizes.push(next.value.tracks.length); }
}
test('playlist extracts rendered metadata, multiple artists, safe link IDs and durations', async t => {
  const { dom, adapter } = fixture('playlist', '/users/public/playlists/4?token=ignored'); t.after(() => dom.window.close());
  assert.deepEqual(adapter.detectPage(), { source: 'yandex', supported: true, adapter: 'ready' });
  assert.equal(adapter.getCollectionMetadata()?.url, 'https://music.yandex.ru/users/public/playlists/4');
  assert.equal(adapter.getCollectionMetadata()?.kind, 'playlist');
  const tracks = adapter.collectVisibleTracks();
  assert.equal(tracks.length, 2);
  assert.deepEqual(tracks[0], { title: 'Север', artists: ['Первый', 'Второй'], album: 'Берег', duration_ms: 187000, source_track_key: '101', source_url: 'https://music.yandex.ru/album/10/track/101', position: 0 });
  assert.equal(tracks[1]?.version, 'Live'); assert.equal(tracks[1]?.duration_ms, 3723000);
  const result = await drain(adapter); assert.equal(result.summary.completeness, 'complete');
});
test('album uses visible heading as album fallback and omits unavailable duration', async t => {
  const { dom, adapter } = fixture('album', '/album/20'); t.after(() => dom.window.close());
  const result = await drain(adapter);
  assert.equal(adapter.getCollectionMetadata()?.kind, 'album');
  assert.equal(result.tracks[1]?.title, 'Ночь'); assert.equal(result.tracks[1]?.album, 'Открытое море');
  assert.deepEqual(result.tracks[1]?.artists, ['Ансамбль']); assert.equal(result.tracks[1]?.duration_ms, undefined);
  assert.equal(result.summary.completeness, 'complete');
});
test('favorites has metadata fallback, preserves versions, skips incomplete rows and stays partial', async t => {
  const { dom, adapter } = fixture('favorites', '/collection/tracks'); t.after(() => dom.window.close());
  const result = await drain(adapter);
  assert.equal(adapter.getCollectionMetadata()?.kind, 'favorites'); assert.equal(result.tracks.length, 2);
  assert.ok(result.tracks.every(track => track.source_track_key === undefined));
  assert.equal(result.summary.completeness, 'partial'); assert.equal(result.summary.reason, 'unknown_end');
});
function virtualized() {
  const f = fixture('virtualized', '/users/public/playlists/10');
  const viewport = f.doc.getElementById('viewport')!;
  const rows = [...f.doc.querySelectorAll(s.row)];
  Object.defineProperties(viewport, { clientHeight: { value: 120 }, scrollHeight: { value: 600 } });
  viewport.getBoundingClientRect = () => f.rect(0, 120);
  const render = () => rows.forEach((row, offset) => {
    const index = Math.floor(viewport.scrollTop / 60) + offset;
    row.toggleAttribute('hidden', index >= 10);
    row.setAttribute('aria-posinset', String(index + 1));
    const link = row.querySelector(s.title)!;
    link.textContent = `Track ${index}`; link.setAttribute('href', `/track/${100 + index}`);
    row.getBoundingClientRect = () => f.rect(index * 60 - viewport.scrollTop, 60);
    for (const child of row.querySelectorAll('*')) child.getBoundingClientRect = row.getBoundingClientRect;
  });
  viewport.scrollTo = (value?: ScrollToOptions | number) => { viewport.scrollTop = Math.min(480, Math.max(0, typeof value === 'object' ? value.top ?? 0 : 0)); render(); };
  viewport.scrollTop = 240; render(); // start in the middle; collector must rewind
  return { ...f, viewport, rows, render };
}
test('virtualized nested scroller reuses only three DOM nodes, dedups overlap and reports progress', async t => {
  const f = virtualized(); t.after(() => f.dom.window.close());
  const progress: number[] = [];
  const result = await drain(f.adapter, { ...options(), onProgress: value => progress.push(value.collected) });
  assert.deepEqual(result.tracks.map(track => track.source_track_key), Array.from({ length: 10 }, (_, i) => String(100 + i)));
  assert.deepEqual(result.tracks.map(track => track.position), Array.from({ length: 10 }, (_, i) => i));
  assert.equal(f.rows[0], f.doc.querySelector(s.row)); assert.equal(result.summary.completeness, 'complete');
  assert.equal(progress.at(-1), 10); assert.ok(progress.every((n, i) => i === 0 || n >= progress[i - 1]!));
});
test('cancel during wait is prompt, partial and stops further scrolling', async t => {
  const f = virtualized(); t.after(() => f.dom.window.close());
  const controller = new AbortController(); let scrolled = 0;
  const original = f.viewport.scrollTo;
  f.viewport.scrollTo = value => { scrolled++; original(typeof value === 'object' ? value : {}); };
  const result = await drain(f.adapter, { ...options(), signal: controller.signal, onProgress: value => { if (value.collected >= 2) controller.abort(); } });
  assert.equal(result.summary.reason, 'user_stopped'); assert.equal(result.summary.completeness, 'partial'); assert.equal(scrolled, 1);
  const preAborted = await drain(f.adapter, { ...options(), signal: controller.signal }); assert.equal(preAborted.tracks.length, 0);
});
test('SPA navigation or root replacement stops capture without mixing collections', async t => {
  for (const replace of [false, true]) {
    const f = virtualized(); t.after(() => f.dom.window.close());
    const result = await drain(f.adapter, { ...options(), onProgress: value => { if (value.collected) { if (replace) f.doc.querySelector('main')!.replaceWith(f.doc.createElement('main')); else f.dom.window.history.pushState({}, '', '/album/999'); } } });
    assert.equal(result.summary.reason, 'dom_changed'); assert.ok(result.tracks.length < 10);
  }
});
test('unknown end, mismatched visible count and loading indicator never become complete', async t => {
  for (const mode of ['missing', 'mismatch', 'busy']) {
    const f = fixture('playlist', '/playlists/public-id'); t.after(() => f.dom.window.close());
    const count = f.doc.querySelector(s.count)!;
    if (mode === 'missing') count.remove();
    if (mode === 'mismatch') count.textContent = '99 треков';
    if (mode === 'busy') f.doc.querySelector(s.list)!.setAttribute('aria-busy', 'true');
    assert.equal((await drain(f.adapter)).summary.completeness, 'partial');
  }
});
test('visible explicit end can confirm collection with no count, but hidden end cannot', async t => {
  for (const hidden of [false, true]) {
    const f = fixture('album', '/album/20'); t.after(() => f.dom.window.close());
    f.doc.querySelector(s.count)!.remove();
    const end = f.doc.createElement('div'); end.className = 'd-track-list__end'; end.textContent = 'Конец списка'; end.hidden = hidden;
    f.doc.querySelector(s.list)!.append(end);
    assert.equal((await drain(f.adapter)).summary.completeness, hidden ? 'partial' : 'complete');
  }
});
test('track limits stay partial and unsupported routes fail closed', async t => {
  const f = virtualized(); t.after(() => f.dom.window.close());
  const result = await drain(f.adapter, { ...options(), maxTracks: 3 });
  assert.equal(result.tracks.length, 3); assert.equal(result.summary.completeness, 'partial');
  f.dom.window.history.pushState({}, '', '/artist/42'); assert.equal(f.adapter.detectPage()?.supported, false);
  await assert.rejects(() => drain(f.adapter));
  await assert.rejects(() => drain(f.adapter, { ...options(), maxTracks: 100001 }));
});
test('observe sees recycled-node text/attributes and dispose disconnects', async t => {
  const f = fixture('album', '/album/20'); t.after(() => f.dom.window.close());
  let calls = 0; const handle = f.adapter.observe(() => calls++);
  f.doc.querySelector(s.title)!.textContent = 'Changed'; await new Promise(resolve => setTimeout(resolve, 0));
  assert.ok(calls > 0); handle.dispose(); const before = calls;
  f.doc.querySelector(s.title)!.textContent = 'Again'; await new Promise(resolve => setTimeout(resolve, 0)); assert.equal(calls, before);
});
test('invalid duration formats never become false precision', () => {
  for (const value of ['live', '3:99', '-1:00', '1:60:00', '3.14']) assert.equal(duration(value), undefined);
  assert.equal(duration('0:00'), 0);
});

test('delayed rendered rows reset end checks instead of sealing an early bottom', async t => {
  const f = fixture('album', '/album/20'); t.after(() => f.dom.window.close());
  const row = f.doc.querySelectorAll(s.row)[1]!; row.remove();
  let scheduled = false;
  const result = await drain(f.adapter, { ...options(), onProgress: value => {
    if (!scheduled && value.phase === 'scrolling') { scheduled = true; setTimeout(() => f.doc.querySelector(s.list)!.append(row), 5); }
  } });
  assert.equal(result.tracks.length, 2); assert.equal(result.summary.completeness, 'complete');
});
test('abort while MutationObserver is waiting releases resources without new batches', async t => {
  const f = fixture('album', '/album/20'); t.after(() => f.dom.window.close());
  const adapter = new YandexAdapter(new URL(f.doc.location.href), f.doc, { ...timing, settleMs: 1000 });
  const controller = new AbortController();
  const iterator = adapter.collectAllTracks({ ...options(), signal: controller.signal });
  const pending = iterator.next(); controller.abort();
  const result = await pending;
  assert.equal(result.done, true); if (result.done) assert.equal(result.value.reason, 'user_stopped');
});
test('unknown markup, unsafe URLs, hidden and out-of-viewport rows are not exported', async t => {
  const f = fixture('playlist', '/users/public/playlists/4'); t.after(() => f.dom.window.close());
  const rows = [...f.doc.querySelectorAll(s.row)];
  rows[1]!.getBoundingClientRect = () => f.rect(10000);
  assert.equal(f.adapter.collectVisibleTracks().length, 1);
  const link = rows[0]!.querySelector(s.title)!;
  for (const href of ['https://music.yandex.ru.evil.example/track/1', 'https://user:secret@music.yandex.ru/track/1', 'javascript:alert(1)', '/artist/1']) {
    link.setAttribute('href', href);
    assert.equal(f.adapter.collectVisibleTracks()[0]?.source_track_key, undefined);
  }
  f.doc.querySelector(s.list)!.className = 'unknown-markup';
  assert.equal(f.adapter.collectVisibleTracks().length, 0);
  await assert.rejects(() => drain(f.adapter));
});
test('metadata-only repeats dedup but distinct albums, versions and duration do not merge', async t => {
  const f = fixture('favorites', '/users/public/tracks'); t.after(() => f.dom.window.close());
  const list = f.doc.querySelector(s.list)!;
  const first = list.querySelector(s.row)!;
  list.append(first.cloneNode(true));
  const differentAlbum = first.cloneNode(true) as Element;
  const album = f.doc.createElement('span'); album.className = 'CommonTrack_album__fixture';
  const link = f.doc.createElement('a'); link.textContent = 'Другой альбом'; album.append(link); differentAlbum.append(album); list.append(differentAlbum);
  const differentDuration = first.cloneNode(true) as Element;
  differentDuration.querySelector(s.duration)!.textContent = '2:03'; list.append(differentDuration);
  const result = await drain(f.adapter);
  assert.equal(result.tracks.length, 4); assert.equal(result.summary.completeness, 'partial');
});
test('same source ID changing metadata is marked partial, not silently complete', async t => {
  const f = fixture('album', '/album/20'); t.after(() => f.dom.window.close());
  let mutated = false;
  const result = await drain(f.adapter, { ...options(), onProgress: value => {
    if (!mutated && value.collected > 0) { mutated = true; f.doc.querySelector(s.title)!.textContent = 'Renamed'; }
  } });
  assert.equal(result.tracks.length, 2); assert.equal(result.summary.completeness, 'partial');
});
test('concurrent capture rejected; generator return releases capture lock', async t => {
  const f = fixture('album', '/album/20'); t.after(() => f.dom.window.close());
  const iterator = f.adapter.collectAllTracks(options()); await iterator.next();
  await assert.rejects(() => f.adapter.collectAllTracks(options()).next());
  await iterator.return({ completeness: 'partial', reason: 'user_stopped', observedCount: 2 });
  assert.equal((await drain(f.adapter)).summary.completeness, 'complete');
});

test('large visible batch is streamed in at most 200 tracks and below the wire byte limit', async t => {
  const f = fixture('album', '/album/20'); t.after(() => f.dom.window.close());
  const list = f.doc.querySelector(s.list)!, original = list.querySelector(s.row)!;
  list.replaceChildren();
  for (let i = 0; i < 205; i++) {
    const row = original.cloneNode(true) as Element;
    row.querySelector(s.title)!.setAttribute('href', `/track/${1000 + i}`); list.append(row);
  }
  f.doc.querySelector(s.count)!.textContent = '205 tracks';
  const iterator = f.adapter.collectAllTracks(options());
  const first = await iterator.next(); assert.equal(first.done, false);
  if (!first.done) { assert.equal(first.value.tracks.length, 200); assert.ok(new TextEncoder().encode(JSON.stringify(first.value)).length < 512 * 1024 - 1024); }
  const second = await iterator.next(); if (!second.done) assert.equal(second.value.tracks.length, 5);
  await iterator.return({ completeness: 'partial', reason: 'user_stopped', observedCount: 205 });
});
test('recommendations inside the page root are excluded from collection tracks', async t => {
  const f = fixture('album', '/album/20'); t.after(() => f.dom.window.close());
  const aside = f.doc.createElement('aside');
  aside.append(f.doc.querySelector(s.list)!.cloneNode(true));
  f.doc.querySelector('main')!.append(aside);
  assert.equal(f.adapter.collectVisibleTracks().length, 2);
  assert.equal((await drain(f.adapter)).summary.completeness, 'complete');
});


test('track cap with an explicit end but no count cannot seal omitted rows as complete', async t => {
  const f = fixture('album', '/album/20'); t.after(() => f.dom.window.close());
  f.doc.querySelector(s.count)!.remove();
  const end = f.doc.createElement('div'); end.className = 'd-track-list__end'; end.textContent = 'Конец списка';
  f.doc.querySelector(s.list)!.append(end);
  const result = await drain(f.adapter, { ...options(), maxTracks: 1 });
  assert.equal(result.tracks.length, 1); assert.equal(result.summary.completeness, 'partial');
});
