import type { Job } from './job';
import type { Producer } from './demo';
import type { SelectionDatabase } from '../storage/selection-database';
import { DemoProducer } from './demo';
export class SelectionProducer implements Producer {
  constructor(private readonly selections: SelectionDatabase) {}
  async next(job: Job) {
    if (!job.captureId) return new DemoProducer().next(job);
    const entries = await this.selections.page(job.captureId, job.cursor);
    if (!entries.length) return undefined;
    return { schema_version: 1 as const, idempotency_key: crypto.randomUUID(), sequence: job.nextSequence, tracks: entries.map(entry => entry.track) };
  }
}
