import { record, text } from '../core/validation';
import type { CollectionMetadata } from '../core/adapter';
import type { CaptureSummary, Source } from '../core/contracts';
import { AppError } from '../core/errors';
export function sourceValue(value: unknown): Source {
  if (value !== 'spotify' && value !== 'yandex' && value !== 'vk') throw new AppError('invalid_data');
  return value;
}
export function collectionValue(value: unknown): CollectionMetadata {
  const c = record(value, ['key','kind','title','provisional','url']);
  if (!['favorites','playlist','album','selection'].includes(String(c.kind)) || typeof c.provisional !== 'boolean') throw new AppError('invalid_data');
  // URLs are unnecessary for create; do not relay them or any other page fields.
  return { key: text(c.key,512), title: text(c.title,1024), kind: c.kind as CollectionMetadata['kind'], provisional: c.provisional };
}
export function summaryValue(value: unknown): CaptureSummary {
  const s = record(value, ['completeness','reason','observedCount','renderedExpectedCount']);
  if (!['partial','complete'].includes(String(s.completeness)) || !['visible_end_confirmed','user_stopped','dom_changed','unknown_end'].includes(String(s.reason)) || (s.completeness === 'complete' && s.reason !== 'visible_end_confirmed') || !Number.isSafeInteger(s.observedCount) || (s.observedCount as number) < 0 || (s.observedCount as number) > 100000) throw new AppError('invalid_data');
  return { completeness: s.completeness as CaptureSummary['completeness'], reason: s.reason as CaptureSummary['reason'], observedCount: s.observedCount as number };
}
export function sourceHost(source: Source, hostname: string): boolean {
  return ({ spotify: ['open.spotify.com'], yandex: ['music.yandex.ru','music.yandex.com','music.yandex.kz'], vk: ['vk.com','music.vk.com'] })[source].includes(hostname);
}
