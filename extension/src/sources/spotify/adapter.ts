import { StubAdapter } from '../stub';
export const createAdapter = (url: URL): StubAdapter => new StubAdapter('spotify', url, ['open.spotify.com']);
