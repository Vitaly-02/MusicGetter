import type { CollectionMetadata } from '../../core/adapter';
import { rendered, renderedText } from '../../core/dom';
import { selectors as s } from './selectors';
export const hosts = ['vk.com', 'music.vk.com'];
export function trustedURL(value: string, base: URL): URL | null {
  try {
    const url = new URL(value, base);
    return url.protocol === 'https:' && hosts.includes(url.hostname) && !url.username && !url.password && !url.port ? url : null;
  } catch { return null; }
}
/** Parse only user-facing navigation. Optional share/access suffixes are discarded, never exported. */
function collectionPath(path: string): { key: string; kind: 'playlist' | 'album' } | null {
  const match = /^(?:\/music\/(playlist|album)\/|\/(audio_playlist))(-?\d+_\d+)(?:_[a-zA-Z0-9-]+)?\/?$/.exec(path);
  if (!match || match[3]!.length > 80) return null;
  const kind = match[1] === 'album' ? 'album' : 'playlist';
  return { key: `/music/${kind}/${match[3]}`, kind };
}
export function route(url: URL): { key: string; kind: CollectionMetadata['kind'] } | null {
  if (!trustedURL(url.href, url)) return null;
  // VK can open a collection as a user-visible modal without changing the base pathname.
  const overlay = url.searchParams.get('z');
  if (overlay !== null) return collectionPath('/' + overlay.replace(/^\//, ''));
  const collection = collectionPath(url.pathname);
  if (collection) return collection;
  if (/^\/audios-?\d+\/?$/.test(url.pathname)) return { key: url.pathname.replace(/\/$/, ''), kind: 'favorites' };
  if (/^\/music\/?$/.test(url.pathname)) return { key: '/music', kind: 'favorites' };
  return null;
}
export function trackURL(value: string, base: URL): URL | null {
  const url = trustedURL(value, base);
  if (!url) return null;
  const match = /^\/(?:audio|music\/track\/)(-?\d+_\d+)\/?$/.exec(url.pathname);
  if (!match || match[1]!.length > 80) return null;
  url.search = ''; url.hash = '';
  return url;
}
export function firstVisible(root: ParentNode, selector: string): Element | null {
  return [...root.querySelectorAll(selector)].find(node => rendered(node) && !node.closest(s.excluded)) ?? null;
}
export function pageRoot(doc: Document): Element | null {
  const dialogs = [...doc.querySelectorAll(s.dialog)].filter(rendered);
  // Never collect the background page under an unrelated or unrecognized modal.
  if (dialogs.length) {
    const dialog = dialogs.at(-1)!;
    return firstVisible(dialog, s.list) ? dialog : null;
  }
  return firstVisible(doc, s.page);
}
export function metadata(doc: Document, url: URL): CollectionMetadata | null {
  const page = route(url), root = pageRoot(doc);
  if (!page || !root || !firstVisible(root, s.list)) return null;
  const title = renderedText(firstVisible(root, s.heading));
  if (!title || [...title].length > 1024) return null;
  if (url.searchParams.has('z') && !root.matches(s.dialog)) return null;
  if (page.kind === 'favorites') {
    const selected = renderedText(firstVisible(root, s.activeTab));
    if ((selected || page.key === '/music') && !/^(?:Моя музыка|Сохранённые треки|My music|Saved tracks)$/iu.test(selected || title)) return null;
  }
  // A modal over /audios without a navigable collection identity must not become favorites.
  if (root.matches(s.dialog) && page.kind === 'favorites') return null;
  const label = renderedText(firstVisible(root, s.kind));
  const kind = page.kind === 'playlist' && /^(?:Альбом|Album)$/iu.test(label) ? 'album' : page.kind;
  return { ...page, kind, title, provisional: page.kind === 'favorites', url: new URL(page.key, url.origin).href };
}
export function expectedCount(root: Element): number | undefined {
  const text = renderedText(firstVisible(root, s.count));
  const match = /^(\d+|\d{1,3}(?:[,\s]\d{3})+)\s*(?:трек(?:а|ов)?|аудиозапис(?:ь|и|ей)|пес(?:ня|ни|ен)|tracks?|songs?)$/iu.exec(text);
  if (!match) return undefined;
  const value = Number(match[1]!.replace(/[,\s]/gu, ''));
  return Number.isSafeInteger(value) && value <= 100_000 ? value : undefined;
}
export function scrollContainer(list: Element): HTMLElement {
  const doc = list.ownerDocument;
  for (let node: Element | null = list; node; node = node.parentElement) {
    const style = doc.defaultView!.getComputedStyle(node);
    if (/(auto|scroll)/.test(style.overflowY || style.overflow) && node.clientHeight > 0) return node as HTMLElement;
  }
  return (doc.scrollingElement ?? doc.documentElement) as HTMLElement;
}
