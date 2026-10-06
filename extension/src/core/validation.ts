import type { ImportTrack, ImportChunk } from './contracts';
import { AppError } from './errors';
export const MAX_TRACKS = 200;
export const MAX_BYTES = 512 * 1024;
export const MAX_OBSERVATIONS = 100_000;
export const jsonBytes = (value: unknown): number => new TextEncoder().encode(JSON.stringify(value)).length;
export const isID = (value: unknown): value is string => typeof value === 'string' && /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(value);
export function record(value: unknown, allowed: readonly string[]): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new AppError('invalid_data');
  const result = value as Record<string, unknown>;
  if (Object.keys(result).some(key => !allowed.includes(key))) throw new AppError('invalid_data');
  return result;
}
export function text(value: unknown, max: number, required = true): string {
  if (typeof value !== 'string' || [...value].length > max || value.includes('\0') || (required && !value.trim())) throw new AppError('invalid_data');
  return value;
}
export function validateTrack(value: unknown): ImportTrack {
  const t = record(value, ['title','artists','album','duration_ms','version','source_track_key','source_url','position']);
  text(t.title, 1024);
  if (!Array.isArray(t.artists) || t.artists.length < 1 || t.artists.length > 32) throw new AppError('invalid_data');
  t.artists.forEach(a => text(a, 512));
  if (!Number.isSafeInteger(t.position) || (t.position as number) < 0) throw new AppError('invalid_data');
  if (t.duration_ms != null && (!Number.isSafeInteger(t.duration_ms) || (t.duration_ms as number) < 0)) throw new AppError('invalid_data');
  if (t.album !== undefined) text(t.album, 1024, false);
  if (t.version !== undefined) text(t.version, 512, false);
  if (t.source_track_key != null) text(t.source_track_key, 512);
  if (t.source_url != null) {
    const value = text(t.source_url, 2048); let url: URL;
    try { url = new URL(value); } catch { throw new AppError('invalid_data'); }
    if (url.protocol !== 'https:' || url.username || url.password || url.port || value.includes('?') || value.includes('#') || !['open.spotify.com','music.yandex.ru','music.yandex.com','music.yandex.kz','vk.com','music.vk.com'].includes(url.hostname)) throw new AppError('invalid_data');
  }
  return structuredClone(t) as unknown as ImportTrack;
}
/** Only one bounded batch retained; producer waits for durable acceptance/ACK. */
export async function* chunkTracks(tracks: AsyncIterable<ImportTrack>, start = 0, key: () => string = () => crypto.randomUUID()): AsyncGenerator<ImportChunk> {
  let sequence = start, count = 0;
  let chunk: ImportChunk = { schema_version: 1, idempotency_key: key(), sequence, tracks: [] };
  for await (const value of tracks) {
    const track = validateTrack(value);
    if (++count > MAX_OBSERVATIONS || sequence >= 10000) throw new AppError('invalid_data');
    if (chunk.tracks.length === MAX_TRACKS || jsonBytes({ ...chunk, tracks: [...chunk.tracks, track] }) > MAX_BYTES) {
      if (!chunk.tracks.length) throw new AppError('invalid_data');
      yield chunk;
      chunk = { schema_version: 1, idempotency_key: key(), sequence: ++sequence, tracks: [] };
    }
    chunk = { ...chunk, tracks: [...chunk.tracks, track] };
    if (jsonBytes(chunk) > MAX_BYTES) throw new AppError('invalid_data');
  }
  if (chunk.tracks.length) yield chunk;
}
