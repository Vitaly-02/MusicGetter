import type { CollectionKind, ImportTrack, Source, CaptureSummary } from './contracts';
export type Track = ImportTrack;
export interface PageContext { source: Source; supported: boolean; adapter: 'stub' | 'ready'; }
export interface CollectionMetadata { key: string; provisional: boolean; kind: CollectionKind; title: string; }
export interface Disposable { dispose(): void; }
export interface CollectOptions { signal: AbortSignal; maxTracks: number; }
export interface TrackBatch { tracks: readonly Track[]; }
/** Service-specific DOM belongs only inside sources/<service>. No network clients. */
export interface MusicSourceAdapter {
  detectPage(): PageContext | null;
  getCollectionMetadata(): CollectionMetadata | null;
  collectVisibleTracks(): Track[];
  // Streaming is deliberate: Promise<Track[]> would retain a large library in RAM.
  collectAllTracks(options: CollectOptions): AsyncGenerator<TrackBatch, CaptureSummary, void>;
  observe(callback: () => void): Disposable;
}
