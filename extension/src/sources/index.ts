import { createAdapter as spotify } from './spotify/adapter';
import { createAdapter as yandex } from './yandex/adapter';
import { createAdapter as vk } from './vk/adapter';
import type { MusicSourceAdapter } from '../core/adapter';
/** Registry composes isolated adapters; no adapter imports another source. */
export function adapterFor(url: URL): MusicSourceAdapter | undefined { return [spotify(url), yandex(url), vk(url)].find(adapter => adapter.detectPage() !== null); }
