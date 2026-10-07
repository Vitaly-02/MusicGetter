import type { SelectionRow } from '../../core/capture';
import { metadata, pageRoot } from './page';
import { readTrack, visibleRows } from './tracks';
/** Thin UI adapter: selectors and row semantics remain inside this source. */
export function selectionRows(doc: Document, url: URL): SelectionRow[] {
  const collection = metadata(doc, url), root = pageRoot(doc);
  if (!collection || !root) return [];
  return visibleRows(root).flatMap((element, index) => {
    const value = readTrack(element, url, collection.kind === 'album' ? collection.title : undefined);
    return value ? [{ element, track: { ...value.track, position: value.ordinal ?? index } }] : [];
  });
}
