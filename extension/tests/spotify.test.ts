import test from 'node:test';
import assert from 'node:assert/strict';
import { SpotifyAdapter } from '../src/sources/spotify/adapter';
import { duration } from '../src/sources/spotify/tracks';
import { expectedCount } from '../src/sources/spotify/page';
import { selectors as s } from '../src/sources/spotify/selectors';
import { fixture, drain, id, albumPath, playlistPath } from './spotify-dom';

test('Spotify playlist: rendered metadata, artists, album, duration, canonical DOM link', async t => {
  t.mock.method(globalThis, 'fetch', () => { throw new Error('No source network'); });
  const f = fixture('playlist', '/intl-de' + playlistPath + '?si=ignore'); t.after(() => f.dom.window.close());
  assert.deepEqual(f.adapter.detectPage(), { source: 'spotify', supported: true, adapter: 'ready' });
  assert.equal(f.adapter.getCollectionMetadata()?.url, 'https://open.spotify.com' + playlistPath);
  assert.equal(f.adapter.getCollectionMetadata()?.kind, 'playlist');
  const tracks = f.adapter.collectVisibleTracks();
  assert.equal(tracks.length, 2);
  assert.deepEqual(tracks[0], { title: 'North', artists: ['First Artist', 'Second Artist'], album: 'Coast', duration_ms: 187000, source_track_key: id(1), source_url: 'https://open.spotify.com/track/' + id(1), position: 0 });
  assert.equal(tracks[1]?.title, 'North - Live'); assert.equal(tracks[1]?.duration_ms, 299000);
  assert.equal((await drain(f.adapter)).summary.completeness, 'complete');
});
test('Spotify album: disc headers are not tracks or positions; visible album heading fallback', async t => {
  const f = fixture('album', albumPath); t.after(() => f.dom.window.close());
  const result = await drain(f.adapter);
  assert.equal(f.adapter.getCollectionMetadata()?.kind, 'album');
  assert.deepEqual(result.tracks.map(track => track.position), [0, 1]);
  assert.equal(result.tracks[0]?.title, 'Morning'); assert.deepEqual(result.tracks[0]?.artists, ['Ensemble']);
  assert.equal(result.tracks[1]?.album, 'Open sea'); assert.equal(result.tracks[1]?.duration_ms, undefined);
  assert.equal(result.summary.completeness, 'complete');
});
test('Spotify Liked Songs: ID-less/local metadata is retained, inaccessible hidden text is excluded', async t => {
  const f = fixture('liked', '/collection/tracks'); t.after(() => f.dom.window.close());
  const result = await drain(f.adapter);
  assert.equal(f.adapter.getCollectionMetadata()?.kind, 'favorites'); assert.equal(result.tracks.length, 3);
  assert.equal(result.tracks[0]?.source_track_key, undefined); assert.equal(result.tracks[0]?.album, 'Home');
  assert.equal(result.tracks[2]?.source_track_key, id(3)); assert.equal(result.summary.completeness, 'partial');
});
test('Spotify detection fails closed for other hosts, schemes, pages and unknown markup', async t => {
  const f = fixture('playlist'); t.after(() => f.dom.window.close());
  for (const href of ['https://open.spotify.com.evil.example/collection/tracks', 'http://open.spotify.com/collection/tracks', 'https://user:secret@open.spotify.com/collection/tracks']) assert.equal(new SpotifyAdapter(new URL(href)).detectPage(), null);
  for (const path of ['/search', '/artist/' + id(1), '/episode/' + id(1), '/embed/playlist/' + id(1)]) {
    f.dom.window.history.pushState({}, '', path); assert.equal(f.adapter.detectPage()?.supported, false); await assert.rejects(() => drain(f.adapter));
  }
  f.dom.window.history.pushState({}, '', playlistPath);
  f.doc.querySelector(s.list)!.remove(); assert.equal(f.adapter.detectPage()?.supported, false); await assert.rejects(() => drain(f.adapter));
});
test('Spotify URL allowlist rejects credentials, other hosts, non-track and non-HTTP IDs', t => {
  const f = fixture('playlist'); t.after(() => f.dom.window.close());
  const title = f.doc.querySelector(s.title)!;
  for (const href of ['https://open.spotify.com.evil.example/track/' + id(1), 'https://user:secret@open.spotify.com/track/' + id(1), 'spotify:track:' + id(1), '/episode/' + id(1), '/track/invalid']) {
    title.setAttribute('href', href); assert.equal(f.adapter.collectVisibleTracks()[0]?.source_track_key, undefined);
  }
});
test('Spotify exact visible counts; ARIA grid total never substitutes for a song count', t => {
  const f = fixture('liked', '/collection/tracks'); t.after(() => f.dom.window.close());
  const root = f.doc.querySelector('main')!, count = f.doc.querySelector(s.count)!;
  for (const [value, expected] of [['1,234 songs',1234], ['1 234 трека',1234], ['0 songs',0], ['1.2K songs',undefined], ['2 likes',undefined], ['1,23 songs',undefined]] as const) {
    count.textContent = value; assert.equal(expectedCount(root), expected);
  }
  count.remove(); assert.equal(expectedCount(root), undefined);
});
test('Spotify duration parser rejects invalid values instead of guessing', () => {
  assert.equal(duration('1:02:03'), 3723000); assert.equal(duration('0:00'), 0);
  for (const value of ['Live', '—', '3:60', '-2:00', '1:99:00']) assert.equal(duration(value), undefined);
});
test('Spotify observe reacts to recycled text and releases observer on dispose', async t => {
  const f = fixture('playlist'); t.after(() => f.dom.window.close());
  let calls = 0; const handle = f.adapter.observe(() => calls++);
  f.doc.querySelector(s.title)!.textContent = 'New'; await new Promise(resolve => setTimeout(resolve, 0)); assert.ok(calls > 0);
  handle.dispose(); const before = calls;
  f.doc.querySelector(s.title)!.textContent = 'Again'; await new Promise(resolve => setTimeout(resolve, 0)); assert.equal(calls, before);
});
