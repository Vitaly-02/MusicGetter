import type { Track } from '../../core/adapter';
import { inViewport, renderedText } from '../../core/dom';
import { validateTrack } from '../../core/validation';
import { entityLink, firstVisible } from './page';
import { selectors as s } from './selectors';
export interface Observation { track: Track; ordinal?: number; }
export function duration(text: string): number | undefined {
  if (!/^\d{1,3}:\d{2}(?::\d{2})?$/.test(text)) return undefined;
  const parts = text.split(':').map(Number);
  if (parts.slice(1).some(n => n > 59)) return undefined;
  return parts.reduce((n, part) => n * 60 + part, 0) * 1000;
}
export function readTrack(row: Element, url: URL, albumTitle?: string): Observation | null {
  const links = [...row.querySelectorAll(s.link)];
  const trackLink = links.find(link => inViewport(link) && entityLink(link.getAttribute('href') ?? '', url, 'track'));
  const title = renderedText(firstVisible(row, s.title) ?? trackLink ?? null);
  const artists = [...new Set(links.filter(link => entityLink(link.getAttribute('href') ?? '', url, 'artist')).map(renderedText).filter(Boolean))];
  if (!artists.length) artists.push(...[...row.querySelectorAll(s.artistText)].map(renderedText).filter(Boolean));
  if (!title || !artists.length) return null;
  const track: Track = { title, artists, position: 0 };
  const album = links.filter(link => entityLink(link.getAttribute('href') ?? '', url, 'album')).map(renderedText).find(Boolean) || renderedText(firstVisible(row, s.albumText)) || albumTitle;
  if (album) track.album = album;
  // Spotify layouts move duration between columns. Only the last gridcell can be the fallback.
  const cells = [...row.querySelectorAll(s.cells)];
  const ms = duration(renderedText(firstVisible(row, s.duration) ?? cells.at(-1) ?? null));
  if (ms !== undefined) track.duration_ms = ms;
  if (trackLink) {
    const link = entityLink(trackLink.getAttribute('href')!, url, 'track')!;
    track.source_url = link.href; track.source_track_key = link.pathname.split('/')[2]!;
  }
  // aria-rowindex includes header/disc rows. Use aria-posinset only when explicitly supplied.
  const position = Number(row.getAttribute('aria-posinset'));
  const ordinal = Number.isSafeInteger(position) && position > 0 && position <= 100_000 ? position - 1 : undefined;
  if (ordinal !== undefined) track.position = ordinal;
  try { return { track: validateTrack(track), ...(ordinal === undefined ? {} : { ordinal }) }; }
  catch { return null; }
}
export function visibleRows(root: Element): Element[] {
  const list = firstVisible(root, s.list);
  if (!list) return [];
  return [...list.querySelectorAll(s.row)].filter(row => inViewport(row) && list.contains(row.closest(s.list)) && !row.closest(s.excluded));
}
/** Capture-local digest; not the Go canonical fingerprint or an acoustic identity. */
export async function metadataDigest(track: Track): Promise<string> {
  const norm = (value: string): string => value.normalize('NFKC').replace(/\s+/gu, ' ').trim();
  const value = JSON.stringify([norm(track.title), track.artists.map(norm).sort(), norm(track.album ?? ''), track.duration_ms ?? null, norm(track.version ?? '')]);
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(value));
  return [...new Uint8Array(digest)].map(n => n.toString(16).padStart(2, '0')).join('');
}
