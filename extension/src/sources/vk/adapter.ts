import { StubAdapter } from '../stub';
export const createAdapter = (url: URL): StubAdapter => new StubAdapter('vk', url, ['vk.com', 'music.vk.com']);
