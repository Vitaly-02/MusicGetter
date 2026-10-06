import type { MusicSourceAdapter, PageContext, CollectionMetadata, Track, CollectOptions, Disposable, TrackBatch } from '../core/adapter';
import type { Source, CaptureSummary } from '../core/contracts';
import { AppError } from '../core/errors';
export class StubAdapter implements MusicSourceAdapter {
  constructor(private readonly source: Source, private readonly url: URL, private readonly hosts: readonly string[]) {}
  detectPage(): PageContext | null { return this.url.protocol === 'https:' && this.hosts.includes(this.url.hostname) ? { source: this.source, supported: false, adapter: 'stub' } : null; }
  getCollectionMetadata(): CollectionMetadata | null { return null; }
  collectVisibleTracks(): Track[] { return []; }
  async *collectAllTracks(_options: CollectOptions): AsyncGenerator<TrackBatch, CaptureSummary, void> { throw new AppError('unsupported'); }
  observe(_callback: () => void): Disposable { return { dispose() {} }; }
}
