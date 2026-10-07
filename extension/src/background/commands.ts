import type { BrowserAPI, Sender } from '../core/browser';
import type { Store } from '../core/job';
import { finished } from '../core/job';
import { Engine } from '../core/engine';
import { Sessions } from '../core/session';
import { Client } from '../api/client';
import { record, text, isID, jsonBytes } from '../core/validation';
import { AppError } from '../core/errors';
import { adapterFor } from '../sources';
export function trustedPopup(sender: Sender, browser: Pick<BrowserAPI, 'runtime'>): boolean {
  return sender.id === browser.runtime.id && sender.url === browser.runtime.getURL('popup.html') && !sender.tab;
}
export class Commands {
  private serial: Promise<unknown> = Promise.resolve();
  constructor(private readonly browser: BrowserAPI, private readonly store: Store, private readonly engine: Engine, private readonly sessions: Sessions, private readonly origin: string, private readonly permission: string) {}
  run(message: unknown): Promise<unknown> {
    if (jsonBytes(message) > 4096) return Promise.reject(new AppError('invalid_data'));
    const m = record(message, ['type','code','cursor','destinationId','profileKey']);
    if (m.type === 'state') return this.state();
    if (m.type === 'page') return this.page();
    const operation = this.serial.then(() => this.mutate(m));
    this.serial = operation.catch(() => undefined); return operation;
  }
  private async authorized(): Promise<Client> {
    const session = await this.store.session();
    if (!session || session.origin !== this.origin || Date.parse(session.expiresAt) <= Date.now()) throw new AppError('unauthorized');
    if (!await this.browser.permissions.contains({ origins: [this.permission] })) throw new AppError('permission');
    return new Client(this.origin, session.token);
  }
  private async mutate(m: Record<string, unknown>): Promise<unknown> {
    switch (m.type) {
      case 'pair': {
        record(m, ['type','code']);
        if (!await this.browser.permissions.contains({ origins: [this.permission] })) throw new AppError('permission');
        await this.sessions.pair(text(m.code, 128)); await this.engine.resume(); return null;
      }
      case 'reconnect': record(m,['type']); await this.authorized(); await this.sessions.reconnect(); await this.engine.resume(); return null;
      case 'logout': record(m,['type']); this.engine.abort(); await this.sessions.logout(); return null;
      case 'destinations': record(m,['type','cursor']); return (await this.authorized()).destinations(m.cursor === undefined ? '' : text(m.cursor,36));
      case 'startDemo': {
        record(m,['type','destinationId','profileKey']); await this.authorized();
        if (!isID(m.destinationId)) throw new AppError('invalid_data');
        const profile = text(m.profileKey,256); const session = await this.store.session();
        if (!session) throw new AppError('unauthorized');
        const previous = await this.store.job(); if (previous && !finished(previous)) throw new AppError('conflict');
        const id = crypto.randomUUID();
        await this.store.setJob({ id, owner: session.owner, origin: this.origin,
          create: { client_request_id: id, source: { service: 'spotify', profile_key: `demo:${profile}`, collection_key: 'musicgetter-demo-v1', kind: 'selection', title: 'MusicGetter DEMO — synthetic tracks', provisional: true }, destination_collection_id: m.destinationId },
          stage: 'creating', cancelRequested: false, nextSequence: 0, accepted: 0, added: 0,
          demoTotal: 450, attempts: 0, retryAt: 0, blocked: false }); return null;
      }
      case 'cancel': record(m,['type']); await this.engine.cancel(); return null;
      case 'resume': record(m,['type']); await this.authorized(); await this.engine.resume(); return null;
      case 'refresh': {
        record(m,['type']); const api = await this.authorized(); const job = await this.store.job();
        if (!job?.importId) return null;
        const session = await this.store.session(); if (session?.owner !== job.owner) throw new AppError('account_mismatch');
        const status = await api.progress(job.importId);
        await this.store.updateJob(job.id, j => ({ ...j, serverState: status.state })); return status;
      }
      default: throw new AppError('invalid_data');
    }
  }
  private async state(): Promise<unknown> {
    const session = await this.store.session(); const job = await this.store.job();
    const connected = !!session && session.origin === this.origin && Date.parse(session.expiresAt) > Date.now();
    // Explicit projection: never serialize a Session or a whole Job into messages.
    return { connected, telegramUser: connected ? session.telegramUser : null, expiresAt: connected ? session.expiresAt : null,
      job: job ? { stage: job.stage, accepted: job.accepted, added: job.added, total: job.demoTotal, demo: !job.captureId, completeness: job.summary?.completeness, retryAt: job.retryAt, blocked: job.blocked, error: job.error, cancelRequested: job.cancelRequested, serverState: job.serverState, importId: job.importId } : null };
  }
  private async page(): Promise<unknown> {
    const [tab] = await this.browser.tabs.query({ active: true, currentWindow: true });
    if (!tab?.url || tab.id === undefined) return { page: null };
    let page; try { page = adapterFor(new URL(tab.url))?.detectPage(); } catch { return { page: null }; }
    if (!page) return { page: null };
    await this.browser.scripting.executeScript({ target: { tabId: tab.id }, files: ['content.js'] });
    const response = record(await this.browser.tabs.sendMessage(tab.id, { type: 'page.info' }), ['page']);
    const result = record(response.page, ['source','supported','adapter']);
    if (result.source !== page.source || !['stub','ready'].includes(String(result.adapter)) || typeof result.supported !== 'boolean') throw new AppError('invalid_data');
    return { page: { source:page.source,adapter:result.adapter,supported:result.supported } }; // allowlisted projection
  }
}
