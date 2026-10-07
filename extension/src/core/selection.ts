import type { Source } from './contracts';
import type { Track } from './adapter';
import { validateTrack } from './validation';
/** Local, versioned selection identity; deliberately not the Go canonical fingerprint. */
export async function selectionKey(source: Source, input: Track): Promise<string> {
  const track = validateTrack(input);
  const normalize = (text: string): string => text.normalize('NFKC').toLowerCase().normalize('NFKC').replace(/\s+/gu, ' ').trim();
  const stable = !!track.source_track_key;
  const data = stable ? [source, track.source_track_key] : [source, normalize(track.title), [...new Set(track.artists.map(normalize))].sort(), normalize(track.album ?? ''), track.duration_ms ?? null, normalize(track.version ?? '')];
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify(data)));
  return `${stable ? 'key' : 'fp'}1:` + [...new Uint8Array(digest)].map(n => n.toString(16).padStart(2, '0')).join('');
}
export interface SelectionPort {
  contains(keys: readonly string[]): Promise<readonly boolean[]>;
  change(tracks: readonly Track[], selected: boolean): Promise<number>;
  clear(): Promise<number>;
}
/** No metadata library retained in RAM: storage is behind an asynchronous port. */
export class SelectionEngine {
  constructor(readonly source: Source, private readonly storage: SelectionPort) {}
  key(track: Track): Promise<string> { return selectionKey(this.source, track); }
  async flags(tracks: readonly Track[]): Promise<readonly boolean[]> { return this.storage.contains(await Promise.all(tracks.map(track => this.key(track)))); }
  set(track: Track, selected: boolean): Promise<number> { return this.storage.change([validateTrack(track)], selected); }
  selectBatch(tracks: readonly Track[]): Promise<number> { return this.storage.change(tracks.map(validateTrack), true); }
  clear(): Promise<number> { return this.storage.clear(); }
}
