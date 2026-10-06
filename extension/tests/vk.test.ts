import test from 'node:test';
import assert from 'node:assert/strict';
import { VKAdapter } from '../src/sources/vk/adapter';
import { selectors as s } from '../src/sources/vk/selectors';
import { expectedCount } from '../src/sources/vk/page';
import { fixture, drain, albumPath, playlistPath } from './vk-dom';

test('VK saved tracks read only rendered metadata, not data-audio/data-full-id or cookies', async t => {
  t.mock.method(globalThis, 'fetch', () => { throw new Error('Source network forbidden'); });
  const f = fixture('saved', '/audios42'); t.after(() => f.dom.window.close());
  assert.deepEqual(f.adapter.detectPage(), { source: 'vk', supported: true, adapter: 'ready' });
  assert.equal(f.adapter.getCollectionMetadata()?.kind, 'favorites');
  const result = await drain(f.adapter); assert.equal(result.tracks.length, 2);
  assert.deepEqual(result.tracks[0], { title: 'Север', artists: ['Первый','Второй'], album: 'Берег', duration_ms: 187000, source_track_key: '-42_1', source_url: 'https://vk.com/audio-42_1', position: 0 });
  assert.deepEqual(result.tracks[1]?.artists, ['Ансамбль, оркестр']); assert.equal(result.tracks[1]?.version, 'Live'); assert.equal(result.tracks[1]?.duration_ms, 3723000);
  assert.equal(result.summary.completeness, 'complete');
});
test('VK playlist modal selects foreground collection and strips share/access suffixes', async t => {
  const f = fixture('playlist', '/audios42?z=audio_playlist-42_9_fixtureAccess'); t.after(() => f.dom.window.close());
  assert.equal(f.adapter.getCollectionMetadata()?.key, playlistPath);
  assert.equal(f.adapter.getCollectionMetadata()?.url, 'https://vk.com' + playlistPath);
  const result = await drain(f.adapter);
  assert.deepEqual(result.tracks.map(track => track.title), ['Утро','Ночь']);
  assert.equal(result.tracks[1]?.duration_ms, undefined); assert.equal(result.summary.completeness, 'complete');
});
test('VK album requires visible track list; ID-less tracks are retained as partial', async t => {
  const f = fixture('album', albumPath); t.after(() => f.dom.window.close());
  f.dom.reconfigure({ url: 'https://music.vk.com' + albumPath });
  assert.equal(f.adapter.getCollectionMetadata()?.kind, 'album');
  const result = await drain(f.adapter);
  assert.equal(result.tracks[0]?.album, 'Открытое море'); assert.equal(result.tracks[0]?.source_url, 'https://music.vk.com/music/track/-42_1');
  assert.equal(result.tracks[1]?.source_track_key, undefined); assert.equal(result.summary.completeness, 'partial');
  f.doc.querySelector(s.list)!.remove(); assert.equal(f.adapter.detectPage()?.supported, false); await assert.rejects(() => drain(f.adapter));
});
test('VK playlist is album only when UI explicitly labels it as album', t => {
  const f = fixture('album', playlistPath); t.after(() => f.dom.window.close());
  assert.equal(f.adapter.getCollectionMetadata()?.kind, 'album');
  f.doc.querySelector(s.kind)!.textContent = 'Плейлист'; assert.equal(f.adapter.getCollectionMetadata()?.kind, 'playlist');
  f.doc.querySelector(s.kind)!.remove(); assert.equal(f.adapter.getCollectionMetadata()?.kind, 'playlist');
});
test('VK /music landing, unrelated routes, unknown overlays and wrong hosts fail closed', async t => {
  const f = fixture('saved', '/music'); t.after(() => f.dom.window.close());
  assert.equal(f.adapter.detectPage()?.supported, true);
  f.doc.querySelector('h1')!.textContent = 'Рекомендации'; assert.equal(f.adapter.detectPage()?.supported, false);
  for (const path of ['/feed', '/video', '/music?z=video1_2', '/music?z=audio_playlist-42_9']) {
    f.dom.window.history.pushState({}, '', path); assert.equal(f.adapter.detectPage()?.supported, false); await assert.rejects(() => drain(f.adapter));
  }
  for (const href of ['https://vk.com.evil.example/music','http://vk.com/music','https://user:secret@vk.com/music']) assert.equal(new VKAdapter(new URL(href)).detectPage(), null);
});
test('VK unrelated dialog blocks background capture; track URLs never export arbitrary links', async t => {
  const f = fixture('saved', '/audios42'); t.after(() => f.dom.window.close());
  const link = f.doc.querySelector(s.title)!;
  for (const href of ['https://evil.example/audio1_2', 'https://user:secret@vk.com/audio1_2', '/audio1_2_accessKey', '/artist/1', 'javascript:alert(1)']) {
    link.setAttribute('href', href); assert.equal(f.adapter.collectVisibleTracks()[0]?.source_track_key, undefined);
  }
  const dialog = f.doc.createElement('div'); dialog.setAttribute('role','dialog'); f.doc.body.append(dialog);
  assert.equal(f.adapter.detectPage()?.supported, false); await assert.rejects(() => drain(f.adapter));
});
test('VK exact rendered counts and hidden metadata remain conservative', t => {
  const f = fixture('saved', '/audios42'); t.after(() => f.dom.window.close());
  const count = f.doc.querySelector(s.count)!, root = f.doc.querySelector('main')!;
  for (const [text, expected] of [['1 234 аудиозаписи',1234],['2 songs',2],['1.2K tracks',undefined],['1,23 tracks',undefined]] as const) { count.textContent = text; assert.equal(expectedCount(root), expected); }
  count.setAttribute('hidden', ''); assert.equal(expectedCount(root), undefined);
});
