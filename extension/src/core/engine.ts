import type { Store, Job, Session } from './job';
import { finished } from './job';
import type { API } from '../api/client';
import type { Producer } from './demo';
import { AppError, safeError } from './errors';
import { jsonBytes, MAX_BYTES, MAX_OBSERVATIONS, validateTrack } from './validation';
import { selectionKey } from './selection';
import { retryDelay } from './retry';
/** One durable pending chunk and one HTTP operation at a time. No keepalive hacks. */
export class Engine {
  private running: Promise<void> | undefined;
  private controller: AbortController | undefined;
  constructor(private readonly store: Store, private readonly origin: string, private readonly api: (session: Session) => API, private readonly producer: Producer, private readonly now: () => number = Date.now) {}
  abort(): void { this.controller?.abort(); }
  async cancel(): Promise<void> {
    const job = await this.store.job(); if (!job || job.stage === 'cancelled') return;
    await this.store.updateJob(job.id, j => ({ ...j, cancelRequested: true, blocked: false, attempts: 0, retryAt: 0 }));
    this.abort();
  }
  async resume(): Promise<void> {
    const job = await this.store.job(); if (job) await this.store.updateJob(job.id, j => ({ ...j, blocked: false, attempts: 0, retryAt: 0 }));
  }
  tick(): Promise<void> {
    if (this.running) return this.running;
    this.running = this.pump().finally(() => { this.running = undefined; }); return this.running;
  }
  private async pump(): Promise<void> {
    // Bound work per wake. Alarm and popup events continue remaining work.
    for (let i = 0; i < 4; i++) {
      const job = await this.store.job();
      if (!job || finished(job) || job.blocked || job.retryAt > this.now()) return;
      const session = await this.store.session();
      if (!session || Date.parse(session.expiresAt) <= this.now()) return;
      if (session.origin !== this.origin || job.origin !== this.origin || session.owner !== job.owner) {
        await this.store.updateJob(job.id, j => ({ ...j, blocked: true, error: 'account_mismatch' })); return;
      }
      this.controller = new AbortController();
      try { await this.step(job, this.api(session), this.controller.signal); }
      catch (error) {
        // Logout may already have removed the job; updateJob never resurrects it.
        const latest = await this.store.job(); if (!latest || latest.id !== job.id) return;
        if (latest.cancelRequested && !job.cancelRequested) continue;
        if ((await this.store.session())?.token !== session.token) return;
        if (error instanceof AppError && error.code === 'unauthorized') await this.store.clearSession(session.token);
        const attempts = latest.attempts + 1;
        const retry = error instanceof AppError && error.retryable && attempts < 8;
        await this.store.updateJob(job.id, j => ({ ...j, attempts, blocked: !retry, retryAt: retry ? this.now() + retryDelay(error as AppError, attempts - 1) : 0, error: retry ? 'network' : error instanceof AppError && error.retryable ? 'retry_exhausted' : safeError(error) }));
        return;
      } finally { this.controller = undefined; }
    }
  }
  private async save(job: Job, patch: Partial<Job>, removePending = false): Promise<void> {
    await this.store.updateJob(job.id, current => {
      const next = { ...current, ...patch, attempts: 0, retryAt: 0, blocked: false };
      delete next.error; if (removePending) delete next.pending; return next;
    });
  }
  private async step(job: Job, api: API, signal: AbortSignal): Promise<void> {
    // Even cancel must recover a possibly-created import after a lost create ACK.
    if (!job.importId) {
      const result = await api.create(job.create, signal);
      await this.save(job, { importId: result.id, stage: 'collecting' }); return;
    }
    if (job.cancelRequested) {
      await api.cancel(job.importId, signal);
      await this.save(job, { stage: 'cancelled', cancelRequested: false, serverState: 'cancelled' }, true); return;
    }
    if (job.stage === 'collecting') {
      const chunk = await this.producer.next(job);
      if (!chunk) { await this.save(job, { stage: 'completing' }); return; }
      if (chunk.schema_version !== 1 || chunk.sequence !== job.nextSequence || chunk.sequence >= 10000 || !/^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/.test(chunk.idempotency_key) || chunk.tracks.length < 1 || chunk.tracks.length > 200 || jsonBytes(chunk) > MAX_BYTES || job.accepted + chunk.tracks.length > MAX_OBSERVATIONS) throw new AppError('invalid_data');
      chunk.tracks.forEach(validateTrack);
      // Persist exact key, sequence and payload BEFORE sending; at most 512 KiB.
      await this.save(job, { pending: chunk, stage: 'uploading', ...(job.captureId ? { pendingCursor: await selectionKey(job.create.source.service, chunk.tracks.at(-1)!) } : {}) }); return;
    }
    if (job.stage === 'uploading') {
      if (!job.pending) throw new AppError('storage');
      const ack = await api.append(job.importId, job.pending, signal);
      await this.save(job, { stage: 'collecting', accepted: job.accepted + ack.received, added: job.added + ack.added, nextSequence: job.nextSequence + 1, ...(job.pendingCursor ? { cursor: job.pendingCursor } : {}) }, true); return;
    }
    if (job.stage === 'completing') {
      const result = await api.complete(job.importId, { last_sequence: job.nextSequence - 1, observed_count: job.accepted, completeness: job.summary?.completeness ?? 'partial', reason: job.summary?.reason ?? 'unknown_end' }, signal);
      await this.save(job, { stage: 'done', serverState: result.state });
    }
  }
}
