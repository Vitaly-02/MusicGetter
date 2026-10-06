import type { Job, Session, Store } from '../core/job';
import { AppError } from '../core/errors';
/** Extension-origin IndexedDB: inaccessible to content scripts (page origin).
 * No sync storage, no homemade encryption key stored next to the credential. */
export class Database implements Store {
  private db: Promise<IDBDatabase>;
  constructor(factory: IDBFactory = indexedDB, name = 'musicgetter-private-v1') {
    this.db = new Promise((resolve, reject) => {
      const open = factory.open(name, 1);
      open.onupgradeneeded = () => open.result.createObjectStore('private');
      open.onsuccess = () => resolve(open.result);
      open.onerror = () => reject(new AppError('storage'));
      open.onblocked = () => reject(new AppError('storage'));
    });
  }
  private async read<T>(key: string): Promise<T | undefined> {
    const db = await this.db;
    return new Promise((resolve, reject) => {
      const tx = db.transaction('private', 'readonly');
      const request = tx.objectStore('private').get(key);
      tx.oncomplete = () => resolve(request.result as T | undefined);
      tx.onabort = tx.onerror = () => reject(new AppError('storage'));
    });
  }
  private async write(key: string, value: unknown): Promise<void> {
    const db = await this.db;
    return new Promise((resolve, reject) => {
      const tx = db.transaction('private', 'readwrite');
      tx.objectStore('private').put(value, key);
      tx.oncomplete = () => resolve(); tx.onabort = tx.onerror = () => reject(new AppError('storage'));
    });
  }
  session(): Promise<Session | undefined> { return this.read('session'); }
  setSession(s: Session): Promise<void> { return this.write('session', s); }
  job(): Promise<Job | undefined> { return this.read('job'); }
  setJob(job: Job): Promise<void> { return this.write('job', job); }
  async updateJob(id: string, update: (job: Job) => Job): Promise<void> {
    const db = await this.db;
    return new Promise((resolve, reject) => {
      const tx = db.transaction('private', 'readwrite'); const store = tx.objectStore('private');
      const request = store.get('job');
      request.onsuccess = () => { const job = request.result as Job | undefined; if (job?.id === id) { try { store.put(update(job), 'job'); } catch { tx.abort(); } } };
      tx.oncomplete = () => resolve(); tx.onabort = tx.onerror = () => reject(new AppError('storage'));
    });
  }
  private async remove(all: boolean, expectedToken?: string): Promise<void> {
    const db = await this.db;
    return new Promise((resolve, reject) => {
      const tx = db.transaction('private', 'readwrite'); const store = tx.objectStore('private');
      if (expectedToken) { const request=store.get('session'); request.onsuccess=()=>{ if ((request.result as Session | undefined)?.token === expectedToken) store.delete('session'); }; } else store.delete('session');
      if (all) store.delete('job');
      tx.oncomplete = () => resolve(); tx.onabort = tx.onerror = () => reject(new AppError('storage'));
    });
  }
  clearSession(expectedToken?: string): Promise<void> { return this.remove(false, expectedToken); }
  clearAll(): Promise<void> { return this.remove(true); }
}
