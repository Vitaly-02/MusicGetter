import { StubAdapter } from '../stub';
export const createAdapter = (url: URL): StubAdapter => new StubAdapter('yandex', url, ['music.yandex.ru', 'music.yandex.com', 'music.yandex.kz']);
