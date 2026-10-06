import { build } from 'esbuild';
import { mkdir, cp, writeFile, rm } from 'node:fs/promises';
const firefox = process.argv.includes('firefox');
const url = new URL(process.env.MUSICGETTER_BACKEND_URL || 'http://127.0.0.1:8080');
if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && ['localhost', '127.0.0.1'].includes(url.hostname))) || url.username || url.password || url.search || url.hash || url.pathname !== '/' || /(^|\.)(spotify\.com|yandex\.(ru|com|kz)|vk\.(com|ru))$/.test(url.hostname)) throw Error('Use an HTTPS MusicGetter backend origin, or HTTP loopback for development.');
const outdir = firefox ? 'dist/firefox' : 'dist/chrome';
await rm(outdir, { recursive: true, force: true });
await mkdir(outdir, { recursive: true });
await build({ entryPoints: { background: 'src/background/index.ts', content: 'src/content/index.ts', popup: 'src/popup/index.ts' }, outdir, bundle: true, format: 'iife', target: ['chrome120', 'firefox128'], define: { __BACKEND_ORIGIN__: JSON.stringify(url.origin) }, sourcemap: false });
await cp('src/popup/popup.html', `${outdir}/popup.html`);
await cp('src/popup/popup.css', `${outdir}/popup.css`);
const manifest = {
  manifest_version: 3, name: 'MusicGetter', version: '0.1.0',
  description: 'Перенос музыкальных коллекций через видимые страницы. Источники пока в разработке.',
  permissions: ['activeTab', 'scripting', 'alarms'],
  optional_host_permissions: [`${url.protocol}//${url.hostname}/*`],
  action: { default_popup: 'popup.html', default_title: 'MusicGetter' },
  background: firefox ? { scripts: ['background.js'] } : { service_worker: 'background.js' },
  content_security_policy: { extension_pages: `script-src 'self'; object-src 'none'; connect-src ${url.origin}; base-uri 'none'` },
  ...(firefox ? { browser_specific_settings: { gecko: { id: 'musicgetter@local.invalid', strict_min_version: '128.0' } } } : { minimum_chrome_version: '120' })
};
await writeFile(`${outdir}/manifest.json`, JSON.stringify(manifest, null, 2) + '\n');
console.log(`Built ${outdir}; backend ${url.origin}`);
