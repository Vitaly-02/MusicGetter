import type { CreateImportRequest, ImportChunk, CompleteImportRequest, ChunkReceipt } from '../core/contracts';
import { AppError } from '../core/errors';
import { isID } from '../core/validation';
export interface Destination { id: string; title: string; kind: string; }
export interface ServerProgress { id: string; state: string; total_tracks: number; added: number; failed: number; }
export interface API {
  create(request: CreateImportRequest, signal: AbortSignal): Promise<{ id: string }>;
  append(id: string, chunk: ImportChunk, signal: AbortSignal): Promise<ChunkReceipt>;
  complete(id: string, request: CompleteImportRequest, signal: AbortSignal): Promise<ServerProgress>;
  cancel(id: string, signal: AbortSignal): Promise<ServerProgress>;
}
export class Client implements API {
  constructor(private readonly origin: string, private readonly token: string | undefined, private readonly fetcher: typeof fetch = fetch) {}
  private async request<T>(path: string, method: string, body?: unknown, signal?: AbortSignal): Promise<T> {
    const controller = new AbortController();
    const stop = () => controller.abort(); signal?.addEventListener('abort', stop, { once: true });
    if (signal?.aborted) controller.abort();
    const timer = setTimeout(stop, 15_000);
    try {
      const response = await this.fetcher(this.origin + path, {
        method, credentials: 'omit', redirect: 'error', cache: 'no-store', referrerPolicy: 'no-referrer',
        headers: { ...(body === undefined ? {} : { 'Content-Type': 'application/json' }), ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}) },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }), signal: controller.signal,
      });
      if (!response.ok) {
        if (response.status === 401) throw new AppError('unauthorized');
        if (response.status === 403) throw new AppError('permission');
        if (response.status === 409) throw new AppError('conflict');
        if (response.status === 429 || response.status >= 500) {
          const raw = response.headers.get('Retry-After') || '0';
          const ms = /^\d+$/.test(raw) ? Number(raw) * 1000 : Date.parse(raw) - Date.now();
          throw new AppError('network', true, Math.max(0, Math.min(Number.isFinite(ms) ? ms : 0, 7_200_000)));
        }
        throw new AppError('invalid_data');
      }
      // Response sizes bounded independently from request bodies.
      const reader = response.body?.getReader(); if (!reader) throw new AppError('unavailable');
      const chunks: Uint8Array[] = []; let size = 0;
      while (true) { const part = await reader.read(); if (part.done) break; size += part.value.length; if (size > 256 * 1024) { await reader.cancel(); throw new AppError('unavailable'); } chunks.push(part.value); }
      const bytes = new Uint8Array(size); let offset = 0; for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
      try { return JSON.parse(new TextDecoder().decode(bytes)) as T; } catch { throw new AppError('unavailable'); }
    } catch (error) { if (error instanceof AppError) throw error; throw new AppError('network', true); }
    finally { clearTimeout(timer); signal?.removeEventListener('abort', stop); }
  }
  async claim(code: string): Promise<{ token: string; expires_at: string }> {
    const result = await this.request<{ token: string; expires_at: string }>('/v1/pair/claim', 'POST', { code });
    if (!/^mge_[A-Za-z0-9_-]{43}$/.test(result.token) || !Number.isFinite(Date.parse(result.expires_at))) throw new AppError('pairing_uncertain');
    return result;
  }
  async me(): Promise<{ id: string; telegram_user_id: number }> {
    const result = await this.request<{ id: string; telegram_user_id: number }>('/v1/me', 'GET');
    if (!isID(result.id) || !Number.isSafeInteger(result.telegram_user_id) || result.telegram_user_id <= 0) throw new AppError('unavailable'); return result;
  }
  async destinations(cursor = ''): Promise<{ items: Destination[]; next_cursor?: string }> {
    if (cursor && !isID(cursor)) throw new AppError('invalid_data');
    const result = await this.request<{ items: Destination[]; next_cursor?: string }>('/v1/destinations?limit=50' + (cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''), 'GET');
    if (!Array.isArray(result.items) || result.items.length > 50 || result.items.some(d => !isID(d.id) || typeof d.title !== 'string' || typeof d.kind !== 'string') || (result.next_cursor && !isID(result.next_cursor))) throw new AppError('unavailable'); return result;
  }
  async create(body: CreateImportRequest, signal: AbortSignal): Promise<{ id: string }> {
    const result = await this.request<{ id: string }>('/v1/imports', 'POST', body, signal);
    if (!isID(result.id)) throw new AppError('unavailable'); return result;
  }
  private path(id: string): string { if (!isID(id)) throw new AppError('invalid_data'); return `/v1/imports/${id}`; }
  async append(id: string, chunk: ImportChunk, signal: AbortSignal): Promise<ChunkReceipt> {
    const receipt = await this.request<ChunkReceipt>(this.path(id) + '/tracks', 'POST', chunk, signal);
    if (receipt.sequence !== chunk.sequence || receipt.idempotency_key !== chunk.idempotency_key || receipt.received !== chunk.tracks.length || !Number.isInteger(receipt.added) || receipt.added < 0 || receipt.added > receipt.received) throw new AppError('unavailable'); return receipt;
  }
  private async serverProgress(path: string, method: string, body?: unknown, signal?: AbortSignal): Promise<ServerProgress> {
    const result = await this.request<ServerProgress>(path, method, body, signal);
    if (!result || !isID(result.id) || !['collecting','queued','running','needs_attention','completed','completed_with_errors','cancelled','failed'].includes(result.state) || [result.total_tracks,result.added,result.failed].some(n => !Number.isSafeInteger(n) || n < 0 || n > 100000)) throw new AppError('unavailable');
    return { id: result.id, state: result.state, total_tracks: result.total_tracks, added: result.added, failed: result.failed };
  }
  complete(id: string, request: CompleteImportRequest, signal: AbortSignal): Promise<ServerProgress> { return this.serverProgress(this.path(id) + '/complete', 'POST', request, signal); }
  cancel(id: string, signal: AbortSignal): Promise<ServerProgress> { return this.serverProgress(this.path(id) + '/cancel', 'POST', undefined, signal); }
  progress(id: string): Promise<ServerProgress> { return this.serverProgress(this.path(id), 'GET'); }
}
