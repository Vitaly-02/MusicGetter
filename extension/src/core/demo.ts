import type { ImportChunk } from './contracts';
import type { Job } from './job';
import { chunkTracks } from './validation';
export interface Producer { next(job: Job): Promise<ImportChunk | undefined>; }
/** Explicit synthetic fixture, never presented as the user's streaming library. */
export class DemoProducer implements Producer {
  async next(job: Job): Promise<ImportChunk | undefined> {
    async function* tracks() {
      const end = Math.min(job.accepted + 200, job.demoTotal);
      for (let index = job.accepted; index < end; index++) {
        yield { title: `MusicGetter DEMO ${index + 1}`, artists: ['Synthetic fixture'], album: 'Not a streaming collection', position: index };
      }
    }
    return (await chunkTracks(tracks(), job.nextSequence).next()).value;
  }
}
