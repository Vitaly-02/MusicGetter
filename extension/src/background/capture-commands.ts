import type { BrowserAPI, Sender } from '../core/browser';
import type { Store } from '../core/job';
import type { Engine } from '../core/engine';
import type { Capture } from '../core/capture';
import { finished } from '../core/job';
import { SelectionDatabase } from '../storage/selection-database';
import { selectionKey } from '../core/selection';
import { record, text, isID, jsonBytes, MAX_BYTES, validateTrack } from '../core/validation';
import { AppError } from '../core/errors';
import { adapterFor } from '../sources';
import { collectionValue, sourceValue, summaryValue, sourceHost } from './capture-validation';
/** Caller serializes all mutations with pairing/logout and other popup commands. */
export class CaptureCommands {
  constructor(private readonly browser: BrowserAPI, private readonly store: Store, private readonly db: SelectionDatabase, private readonly engine: Engine, private readonly origin: string, private readonly permission: string) {}
  private async session() {
    const s = await this.store.session();
    if (!s || s.origin !== this.origin || Date.parse(s.expiresAt) <= Date.now()) throw new AppError('unauthorized');
    if (!await this.browser.permissions.contains({ origins: [this.permission] })) throw new AppError('permission');
    return s;
  }
  async popup(message: unknown): Promise<unknown> {
    if (jsonBytes(message) > 4096) throw new AppError('invalid_data');
    const m = record(message, ['type','mode','profileKey','destinationId']);
    if (m.type === 'capture.cancel') { record(m,['type']); return this.cancel(await this.owned(true)); }
    const session = await this.session();
    if (m.type === 'capture.open') {
      record(m, ['type','mode','profileKey','destinationId']);
      if (!['all','selected'].includes(String(m.mode)) || !isID(m.destinationId)) throw new AppError('invalid_data');
      const previous = await this.store.job(); if (previous && !finished(previous)) throw new AppError('conflict');
      const [tab] = await this.browser.tabs.query({ active:true,currentWindow:true });
      if (tab?.id === undefined || !tab.url) throw new AppError('unsupported');
      const expected = adapterFor(new URL(tab.url))?.detectPage()?.source;
      if (!expected) throw new AppError('unsupported');
      await this.browser.scripting.executeScript({ target:{tabId:tab.id},files:['content.js'] });
      const info = record(await this.browser.tabs.sendMessage(tab.id,{type:'capture.describe'}),['source','collection','document']);
      const source = sourceValue(info.source);
      if (source !== expected || !isID(info.document)) throw new AppError('invalid_data');
      const capture: Capture = { id:crypto.randomUUID(),owner:session.owner,origin:this.origin,tabId:tab.id,document:info.document,source,collection:collectionValue(info.collection),mode:m.mode as Capture['mode'],profile:text(m.profileKey,256),destination:m.destinationId,state:'editing',count:0 };
      await this.close(); await this.db.reset(capture);
      const ready = record(await this.browser.tabs.sendMessage(tab.id,{type:'capture.init',id:capture.id,document:capture.document,mode:capture.mode}),['ok']);
      if (ready.ok !== true) { await this.db.reset(); throw new AppError('unsupported'); }
      return { count:0 };
    }
    record(m,['type']);
    const capture = await this.owned();
    if (m.type === 'capture.state') return { count:capture.count,state:capture.state,mode:capture.mode,summary:capture.summary };
    if (m.type === 'capture.start') return this.start(capture);
    if (m.type === 'capture.cancel') return this.cancel(capture);
    throw new AppError('invalid_data');
  }
  private async owned(localCancel = false): Promise<Capture> {
    const session = localCancel ? await this.store.session() : await this.session(), capture = await this.db.current();
    if (!session) throw new AppError('unauthorized');
    if (!capture) throw new AppError('unsupported');
    if (capture.owner !== session.owner || capture.origin !== this.origin || session.origin !== this.origin) throw new AppError('account_mismatch');
    return capture;
  }
  async content(message: unknown, sender: Sender): Promise<unknown> {
    if (sender.id !== this.browser.runtime.id || sender.frameId !== 0 || sender.tab?.id === undefined || !sender.url || jsonBytes(message) > MAX_BYTES) throw new AppError('permission');
    const m = record(message,['type','id','document','tracks','selected','keys','summary']);
    const capture = await this.owned(m.type === 'capture.cancel');
    const url = new URL(sender.url);
    if (capture.tabId !== sender.tab.id || url.protocol !== 'https:' || !sourceHost(capture.source,url.hostname)) throw new AppError('permission');
    if (m.id !== capture.id || m.document !== capture.document) throw new AppError('conflict');
    if (m.type === 'selection.contains') {
      record(m,['type','id','document','keys']);
      if (!Array.isArray(m.keys) || m.keys.length > 200 || m.keys.some(k => typeof k !== 'string' || !/^(key|fp)1:[a-f0-9]{64}$/.test(k))) throw new AppError('invalid_data');
      return { flags:await this.db.contains(capture.id,m.keys as string[]),count:capture.count };
    }
    if (m.type === 'selection.change') {
      record(m,['type','id','document','tracks','selected']);
      if (!Array.isArray(m.tracks) || m.tracks.length > 200 || typeof m.selected !== 'boolean') throw new AppError('invalid_data');
      const tracks = m.tracks.map(validateTrack);
      for (const track of tracks) if (track.source_url && !sourceHost(capture.source,new URL(track.source_url).hostname)) throw new AppError('invalid_data');
      const entries = await Promise.all(tracks.map(async track => ({ key:await selectionKey(capture.source,track),track })));
      return this.db.change(capture.id,entries,m.selected);
    }
    if (m.type === 'selection.clear') {
      record(m,['type','id','document']);
      const result = await this.db.edit(capture.id,c => { if (c.state !== 'editing') throw new AppError('conflict'); const next = {...c,count:0}; delete next.summary; return next; },true);
      return result.count;
    }
    if (m.type === 'capture.seal') {
      record(m,['type','id','document','summary']); const summary = summaryValue(m.summary);
      await this.db.edit(capture.id,c => {
        if (c.state !== 'editing') throw new AppError('conflict');
        return {...c,summary:c.mode === 'all' && summary.observedCount === c.count ? summary : {completeness:'partial',reason:'user_stopped',observedCount:c.count}};
      });
      return null;
    }
    record(m,['type','id','document']);
    if (m.type === 'capture.start') return this.start(capture);
    if (m.type === 'capture.cancel') return this.cancel(capture);
    throw new AppError('invalid_data');
  }
  private async start(capture: Capture): Promise<unknown> {
    const previous = await this.store.job();
    if (previous?.id === capture.id) return { started:true }; // recover lost start ACK
    if (previous && !finished(previous)) throw new AppError('conflict');
    const frozen = await this.db.edit(capture.id,c => {
      if (c.state === 'cancelled' || !c.count || (c.mode === 'all' && !c.summary)) throw new AppError('conflict');
      return {...c,state:'frozen'};
    });
    await this.store.setJob({ id:frozen.id,owner:frozen.owner,origin:frozen.origin,captureId:frozen.id,
      create:{client_request_id:frozen.id,source:{service:frozen.source,profile_key:frozen.profile,collection_key:frozen.mode === 'selected' ? `selection:${frozen.id}` : frozen.collection.key,kind:frozen.mode === 'selected' ? 'selection' : frozen.collection.kind,title:frozen.collection.title,provisional:frozen.collection.provisional},destination_collection_id:frozen.destination},
      summary:frozen.mode === 'all' && frozen.summary ? frozen.summary : {completeness:'partial',reason:'user_stopped',observedCount:frozen.count},
      stage:'creating',cancelRequested:false,nextSequence:0,accepted:0,added:0,demoTotal:frozen.count,attempts:0,retryAt:0,blocked:false });
    return { started:true };
  }
  private async cancel(capture: Capture): Promise<null> {
    if ((await this.store.job())?.captureId === capture.id) await this.engine.cancel();
    await this.db.edit(capture.id,c => ({...c,state:'cancelled',count:0}),true);
    await this.close(); return null;
  }
  async close(): Promise<void> {
    const previous = await this.db.current();
    if (previous) await this.browser.tabs.sendMessage(previous.tabId,{type:'capture.close',id:previous.id}).catch(() => undefined);
  }
  async clear(): Promise<void> { await this.close(); await this.db.reset(); }
}
