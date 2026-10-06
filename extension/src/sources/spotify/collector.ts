import type { CollectOptions, Track, TrackBatch } from '../../core/adapter';
import type { CaptureSummary } from '../../core/contracts';
import { inViewport, rendered, renderedText, waitForDOM } from '../../core/dom';
import { AppError } from '../../core/errors';
import { jsonBytes, MAX_BYTES, MAX_OBSERVATIONS, MAX_TRACKS } from '../../core/validation';
import { expectedCount, firstVisible, metadata, pageRoot, scrollContainer } from './page';
import { metadataDigest, readTrack, visibleRows } from './tracks';
import { selectors as s } from './selectors';
export interface CollectorTiming { settleMs: number; quietPasses: number; maxSteps: number; }
export const defaultTiming: CollectorTiming = { settleMs: 700, quietPasses: 4, maxSteps: 10_000 };
export async function* collect(doc: Document, currentURL: () => URL, options: CollectOptions, timing: CollectorTiming): AsyncGenerator<TrackBatch, CaptureSummary, void> {
  if (!Number.isInteger(options.maxTracks) || options.maxTracks < 1 || options.maxTracks > MAX_OBSERVATIONS) throw new AppError('invalid_data');
  const url = currentURL(), collection = metadata(doc, url), root = pageRoot(doc);
  const list = root && firstVisible(root, s.list);
  if (!collection || !root || !list) throw new AppError('unsupported');
  const scroll = scrollContainer(list), seen = new Map<string, string>();
  let expected = expectedCount(root), count = 0, uncertain = false, bottomQuiet = 0, stagnant = 0;
  let reason: CaptureSummary['reason'] = 'unknown_end';
  const progress = (phase: 'collecting' | 'scrolling' | 'finished', summary?: CaptureSummary): void => {
    options.onProgress?.({ collected: count, ...(expected === undefined ? {} : { expected }), phase, ...(summary ? { summary } : {}) });
  };
  const changed = (): boolean => currentURL().origin !== url.origin || currentURL().pathname !== url.pathname ||
    !root.isConnected || pageRoot(doc) !== root || firstVisible(root, s.list) !== list ||
    !scroll.isConnected || metadata(doc, currentURL())?.title !== collection.title;
  try {
    if (options.signal.aborted) reason = 'user_stopped';
    else {
      // Always traverse from the beginning; never call a bottom-only snapshot complete.
      await waitForDOM(root, options.signal, timing.settleMs, () => { scroll.scrollTo({ top: 0, behavior: 'instant' }); });
      for (let step = 0; step < timing.maxSteps; step++) {
        if (options.signal.aborted) { reason = 'user_stopped'; break; }
        if (changed()) { reason = 'dom_changed'; break; }
        const nextExpected = expectedCount(root);
        if (expected !== undefined && nextExpected !== expected) { reason = 'dom_changed'; break; }
        expected = nextExpected;
        const before = count;
        let batch: Track[] = [];
        for (const row of visibleRows(root)) {
          if (options.signal.aborted || changed()) break;
          if (!inViewport(row)) continue;
          const observation = readTrack(row, url, collection.kind === 'album' ? collection.title : undefined);
          if (!observation) { uncertain = true; continue; }
          const { track, ordinal } = observation;
          const digest = await metadataDigest(track);
          if (options.signal.aborted || changed()) break;
          const key = track.source_track_key ? `id:${track.source_track_key}` : `metadata:${digest}`;
          if (!track.source_track_key) uncertain = true;
          const previous = seen.get(key);
          if (previous !== undefined) { if (previous !== digest) uncertain = true; continue; }
          if (count >= options.maxTracks) { uncertain = true; break; }
          if (batch.length === MAX_TRACKS || jsonBytes({ tracks: [...batch, track] }) > MAX_BYTES - 1024) {
            yield { tracks: batch }; batch = [];
            if (options.signal.aborted || changed()) break;
          }
          track.position = ordinal ?? count;
          seen.set(key, digest); batch.push(track); count++;
        }
        if (batch.length) yield { tracks: batch };
        progress('collecting');
        if (options.signal.aborted) { reason = 'user_stopped'; break; }
        if (changed()) { reason = 'dom_changed'; break; }
        const atBottom = scroll.clientHeight > 0 && scroll.scrollHeight - scroll.clientHeight - scroll.scrollTop <= 2;
        const busy = root.matches(s.busy) || [...root.querySelectorAll(s.busy)].some(node => rendered(node) && !node.closest(s.excluded));
        const end = [...root.querySelectorAll(s.end)].some(node => inViewport(node) && !node.closest(s.excluded) && /^(?:Конец списка|Больше треков нет|End of (?:list|tracks)|No more tracks)[.!]?$/iu.test(renderedText(node)));
        bottomQuiet = atBottom && count === before && !busy ? bottomQuiet + 1 : 0;
        stagnant = count === before ? stagnant + 1 : 0;
        if (bottomQuiet >= timing.quietPasses) {
          if (expectedCount(root) !== expected) { reason = 'dom_changed'; break; }
          if (!uncertain && (expected === count || (expected === undefined && end && count > 0))) reason = 'visible_end_confirmed';
          break;
        }
        // The cap is partial unless the visible end has also been verified.
        if ((count >= options.maxTracks && !atBottom) || stagnant >= timing.quietPasses * 3) break;
        progress('scrolling');
        await waitForDOM(root, options.signal, timing.settleMs, () => {
          if (!atBottom) scroll.scrollTo({ top: scroll.scrollTop + Math.max(1, scroll.clientHeight * 0.7), behavior: 'instant' });
        });
      }
    }
  } catch (error) {
    if (!options.signal.aborted) throw error;
    reason = 'user_stopped';
  }
  const summary: CaptureSummary = { completeness: reason === 'visible_end_confirmed' ? 'complete' : 'partial', reason, observedCount: count, ...(expected === undefined ? {} : { renderedExpectedCount: expected }) };
  progress('finished', summary);
  return summary;
}
