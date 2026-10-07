import type { BrowserAPI } from '../core/browser';
import type { MusicSourceAdapter } from '../core/adapter';
import type { SelectionRow } from '../core/capture';
import { SelectionEngine } from '../core/selection';
import { SelectionView } from './selection-view';
import { AppError } from '../core/errors';
import { chunkTracks } from '../core/validation';
export class SelectionController {
  private readonly engine: SelectionEngine;
  private readonly view: SelectionView;
  private readonly scope: string;
  private readonly controller = new AbortController();
  private readonly observer: {dispose():void};
  private readonly timer: ReturnType<typeof setInterval>;
  private serial: Promise<unknown> = Promise.resolve();
  private count = 0; private working = false; private frozen = false; private disposed = false; private refreshing = false; private invalid = false; private complete = false;
  private message = 'Отметьте треки. Выбор сохраняется при прокрутке.';
  constructor(private readonly doc: Document, private readonly browser: BrowserAPI, private readonly adapter: MusicSourceAdapter, private readonly id: string, private readonly document: string, private readonly mode: 'all'|'selected') {
    const source = adapter.detectPage()?.source; if (!source || !adapter.selectionRows) throw new AppError('unsupported');
    this.scope = this.identity();
    this.engine = new SelectionEngine(source, {
      contains:async keys => {
        const result = await this.send<{flags:boolean[];count:number}>('selection.contains',{keys});
        this.count = result.count; this.update(); return result.flags;
      },
      change:(tracks,selected) => this.send('selection.change',{tracks,selected}),
      clear:() => this.send('selection.clear'),
    });
    this.view = new SelectionView(doc, {
      all:() => this.enqueue(() => this.all()),
      clear:() => this.enqueue(async () => { this.count = await this.engine.clear(); this.message = 'Выбор очищен.'; }),
      start:() => this.enqueue(() => this.start()),
      cancel:() => { this.controller.abort(); this.enqueue(async () => { try { await this.send('capture.cancel'); } finally { this.dispose(); } }); },
    });
    this.observer = adapter.observe(() => { void this.refresh(); });
    this.timer = setInterval(() => { void this.refresh(); },500);
    doc.addEventListener('scroll',this.onScroll,true); doc.defaultView!.addEventListener('resize',this.onScroll);
    void this.refresh(); if (mode === 'all') this.enqueue(() => this.all());
  }
  private identity(): string { const c = this.adapter.getCollectionMetadata(); return JSON.stringify([this.adapter.detectPage()?.source,c?.key,c?.kind,c?.title]); }
  private async send<T>(type:string,fields:Record<string,unknown> = {}): Promise<T> {
    const reply = await this.browser.runtime.sendMessage({type,id:this.id,document:this.document,...fields}) as {ok:boolean;data:T;error?:string};
    if (!reply.ok) throw new AppError(reply.error === 'unauthorized' ? 'unauthorized' : 'conflict');
    return reply.data;
  }
  private onScroll = ():void => { void this.refresh(); };
  private enqueue(action:()=>Promise<void>): void {
    this.serial = this.serial.then(async () => {
      if (this.disposed) return;
      this.working = true; this.update();
      try { await action(); }
      catch { this.message = 'Операция не завершена. Проверьте подключение; повторите действие или отмените выбор.'; }
      finally { this.working = false; this.update(); void this.refresh(); }
    });
  }
  private update(): void { if (!this.disposed) this.view.update(this.count,this.working || this.frozen || this.invalid,this.message); }
  private async refresh(): Promise<void> {
    if (this.disposed || this.refreshing) return;
    if (this.identity() !== this.scope) {
      this.invalid = true; this.controller.abort(); this.message = 'Коллекция изменилась. Отмените выбор и откройте режим заново.'; this.view.render([],[],[],true,()=>undefined); this.update(); return;
    }
    this.refreshing = true;
    try {
      const rows = (this.adapter.selectionRows?.() ?? []).slice(0,200);
      const keys = await Promise.all(rows.map(row => this.engine.key(row.track)));
      const flags = await this.engine.flags(rows.map(row => row.track));
      if (this.disposed || this.identity() !== this.scope) return;
      // A row may be recycled while the async lookup was pending. Never bind the old checkbox.
      const current = this.adapter.selectionRows?.() ?? [];
      const valid: SelectionRow[] = [], validKeys: string[] = [], validFlags: boolean[] = [];
      for (let i = 0; i < rows.length; i++) {
        const row = rows[i]!, now = current.find(item => item.element === row.element);
        if (now && await this.engine.key(now.track) === keys[i]) { valid.push(now); validKeys.push(keys[i]!); validFlags.push(flags[i] ?? false); }
      }
      if (!this.disposed) this.view.render(valid,validKeys,validFlags,this.working || this.frozen || this.invalid,(row,key,selected) => this.enqueue(async () => {
        if (this.invalid || this.frozen || this.identity() !== this.scope) return;
        const currentRow = this.adapter.selectionRows?.().find(item => item.element === row.element);
        if (!currentRow || await this.engine.key(currentRow.track) !== key) return;
        this.count = await this.engine.set(currentRow.track,selected); this.message = 'Выбор сохранён.';
      }));
    } catch { this.message = 'Не удалось прочитать выбор. Проверьте подключение или откройте режим заново.'; this.update(); }
    finally { this.refreshing = false; }
  }
  private async all(): Promise<void> {
    if (this.invalid || this.frozen || this.controller.signal.aborted) return;
    this.message = 'Собираем доступные треки с прокруткой…'; this.update();
    const iterator = this.adapter.collectAllTracks({signal:this.controller.signal,maxTracks:100000,onProgress:p => { this.message = `Просмотрено: ${p.collected}${p.expected === undefined ? '' : ' / '+p.expected}`; this.update(); }});
    try {
      for (;;) {
        const result = await iterator.next();
        if (this.disposed || this.controller.signal.aborted) return;
        if (result.done) {
          if (result.value.reason === 'dom_changed' || this.identity() !== this.scope) { this.invalid = true; this.message = 'Коллекция изменилась. Начните новый выбор.'; return; }
          await this.send('capture.seal',{summary:result.value});
          this.complete = result.value.completeness === 'complete';
          this.message = result.value.completeness === 'complete' ? 'Достигнут подтверждённый конец списка.' : 'Собрана только доступная часть списка.';
          if (this.mode === 'all') await this.start(); return;
        }
        // Split again with envelope budget; do not retain an entire library.
        const batch = result.value.tracks;
        async function* tracks() { yield* batch; }
        for await (const chunk of chunkTracks(tracks())) {
          if (this.controller.signal.aborted || this.disposed) return;
          this.count = await this.engine.selectBatch(chunk.tracks); this.update();
        }
      }
    } finally { await iterator.return({completeness:'partial',reason:'user_stopped',observedCount:this.count}); }
  }
  private async start(): Promise<void> {
    if (this.invalid || this.identity() !== this.scope || this.controller.signal.aborted) return;
    await this.send('capture.start'); this.frozen = true; this.message = `${this.mode === 'selected' ? 'Импорт выбранных треков' : this.complete ? 'Полный DOM-сбор' : 'Частичный DOM-сбор'}: импорт запущен. Прогресс и отмена доступны в popup.`;
  }
  dispose(): void {
    if (this.disposed) return; this.disposed = true; this.controller.abort(); clearInterval(this.timer); this.observer.dispose();
    this.doc.removeEventListener('scroll',this.onScroll,true); this.doc.defaultView!.removeEventListener('resize',this.onScroll); this.view.dispose();
  }
}
