import type { MusicSourceAdapter, PageContext, CollectionMetadata, Track, CollectOptions, Disposable, TrackBatch } from '../../core/adapter';
import type { CaptureSummary } from '../../core/contracts';
import { AppError } from '../../core/errors';
import { collect, defaultTiming, type CollectorTiming } from './collector';
import { firstVisible, hosts, metadata, pageRoot, route } from './page';
import { readTrack, visibleRows } from './tracks';
import { selectors as s } from './selectors';
export class YandexAdapter implements MusicSourceAdapter {
  private collecting = false;
  constructor(private readonly url: URL, private readonly doc?: Document, private readonly timing: CollectorTiming = defaultTiming) {}
  private currentURL = (): URL => this.doc ? new URL(this.doc.location.href) : this.url;
  detectPage(): PageContext | null {
    const url = this.currentURL();
    if (url.protocol !== 'https:' || !hosts.includes(url.hostname) || url.port || url.username || url.password) return null;
    const root = this.doc && pageRoot(this.doc);
    return { source: 'yandex', supported: !!route(url) && !!this.getCollectionMetadata() && !!root && !!firstVisible(root, s.list), adapter: 'ready' };
  }
  getCollectionMetadata(): CollectionMetadata | null { return this.doc ? metadata(this.doc, this.currentURL()) : null; }
  collectVisibleTracks(): Track[] {
    const collection = this.getCollectionMetadata(), root = this.doc && pageRoot(this.doc);
    if (!collection || !root) return [];
    return visibleRows(root).flatMap((row, index) => {
      const value = readTrack(row, this.currentURL(), collection.kind === 'album' ? collection.title : undefined);
      return value ? [{ ...value.track, position: value.ordinal ?? index }] : [];
    });
  }
  async *collectAllTracks(options: CollectOptions): AsyncGenerator<TrackBatch, CaptureSummary, void> {
    if (!this.doc) throw new AppError('unsupported');
    if (this.collecting) throw new AppError('conflict');
    this.collecting = true;
    try { return yield* collect(this.doc, this.currentURL, options, this.timing); }
    finally { this.collecting = false; }
  }
  observe(callback: () => void): Disposable {
    const root = this.doc && pageRoot(this.doc), view = this.doc?.defaultView;
    if (!root || !view) return { dispose() {} };
    const observer = new view.MutationObserver(callback);
    observer.observe(root, { childList: true, subtree: true, characterData: true, attributes: true });
    return { dispose: () => observer.disconnect() };
  }
}
export const createAdapter = (url: URL, doc: Document | undefined = globalThis.document): YandexAdapter => new YandexAdapter(url, doc);
