import type { CollectionMetadata } from '../../core/adapter';
import { rendered, renderedText } from '../../core/dom';
import { selectors as s } from './selectors';
export const hosts = ['music.yandex.ru', 'music.yandex.com', 'music.yandex.kz'];
export function safeURL(value: string, base: URL): URL | null {
  try {
    const url = new URL(value, base);
    if (url.protocol !== 'https:' || !hosts.includes(url.hostname) || url.username || url.password || url.port) return null;
    url.search = ''; url.hash = ''; return url;
  } catch { return null; }
}
export function route(url: URL): { key: string; kind: CollectionMetadata['kind'] } | null {
  if (!safeURL(url.href, url)) return null;
  const path = url.pathname.replace(/\/$/, '');
  if (/^\/users\/[^/]+\/playlists\/\d+$/.test(path) || /^\/playlists\/[a-zA-Z0-9-]+$/.test(path)) return { key: path, kind: 'playlist' };
  if (/^\/album\/\d+$/.test(path)) return { key: path, kind: 'album' };
  if (/^\/users\/[^/]+\/tracks$/.test(path) || /^\/collection\/(tracks|liked-tracks)$/.test(path)) return { key: path, kind: 'favorites' };
  return null;
}
export function firstVisible(root: ParentNode, selector: string): Element | null {
  return [...root.querySelectorAll(selector)].find(rendered) ?? null;
}
export function pageRoot(doc: Document): Element | null { return firstVisible(doc, s.page); }
export function metadata(doc: Document, url: URL): CollectionMetadata | null {
  const page = route(url), root = pageRoot(doc);
  if (!page || !root) return null;
  const title = renderedText(firstVisible(root, s.heading));
  if (!title || [...title].length > 1024) return null;
  return { ...page, title, provisional: page.kind === 'favorites', url: safeURL(url.href, url)!.href };
}
export function expectedCount(root: Element): number | undefined {
  const text = renderedText(firstVisible(root, s.count));
  const match = /^(\d[\d\s\u00a0]*)\s*(?:трек(?:а|ов)?|пес(?:ня|ни|ен)|tracks?|songs?)$/iu.exec(text);
  if (!match) return undefined;
  const count = Number(match[1]!.replace(/\s/gu, ''));
  return Number.isSafeInteger(count) && count <= 100_000 ? count : undefined;
}
export function scrollContainer(list: Element): HTMLElement {
  const doc = list.ownerDocument;
  for (let node: Element | null = list; node; node = node.parentElement) {
    const style = doc.defaultView!.getComputedStyle(node);
    if (/(auto|scroll)/.test(style.overflowY || style.overflow) && node.clientHeight > 0) return node as HTMLElement;
  }
  return (doc.scrollingElement ?? doc.documentElement) as HTMLElement;
}
