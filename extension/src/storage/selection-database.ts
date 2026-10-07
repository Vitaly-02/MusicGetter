import type { Capture } from '../core/capture';
import type { Track } from '../core/adapter';
import { AppError } from '../core/errors';
import { jsonBytes, MAX_BYTES, MAX_OBSERVATIONS } from '../core/validation';
export interface Entry { key: string; track: Track; }
/** Extension-origin only. One active capture, immutable once attached to an import. */
export class SelectionDatabase {
  private readonly database: Promise<IDBDatabase>;
  constructor(factory: IDBFactory = indexedDB, name = 'musicgetter-selection-v1') {
    this.database = new Promise((resolve, reject) => {
      const open = factory.open(name, 1);
      open.onupgradeneeded = () => { open.result.createObjectStore('state'); open.result.createObjectStore('tracks'); };
      open.onsuccess = () => resolve(open.result);
      open.onerror = open.onblocked = () => reject(new AppError('storage'));
    });
  }
  private async transaction<T>(write: boolean, action: (state: IDBObjectStore, tracks: IDBObjectStore, result: (value: T) => void, fail: (error: AppError) => void) => void): Promise<T> {
    const db = await this.database;
    return new Promise((resolve, reject) => {
      const tx = db.transaction(['state','tracks'], write ? 'readwrite' : 'readonly'); let value: T; let error: AppError;
      const fail = (e: AppError) => { error = e; tx.abort(); };
      tx.oncomplete = () => resolve(value); tx.onabort = tx.onerror = () => reject(error ?? new AppError('storage'));
      try { action(tx.objectStore('state'), tx.objectStore('tracks'), v => { value = v; }, fail); } catch { fail(new AppError('storage')); }
    });
  }
  current(): Promise<Capture | undefined> {
    return this.transaction(false, (state, _tracks, result) => { const r = state.get('capture'); r.onsuccess = () => result(r.result as Capture | undefined); });
  }
  reset(capture?: Capture): Promise<void> {
    return this.transaction(true, (state, tracks, result) => { state.clear(); tracks.clear(); if (capture) state.put(capture, 'capture'); result(); });
  }
  async contains(id: string, keys: readonly string[]): Promise<boolean[]> {
    return this.transaction(false, (state, tracks, result, fail) => {
      const r = state.get('capture'); r.onsuccess = () => {
        if ((r.result as Capture | undefined)?.id !== id) { fail(new AppError('conflict')); return; }
        const flags = keys.map(() => false); result(flags);
        keys.forEach((key, i) => { const found = tracks.getKey(key); found.onsuccess = () => { flags[i] = found.result !== undefined; }; });
      };
    });
  }
  change(id: string, entries: readonly Entry[], selected: boolean): Promise<number> {
    return this.transaction(true, (state, tracks, result, fail) => {
      const r = state.get('capture'); r.onsuccess = () => {
        const capture = r.result as Capture | undefined;
        if (capture?.id !== id || capture.state !== 'editing') { fail(new AppError('conflict')); return; }
        const unique = [...new Map(entries.map(entry => [entry.key, entry])).values()]; let remaining = unique.length;
        const finish = () => { state.put(capture, 'capture'); result(capture.count); };
        if (!remaining) { finish(); return; }
        for (const entry of unique) {
          const found = tracks.getKey(entry.key);
          found.onsuccess = () => {
            if (selected && found.result === undefined) { capture.count++; delete capture.summary; tracks.put(entry.track, entry.key); }
            if (!selected && found.result !== undefined) { capture.count--; delete capture.summary; tracks.delete(entry.key); }
            if (capture.count > MAX_OBSERVATIONS) { fail(new AppError('invalid_data')); return; }
            if (--remaining === 0) finish();
          };
        }
      };
    });
  }
  edit(id: string, fn: (capture: Capture) => Capture, clear = false): Promise<Capture> {
    return this.transaction(true, (state, tracks, result, fail) => {
      const r = state.get('capture'); r.onsuccess = () => {
        const capture = r.result as Capture | undefined;
        if (capture?.id !== id) { fail(new AppError('conflict')); return; }
        try { const next = fn(capture); state.put(next, 'capture'); if (clear) tracks.clear(); result(next); } catch (e) { fail(e instanceof AppError ? e : new AppError('storage')); }
      };
    });
  }
  page(id: string, after = ''): Promise<Entry[]> {
    return this.transaction(false, (state, tracks, result, fail) => {
      const r = state.get('capture'); r.onsuccess = () => {
        const capture = r.result as Capture | undefined;
        if (capture?.id !== id || capture.state !== 'frozen') { fail(new AppError('conflict')); return; }
        const entries: Entry[] = []; let bytes = 1024; result(entries);
        const cursor = tracks.openCursor(after ? IDBKeyRange.lowerBound(after, true) : undefined);
        cursor.onsuccess = () => {
          const c = cursor.result; if (!c) return;
          const track = c.value as Track; const size = jsonBytes(track) + 1;
          if (entries.length >= 200 || bytes + size > MAX_BYTES) return;
          bytes += size; entries.push({ key: String(c.key), track }); c.continue();
        };
      };
    });
  }
}
