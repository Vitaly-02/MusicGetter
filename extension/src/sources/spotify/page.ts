import type { CollectionMetadata } from '../../core/adapter';
import { rendered, renderedText } from '../../core/dom';
import { selectors as s } from './selectors';
export const hosts = ['open.spotify.com'];
export function safeURL(value: string, base: URL): URL | null {
  try {
    const url = new URL(value, base);
    if (url.protocol !== 'https:' || !hosts.includes(url.hostname) || url.username || url.password || url.port) return null;
    url.search = ''; url.hash = '';
    url.pathname = url.pathname.replace(/^\/intl-[a-z]{2}(?:-[a-z]{2})?(?=\/)/i, '');
    return url;
  } catch { return null; }
}
export function entityLink(value: string, base: URL, kind: 'track' | 'album' | 'artist'): URL | null {
  const url = safeURL(value, base);
  return url && new RegExp(`^/${kind}/[a-zA-Z0-9]{22}/?$`).test(url.pathname) ? url : null;
}
export function route(url: URL): { key: string; kind: CollectionMetadata['kind'] } | null {
  const safe = safeURL(url.href, url);
  if (!safe) return null;
  const path = safe.pathname.replace(/\/$/, '');
  if (path === '/collection/tracks') return { key: path, kind: 'favorites' };
  const match = /^\/(playlist|album)\/[a-zA-Z0-9]{22}$/.exec(path);
  return match ? { key: path, kind: match[1] as 'playlist' | 'album' } : null;
}
export function firstVisible(root: ParentNode, selector: string): Element | null {
  return [...root.querySelectorAll(selector)].find(node => rendered(node) && !node.closest(s.excluded)) ?? null;
}
export function pageRoot(doc: Document): Element | null { return firstVisible(doc, s.page) ?? firstVisible(doc, s.main); }
export function metadata(doc: Document, url: URL): CollectionMetadata | null {
  const page = route(url), root = pageRoot(doc);
  if (!page || !root) return null;
  const title = renderedText(firstVisible(root, s.heading));
  if (!title || [...title].length > 1024) return null;
  return { ...page, title, provisional: page.kind === 'favorites', url: safeURL(url.href, url)!.href };
}
export function expectedCount(root: Element): number | undefined {
  for (const node of root.querySelectorAll(s.count)) {
    if (node.closest(s.excluded)) continue;
    const text = renderedText(node);
    // English/Russian exact counters; never use grid aria-rowcount (includes headers).
    const match = /^(\d+|\d{1,3}(?:[,\s]\d{3})+)\s*(?:songs?|tracks?|трек(?:а|ов)?|пес(?:ня|ни|ен))$/iu.exec(text);
    if (!match) continue;
    const value = Number(match[1]!.replace(/[,\s]/gu, ''));
    if (Number.isSafeInteger(value) && value <= 100_000) return value;
  }
  return undefined;
}
export function scrollContainer(list: Element): HTMLElement {
  const doc = list.ownerDocument;
  for (let node: Element | null = list; node; node = node.parentElement) {
    const style = doc.defaultView!.getComputedStyle(node);
    if (/(auto|scroll)/.test(style.overflowY || style.overflow) && node.clientHeight > 0) return node as HTMLElement;
  }
  return (doc.scrollingElement ?? doc.documentElement) as HTMLElement;
}
