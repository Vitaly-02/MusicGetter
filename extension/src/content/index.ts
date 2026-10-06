import { browserAPI } from '../core/browser';
import { adapterFor } from '../sources';
import { record } from '../core/validation';
const host = globalThis as unknown as { __musicGetterContent?: boolean };
if (!host.__musicGetterContent) {
  host.__musicGetterContent = true;
  const browser = browserAPI();
  browser.runtime.onMessage.addListener((message, sender, respond) => {
    if (sender.id !== browser.runtime.id) return;
    try {
      const request = record(message, ['type']);
      if (request.type !== 'page.info') return;
      // Only source identity, never full URL/query, HTML or page state.
      respond({ page: adapterFor(new URL(location.href))?.detectPage() ?? null });
    } catch { respond({ page: null }); }
  });
}
