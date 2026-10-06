import type { Track } from '../../core/adapter';
import { inViewport, renderedText } from '../../core/dom';
import { validateTrack } from '../../core/validation';
import { firstVisible, safeURL } from './page';
import { selectors as s } from './selectors';
export interface Observation { track: Track; ordinal?: number; }
export function duration(text: string): number | undefined {
  if (!/^\d{1,3}:\d{2}(?::\d{2})?$/.test(text)) return undefined;
  const parts = text.split(':').map(Number);
  if (parts.slice(1).some(n => n > 59)) return undefined;
  return parts.reduce((n, p) => n * 60 + p, 0) * 1000;
}
export function readTrack(row: Element, url: URL, albumTitle?: string): Observation | null {
  const title = renderedText(firstVisible(row, s.title));
  const artists = [...new Set([...row.querySelectorAll(s.artist)].map(renderedText).filter(Boolean))];
  if (!title || !artists.length) return null;
  const track: Track = { title, artists, position: 0 };
  const album = renderedText(firstVisible(row, s.album)) || albumTitle;
  const version = renderedText(firstVisible(row, s.version));
  const ms = duration(renderedText(firstVisible(row, s.duration)));
  if (album) track.album = album;
  if (version) track.version = version;
  if (ms !== undefined) track.duration_ms = ms;
  for (const link of row.querySelectorAll(s.link)) {
    if (!inViewport(link)) continue;
    const href = safeURL(link.getAttribute('href') ?? '', url);
    const match = href && /^\/(?:album\/\d+\/)?track\/(\d+)\/?$/.exec(href.pathname);
    if (match) { track.source_track_key = match[1]!; track.source_url = href!.href; break; }
  }
  const position = Number(row.getAttribute('aria-posinset'));
  const ordinal = Number.isSafeInteger(position) && position > 0 && position <= 100_000 ? position - 1 : undefined;
  if (ordinal !== undefined) track.position = ordinal;
  try { return { track: validateTrack(track), ...(ordinal === undefined ? {} : { ordinal }) }; }
  catch { return null; }
}
export function visibleRows(root: Element): Element[] {
  return [...root.querySelectorAll(s.row)].filter(row => inViewport(row) && !!row.closest(s.list) && !row.closest(s.excluded));
}
/** Capture-local digest, NOT the backend canonical fingerprint algorithm. */
export async function metadataDigest(track: Track): Promise<string> {
  const norm = (value: string): string => value.normalize('NFKC').replace(/\s+/gu, ' ').trim();
  const value = JSON.stringify([norm(track.title), track.artists.map(norm).sort(), norm(track.album ?? ''), track.duration_ms ?? null, norm(track.version ?? '')]);
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(value));
  return [...new Uint8Array(digest)].map(n => n.toString(16).padStart(2, '0')).join('');
}
