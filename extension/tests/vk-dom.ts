import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
import { VKAdapter } from '../src/sources/vk/adapter';
import type { CollectOptions, Track } from '../src/core/adapter';
import { selectors as s } from '../src/sources/vk/selectors';
export const timing = { settleMs: 2, quietPasses: 3, maxSteps: 100 };
export const options = (): CollectOptions => ({ signal: new AbortController().signal, maxTracks: 1000 });
export const id = (n: number): string => `-42_${n}`;
export const playlistPath = '/music/playlist/-42_9';
export const albumPath = '/music/album/-42_9';
export function fixture(name: string, path = playlistPath) {
  // Synthetic layout. jsdom cannot prove compatibility with live browser rendering.
  const dom = new JSDOM(readFileSync(`tests/fixtures/vk/${name}.html`, 'utf8'), { url: `https://vk.com${path}`, pretendToBeVisual: true });
  const { window } = dom;
  const rect = (top = 0, height = 20) => ({ top, bottom: top + height, left: 0, right: 300, width: 300, height, x: 0, y: top, toJSON() {} });
  window.Element.prototype.getBoundingClientRect = function () { return rect(); };
  window.Element.prototype.getClientRects = function () { return [this.getBoundingClientRect()] as unknown as DOMRectList; };
  Object.defineProperties(window.document.documentElement, { clientHeight: { value: 600, configurable: true }, scrollHeight: { value: 600, configurable: true } });
  window.Element.prototype.scrollTo = function (value?: ScrollToOptions | number) { this.scrollTop = typeof value === 'object' ? value.top ?? 0 : 0; };
  const forbidden = () => { throw new Error('Forbidden source access'); };
  Object.defineProperty(window.document, 'cookie', { get: forbidden });
  Object.defineProperties(window, { fetch: { value: forbidden }, XMLHttpRequest: { value: forbidden }, localStorage: { get: forbidden }, sessionStorage: { get: forbidden } });
  for (const node of window.document.querySelectorAll('script')) Object.defineProperty(node, 'textContent', { get: forbidden });
  const originalGetAttribute = window.Element.prototype.getAttribute;
  window.Element.prototype.getAttribute = function (name: string) {
    if (['data-audio', 'data-full-id', 'data-owner-id'].includes(name)) return forbidden();
    return originalGetAttribute.call(this, name);
  };
  const adapter = new VKAdapter(new URL(window.location.href), window.document, timing);
  return { dom, doc: window.document, adapter, rect };
}
export async function drain(adapter: VKAdapter, settings = options()) {
  const tracks: Track[] = [], sizes: number[] = [];
  const iterator = adapter.collectAllTracks(settings);
  for (;;) { const next = await iterator.next(); if (next.done) return { tracks, sizes, summary: next.value }; tracks.push(...next.value.tracks); sizes.push(next.value.tracks.length); }
}
export function virtualized(infinite = false, total = 10) {
  const f = fixture('virtualized', '/audios42');
  const viewport = f.doc.getElementById('viewport')!;
  const list = f.doc.querySelector(s.list)!;
  f.doc.querySelector(s.count)!.textContent = `${total} треков`;
  const rows = [...list.querySelectorAll(s.row)];
  let loaded = infinite ? 4 : total;
  Object.defineProperties(viewport, { clientHeight: { value: 120 }, scrollHeight: { get: () => loaded * 60 } });
  viewport.getBoundingClientRect = () => f.rect(0, 120);
  const render = () => rows.forEach((row, offset) => {
    const index = Math.floor(viewport.scrollTop / 60) + offset;
    row.toggleAttribute('hidden', index >= loaded);
    row.setAttribute('aria-rowindex', String(index + 2));
    const link = row.querySelector(s.title)!;
    link.textContent = `Track ${index}`; link.setAttribute('href', `/audio${id(index + 1)}`);
    row.getBoundingClientRect = () => f.rect(index * 60 - viewport.scrollTop, 60);
    for (const child of row.querySelectorAll('*')) child.getBoundingClientRect = row.getBoundingClientRect;
  });
  viewport.scrollTo = (value?: ScrollToOptions | number) => {
    viewport.scrollTop = Math.min(loaded * 60 - 120, Math.max(0, typeof value === 'object' ? value.top ?? 0 : 0)); render();
    if (infinite && viewport.scrollTop >= loaded * 60 - 120 && loaded < total) {
      list.setAttribute('aria-busy', 'true');
      setTimeout(() => { loaded = Math.min(total, loaded + 3); list.removeAttribute('aria-busy'); render(); }, 5);
    }
  };
  viewport.scrollTop = infinite ? 0 : 240; render();
  return { ...f, viewport, list, rows };
}
