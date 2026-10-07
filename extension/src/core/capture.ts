import type { CollectionMetadata } from './adapter';
import type { CaptureSummary, Source } from './contracts';
export interface Capture {
  id: string; owner: string; origin: string; tabId: number; document: string;
  source: Source; collection: CollectionMetadata; mode: 'all' | 'selected';
  profile: string; destination: string;
  state: 'editing' | 'frozen' | 'cancelled'; count: number;
  summary?: CaptureSummary;
}
export interface SelectionRow { element: Element; track: import('./adapter').Track; }
